package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

// forwardOpenAIMessagesHTTPRuntime prepares an Anthropic Messages request for
// the OpenAI Responses upstream without constructing a Gin context. Replay,
// continuation and OAuth turn-state compatibility are carried by scalar
// runtime state and the exchange, so the full HTTP/SSE Messages path stays
// transport-neutral.
func (s *OpenAIGatewayService) forwardOpenAIMessagesHTTPRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if s == nil || exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if account.Platform != PlatformOpenAI || account.IsOpenAIPassthroughEnabled() {
		return nil, fmt.Errorf("openai messages responses runtime requires a non-passthrough OpenAI account")
	}
	if isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return nil, fmt.Errorf("openai compact requests remain on the legacy protocol path")
	}
	decision := resolveOpenAIWSDecisionByClientTransport(s.getOpenAIWSProtocolResolver().Resolve(account), OpenAIClientTransportHTTP)
	if decision.Transport != OpenAIUpstreamTransportHTTPSSE {
		return nil, fmt.Errorf("openai websocket requests remain on the legacy protocol path")
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}

	exchange.SetState("openai_runtime_messages_responses_pure", true)
	exchange.SetState(openAICompatMessagesBridgeContextKey, account.Type == AccountTypeOAuth)
	ClearActualOpenAIUpstreamEndpointExchange(exchange)
	SetActualOpenAIUpstreamEndpointExchange(exchange, openAIResponsesEndpoint)

	restrictionResult := s.detectCodexClientRestrictionRequest(ctx, exchange.Request().Header, account, body)
	if restrictionResult.Enabled && !restrictionResult.Matched {
		exchange.SetState(OpsClientBusinessLimitedKey, true)
		exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		message := CodexClientRestrictionMessage(restrictionResult)
		writeRuntimeAnthropicError(exchange, http.StatusForbidden, "forbidden_error", message)
		return nil, errors.New("codex_cli_only restriction: only codex official clients are allowed")
	}

	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := strings.TrimSpace(anthropicReq.Model)
	if originalModel == "" {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	anthropicDigestReq := cloneAnthropicRequestForDigest(&anthropicReq)
	normalizedModel := strings.TrimSpace(anthropicReq.Model)
	clientStream := anthropicReq.Stream
	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, normalizedModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	promptCacheKey = strings.TrimSpace(promptCacheKey)
	anthropicDigestChain := ""
	anthropicMatchedDigestChain := ""
	compatReplayGuardEnabled := shouldAutoInjectPromptCacheKeyForCompat(upstreamModel)
	if promptCacheKey == "" && compatReplayGuardEnabled {
		promptCacheKey = promptCacheKeyFromAnthropicMetadataSession(&anthropicReq)
		if promptCacheKey == "" {
			promptCacheKey = deriveAnthropicCacheControlPromptCacheKey(&anthropicReq)
		}
		if promptCacheKey == "" {
			anthropicDigestChain = buildOpenAICompatAnthropicDigestChain(anthropicDigestReq)
			if reusedKey, matchedChain := s.findOpenAICompatAnthropicDigestPromptCacheKey(account, apiKeyID, anthropicDigestChain); reusedKey != "" {
				promptCacheKey = reusedKey
				anthropicMatchedDigestChain = matchedChain
			} else {
				promptCacheKey = promptCacheKeyFromAnthropicDigest(anthropicDigestChain)
			}
		}
	}
	compatContinuationEnabled := openAICompatContinuationEnabled(account, upstreamModel)
	previousResponseID := ""
	if compatContinuationEnabled {
		previousResponseID = s.getOpenAICompatSessionResponseIDRuntime(ctx, account, apiKeyID, promptCacheKey)
	}
	compatContinuationDisabled := compatContinuationEnabled &&
		s.isOpenAICompatSessionContinuationDisabledRuntime(ctx, account, apiKeyID, promptCacheKey)
	compatTurnState := ""
	if compatReplayGuardEnabled && account.Type != AccountTypeOAuth && previousResponseID == "" && !compatContinuationDisabled {
		applyAnthropicCompatFullReplayGuard(&anthropicReq)
	}

	responsesReq, err := apicompat.AnthropicToResponses(&anthropicReq)
	if err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to responses: %w", err)
	}
	responsesReq.Stream = true
	responsesReq.Model = upstreamModel
	if previousResponseID != "" {
		responsesReq.PreviousResponseID = previousResponseID
		trimAnthropicCompatResponsesInputToLatestTurn(responsesReq)
	}
	if compatReplayGuardEnabled && account.Type != AccountTypeOAuth {
		appendOpenAICompatClaudeCodeTodoGuard(responsesReq)
	}
	if responsesReq.Reasoning != nil {
		responsesReq.Reasoning.Effort = openAICompatAnthropicReasoningEffort(&anthropicReq, upstreamModel, responsesReq.Reasoning.Effort)
	}
	if containsBetaToken(exchange.Request().Header.Get("anthropic-beta"), claude.BetaFastMode) {
		responsesReq.ServiceTier = "priority"
	}

	responsesBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	if account.Type == AccountTypeOAuth {
		var requestBody map[string]any
		if err := json.Unmarshal(responsesBody, &requestBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		codexResult := applyCodexOAuthTransformWithOptions(requestBody, codexOAuthTransformOptions{
			SkipDefaultInstructions: true,
			PreserveToolCallIDs:     true,
		})
		forcedTemplateText := ""
		if s.cfg != nil {
			forcedTemplateText = s.cfg.Gateway.ForcedCodexInstructionsTemplate
		}
		existingInstructions, _ := requestBody["instructions"].(string)
		if strings.TrimSpace(existingInstructions) == "" {
			existingInstructions = extractPromptLikeInstructionsFromInput(requestBody)
		}
		templateUpstreamModel := upstreamModel
		if codexResult.NormalizedModel != "" {
			templateUpstreamModel = codexResult.NormalizedModel
		}
		if _, err := applyForcedCodexInstructionsTemplate(requestBody, forcedTemplateText, forcedCodexInstructionsTemplateData{
			ExistingInstructions: strings.TrimSpace(existingInstructions),
			OriginalModel:        originalModel,
			NormalizedModel:      normalizedModel,
			BillingModel:         billingModel,
			UpstreamModel:        templateUpstreamModel,
		}); err != nil {
			return nil, err
		}
		ensureCodexOAuthInstructionsField(requestBody)
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		}
		if compatReplayGuardEnabled {
			appendOpenAICompatClaudeCodeTodoGuardToRequestBody(requestBody)
			compatTurnState = s.getOpenAICompatSessionTurnStateRuntime(ctx, account, apiKeyID, promptCacheKey)
		}
		delete(requestBody, "prompt_cache_key")
		responsesBody, err = json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("remarshal after codex transform: %w", err)
		}
	}

	if account.Type == AccountTypeAPIKey && promptCacheKey != "" {
		var requestBody map[string]any
		if err := json.Unmarshal(responsesBody, &requestBody); err != nil {
			return nil, fmt.Errorf("unmarshal for prompt cache key injection: %w", err)
		}
		if existing, ok := requestBody["prompt_cache_key"].(string); !ok || strings.TrimSpace(existing) == "" {
			requestBody["prompt_cache_key"] = promptCacheKey
			responsesBody, err = json.Marshal(requestBody)
			if err != nil {
				return nil, fmt.Errorf("remarshal after prompt cache key injection: %w", err)
			}
		}
	}

	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, responsesBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			exchange.SetState(OpsClientBusinessLimitedKey, true)
			exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeRuntimeAnthropicError(exchange, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(exchange.Request().Header.Get("User-Agent"), exchange.Request().Header.Get("originator"))
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		isCodexCLI = true
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(responsesBody, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, responsesBody, billingModel)
	reasoningEffortValue := ""
	if reasoningEffort != nil {
		reasoningEffortValue = *reasoningEffort
	}
	result, forwardErr := s.forwardOpenAIHTTPExchange(ctx, openAIHTTPExchangeForwardInput{
		Exchange:           exchange,
		Account:            account,
		Body:               responsesBody,
		Token:              token,
		OriginalModel:      originalModel,
		UpstreamModel:      upstreamModel,
		BillingModel:       billingModel,
		ReasoningEffort:    reasoningEffortValue,
		PromptCacheKey:     promptCacheKey,
		APIKeyID:           apiKeyID,
		IsCodexCLI:         isCodexCLI,
		Stream:             clientStream,
		ResponseFormat:     openAIHTTPResponseFormatAnthropic,
		PreviousResponseID: previousResponseID,
		OnPreviousResponseRecovery: func(unsupported bool) {
			if unsupported {
				s.disableOpenAICompatSessionContinuationRuntime(ctx, account, apiKeyID, promptCacheKey)
				return
			}
			s.deleteOpenAICompatSessionResponseIDRuntime(ctx, account, apiKeyID, promptCacheKey)
		},
		TurnState: compatTurnState,
		StartTime: time.Now(),
	})
	if result != nil {
		result.Stream = clientStream
		result.UpstreamEndpoint = openAIResponsesEndpoint
		if compatContinuationEnabled && promptCacheKey != "" && result.ResponseID != "" {
			s.bindOpenAICompatSessionResponseIDRuntime(ctx, account, apiKeyID, promptCacheKey, result.ResponseID)
		}
		if promptCacheKey != "" && anthropicDigestChain != "" {
			s.bindOpenAICompatAnthropicDigestPromptCacheKey(account, apiKeyID, anthropicDigestChain, promptCacheKey, anthropicMatchedDigestChain)
		}
		if turnState := strings.TrimSpace(result.ResponseHeaders.Get("x-codex-turn-state")); turnState != "" {
			s.bindOpenAICompatSessionTurnStateRuntime(ctx, account, apiKeyID, promptCacheKey, turnState)
		}
		if responsesReq.ServiceTier != "" {
			value := responsesReq.ServiceTier
			result.ServiceTier = &value
		}
		if responsesReq.Reasoning != nil && responsesReq.Reasoning.Effort != "" {
			value := responsesReq.Reasoning.Effort
			result.ReasoningEffort = &value
		}
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) shouldUseOpenAIMessagesHTTPRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
) bool {
	if s == nil || exchange == nil || exchange.Request() == nil || account == nil || account.Platform != PlatformOpenAI {
		return false
	}
	if account.IsOpenAIPassthroughEnabled() || isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return false
	}
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return false
	}
	if account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey {
		return false
	}
	var request apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return false
	}
	applyOpenAICompatModelNormalization(&request)
	decision := resolveOpenAIWSDecisionByClientTransport(s.getOpenAIWSProtocolResolver().Resolve(account), OpenAIClientTransportHTTP)
	return decision.Transport == OpenAIUpstreamTransportHTTPSSE
}
