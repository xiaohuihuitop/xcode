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
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// forwardOpenAIChatCompletionsHTTPRuntime prepares a Chat Completions request
// for an OpenAI Responses upstream without constructing a Gin context. It is
// limited to ordinary HTTP/SSE accounts; compact, passthrough and WebSocket
// requests continue through their explicit compatibility branch.
func (s *OpenAIGatewayService) forwardOpenAIChatCompletionsHTTPRuntime(
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
		return nil, fmt.Errorf("openai chat responses runtime requires a non-passthrough OpenAI account")
	}
	if isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return nil, fmt.Errorf("openai compact requests remain on the legacy protocol path")
	}
	decision := s.getOpenAIWSProtocolResolver().Resolve(account)
	decision = resolveOpenAIWSDecisionByClientTransport(decision, OpenAIClientTransportHTTP)
	if decision.Transport != OpenAIUpstreamTransportHTTPSSE {
		return nil, fmt.Errorf("openai websocket requests remain on the legacy protocol path")
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}

	exchange.SetState("openai_runtime_chat_responses_pure", true)
	exchange.SetState(openAICompatMessagesBridgeContextKey, false)
	ClearActualOpenAIUpstreamEndpointExchange(exchange)
	SetActualOpenAIUpstreamEndpointExchange(exchange, openAIResponsesEndpoint)

	restrictionResult := s.detectCodexClientRestrictionRequest(ctx, exchange.Request().Header, account, body)
	if restrictionResult.Enabled && !restrictionResult.Matched {
		exchange.SetState(OpsClientBusinessLimitedKey, true)
		exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		message := CodexClientRestrictionMessage(restrictionResult)
		writeRuntimeOpenAIChatError(exchange, http.StatusForbidden, "forbidden_error", message)
		return nil, errors.New("codex_cli_only restriction: only codex official clients are allowed")
	}

	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	originalModel := strings.TrimSpace(chatReq.Model)
	if originalModel == "" {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("missing model in request")
	}
	clientStream := chatReq.Stream
	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	promptCacheKey = strings.TrimSpace(promptCacheKey)
	if promptCacheKey == "" && account.Type == AccountTypeOAuth && shouldAutoInjectPromptCacheKeyForCompat(upstreamModel) {
		promptCacheKey = deriveCompatPromptCacheKey(&chatReq, upstreamModel)
	}

	isResponsesShape := !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()
	var responsesReq *apicompat.ResponsesRequest
	var responsesBody []byte
	var err error
	if isResponsesShape {
		responsesBody, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("rewrite model in responses-shape body: %w", err)
		}
		for _, field := range cursorResponsesUnsupportedFields {
			responsesBody, _ = sjson.DeleteBytes(responsesBody, field)
		}
		responsesBody, normalizedTier, tierErr := normalizeResponsesBodyServiceTier(responsesBody)
		if tierErr != nil {
			return nil, fmt.Errorf("normalize service_tier in responses-shape body: %w", tierErr)
		}
		responsesReq = &apicompat.ResponsesRequest{Model: upstreamModel, ServiceTier: normalizedTier}
		if effort := gjson.GetBytes(responsesBody, "reasoning.effort").String(); effort != "" {
			responsesReq.Reasoning = &apicompat.ResponsesReasoning{Effort: effort}
		}
	} else {
		responsesReq, err = apicompat.ChatCompletionsToResponses(&chatReq)
		if err != nil {
			return nil, fmt.Errorf("convert chat completions to responses: %w", err)
		}
		responsesReq.Model = upstreamModel
		normalizeResponsesRequestServiceTier(responsesReq)
		responsesBody, err = json.Marshal(responsesReq)
		if err != nil {
			return nil, fmt.Errorf("marshal responses request: %w", err)
		}
	}

	// The OAuth upstream always speaks SSE. Force the wire body to stream while
	// retaining clientStream for the response conversion and billing facts.
	responsesBody, err = sjson.SetBytes(responsesBody, "stream", true)
	if err != nil {
		return nil, fmt.Errorf("force upstream responses stream: %w", err)
	}

	if account.Type == AccountTypeOAuth {
		var requestBody map[string]any
		if err := json.Unmarshal(responsesBody, &requestBody); err != nil {
			return nil, fmt.Errorf("unmarshal for codex transform: %w", err)
		}
		isJSONObjectFormat := strings.EqualFold(strings.TrimSpace(gjson.GetBytes(responsesBody, "text.format.type").String()), "json_object")
		codexResult := applyCodexOAuthTransformWithOptions(requestBody, codexOAuthTransformOptions{
			SkipDefaultInstructions:             !isResponsesShape,
			OmitPromotedSystemMessagesFromInput: !isResponsesShape && !isJSONObjectFormat,
		})
		if !isResponsesShape {
			ensureCodexOAuthInstructionsField(requestBody)
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		} else if promptCacheKey != "" {
			requestBody["prompt_cache_key"] = promptCacheKey
		}
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
			writeRuntimeOpenAIChatError(exchange, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	responsesBody = updatedBody

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(responsesBody, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, responsesBody, billingModel)
	reasoningEffortValue := ""
	if reasoningEffort != nil {
		reasoningEffortValue = *reasoningEffort
	}
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(exchange.Request().Header.Get("User-Agent"), exchange.Request().Header.Get("originator"))
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		isCodexCLI = true
	}
	startTime := time.Now()
	result, forwardErr := s.forwardOpenAIHTTPExchange(ctx, openAIHTTPExchangeForwardInput{
		Exchange:        exchange,
		Account:         account,
		Body:            responsesBody,
		Token:           token,
		OriginalModel:   originalModel,
		UpstreamModel:   upstreamModel,
		BillingModel:    billingModel,
		ReasoningEffort: reasoningEffortValue,
		PromptCacheKey:  promptCacheKey,
		APIKeyID:        apiKeyID,
		IsCodexCLI:      isCodexCLI,
		Stream:          clientStream,
		StartTime:       startTime,
		ResponseFormat:  openAIHTTPResponseFormatChatCompletions,
	})
	if result != nil {
		result.Stream = clientStream
		result.UpstreamEndpoint = openAIResponsesEndpoint
		if responsesReq != nil {
			if responsesReq.ServiceTier != "" {
				value := responsesReq.ServiceTier
				result.ServiceTier = &value
			}
			if responsesReq.Reasoning != nil && responsesReq.Reasoning.Effort != "" {
				value := responsesReq.Reasoning.Effort
				result.ReasoningEffort = &value
			}
		}
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) shouldUseOpenAIChatResponsesHTTPRuntime(exchange gatewayruntime.HTTPExchange, account *Account) bool {
	if s == nil || exchange == nil || exchange.Request() == nil || account == nil || account.Platform != PlatformOpenAI {
		return false
	}
	if account.IsOpenAIPassthroughEnabled() || isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return false
	}
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldRouteChatCompletionsViaResponses(account.Extra) {
		return false
	}
	decision := resolveOpenAIWSDecisionByClientTransport(s.getOpenAIWSProtocolResolver().Resolve(account), OpenAIClientTransportHTTP)
	return decision.Transport == OpenAIUpstreamTransportHTTPSSE
}
