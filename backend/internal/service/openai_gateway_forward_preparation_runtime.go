package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/tidwall/gjson"
)

// forwardOpenAIResponsesHTTPRuntime prepares an ordinary OpenAI Responses
// request without constructing a Gin context. It intentionally excludes
// compact, passthrough and WebSocket modes; those modes carry protocol state
// that remains on the explicit legacy path until their own exchange boundary
// is migrated.
func (s *OpenAIGatewayService) forwardOpenAIResponsesHTTPRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if s == nil || exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if account.Platform != PlatformOpenAI || account.IsOpenAIPassthroughEnabled() {
		return nil, fmt.Errorf("openai native responses runtime requires a non-passthrough OpenAI account")
	}
	request := exchange.Request()
	if isOpenAIResponsesCompactPathFromRuntimeRequest(request) {
		return nil, fmt.Errorf("openai compact requests remain on the legacy protocol path")
	}
	if ctx == nil {
		ctx = request.Context()
	}
	wsDecision := s.getOpenAIWSProtocolResolver().Resolve(account)
	wsDecision = resolveOpenAIWSDecisionByClientTransport(wsDecision, OpenAIClientTransportHTTP)
	if wsDecision.Transport != OpenAIUpstreamTransportHTTPSSE {
		return nil, fmt.Errorf("openai websocket requests remain on the legacy protocol path")
	}

	startTime := time.Now()
	canonicalImageIntentBody := append([]byte(nil), body...)
	ClearActualOpenAIUpstreamEndpointExchange(exchange)
	exchange.SetState(openAIResponsesNamespaceNamesContextKey, nil)
	exchange.SetState(openAICompatMessagesBridgeContextKey, false)
	exchange.SetState(openAIImageIntentHintContextKey, nil)
	exchange.SetState("openai_ws_transport_decision", string(wsDecision.Transport))
	exchange.SetState("openai_ws_transport_reason", wsDecision.Reason)

	restrictionResult := s.detectCodexClientRestrictionRequest(ctx, request.Header, account, body)
	if restrictionResult.Enabled && !restrictionResult.Matched {
		exchange.SetState(OpsClientBusinessLimitedKey, true)
		exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		message := CodexClientRestrictionMessage(restrictionResult)
		writeRuntimeOpenAIResponsesError(exchange, http.StatusForbidden, "forbidden_error", message)
		return nil, errors.New("codex_cli_only restriction: only codex official clients are allowed")
	}

	preparedBody, sanitized, err := sanitizeOpenAIResponsesToolParameterTypes(body)
	if err != nil {
		return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "tools", fmt.Errorf("sanitize OpenAI Responses tool parameters: %w", err))
	}
	if sanitized {
		body = preparedBody
	}
	if account.IsOpenAIOAuth() && isOpenAIResponsesLiteHeader(request.Header.Get(responsesLiteHeader)) {
		liteBody, changed, liteErr := normalizeOpenAIResponsesLiteToolsPayload(body)
		if liteErr != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "tools", liteErr)
		}
		if changed {
			body = liteBody
		}
	}

	if shouldFlattenOpenAIResponsesNamespaces(account, wsDecision.Transport, false, false) {
		body, err = flattenOpenAIResponsesNamespacesExchange(exchange, body)
		if err != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "tools", err)
		}
	}
	if shouldStripOpenAIResponsesInputNamespaces(account, wsDecision.Transport, false) {
		body, err = stripOpenAIResponsesInputNamespaces(body, shouldKeepOpenAIResponsesToolCallNamespaces(account, wsDecision.Transport, false, false))
		if err != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "input", err)
		}
	}

	requestView := newOpenAIRequestView(body)
	requestedModel := requestView.Model
	stream := requestView.Stream
	promptCacheKey := requestView.PromptCacheKey
	originalModel := requestedModel
	SetActualOpenAIUpstreamEndpointExchange(exchange, appendOpenAIResponsesRequestPathSuffix(openAIResponsesEndpoint, openAIResponsesRequestPathSuffixFromRuntimeRequest(request)))

	if account.Type == AccountTypeAPIKey {
		sanitizedBody, changed, sanitizeErr := sanitizeOpenAIResponsesInputItemIDs(body)
		if sanitizeErr != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "input", fmt.Errorf("sanitize OpenAI Responses input item IDs: %w", sanitizeErr))
		}
		if changed {
			body = sanitizedBody
			requestView = newOpenAIRequestView(body)
			requestedModel, stream, promptCacheKey = requestView.Model, requestView.Stream, requestView.PromptCacheKey
			originalModel = requestedModel
		}
	}

	compatMessagesBridge := isOpenAICompatMessagesBridgeBody(body)
	exchange.SetState(openAICompatMessagesBridgeContextKey, compatMessagesBridge)
	isCodexCLI := openai.IsCodexOfficialClientByHeaders(request.Header.Get("User-Agent"), request.Header.Get("originator")) || (s.cfg != nil && s.cfg.Gateway.ForceCodexCLI)
	codexImagePolicy := codexImageGenerationExplicitToolPolicyAllow
	if isCodexCLI {
		codexImagePolicy = account.CodexImageGenerationExplicitToolPolicy()
	}

	bodyModified := false
	var reqBody map[string]any
	ensureReqBody := func() (map[string]any, error) {
		if requestView.HasPatches() {
			patchedBody, patchErr := requestView.ApplyPatches()
			if patchErr != nil {
				return nil, patchErr
			}
			body = patchedBody
			requestView = newOpenAIRequestView(body)
			reqBody = nil
			bodyModified = false
		}
		if reqBody != nil {
			return reqBody, nil
		}
		decoded, decodeErr := requestView.DecodeRuntime()
		if decodeErr != nil {
			return nil, decodeErr
		}
		reqBody = decoded
		return reqBody, nil
	}
	markPatchSet := func(path string, value any) {
		bodyModified = true
		if requestView.patchesDisabled {
			if reqBody != nil {
				setOpenAIRequestMapPath(reqBody, path, value)
			}
			return
		}
		requestView.MarkPatchSet(path, value)
	}
	markPatchDelete := func(path string) {
		bodyModified = true
		if requestView.patchesDisabled {
			if reqBody != nil {
				deleteOpenAIRequestMapPath(reqBody, path)
			}
			return
		}
		requestView.MarkPatchDelete(path)
	}
	markDecodedModified := func() {
		bodyModified = true
		requestView.DisablePatches()
	}

	codexImageBridgeEnabled := isCodexCLI &&
		!isOpenAIResponsesLiteHeader(request.Header.Get(responsesLiteHeader)) &&
		codexImagePolicy != codexImageGenerationExplicitToolPolicyStrip &&
		s.isCodexImageGenerationBridgeEnabled(ctx, account, nil)
	imageIntent := IsImageGenerationIntent(openAIResponsesEndpoint, requestedModel, canonicalImageIntentBody)
	exchange.SetState(openAIImageIntentHintContextKey, imageIntent)
	if isCodexCLI && codexImagePolicy == codexImageGenerationExplicitToolPolicyStrip {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if stripOpenAIImageGenerationTools(decoded) {
			markDecodedModified()
		}
		imageIntent = IsImageGenerationIntentMap(openAIResponsesEndpoint, requestedModel, decoded)
	}
	if !gjson.GetBytes(body, "instructions").Exists() && !compatMessagesBridge {
		markPatchSet("instructions", defaultCodexSynthInstructions(requestedModel))
	}

	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, requestedModel, "")
	if billingModel != requestedModel {
		requestedModel = billingModel
		markPatchSet("model", billingModel)
	}
	upstreamModel := normalizeOpenAIModelForUpstream(account, requestedModel)
	if upstreamModel != "" && upstreamModel != requestedModel {
		requestedModel = upstreamModel
		markPatchSet("model", upstreamModel)
	}
	if strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()) == "minimal" {
		markPatchSet("reasoning.effort", "none")
	}
	imageIntent = imageIntent || IsImageGenerationIntent(openAIResponsesEndpoint, requestedModel, nil) || isOpenAIImageGenerationModel(upstreamModel)
	if codexImageBridgeEnabled || isOpenAIImageGenerationModel(requestView.Model) || openAIRequestBodyImageGenerationToolNeedsNormalization(body) || isOpenAIImageGenerationModel(upstreamModel) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if codexImageBridgeEnabled && ensureOpenAIResponsesImageGenerationTool(decoded) {
			markDecodedModified()
		}
		if codexImageBridgeEnabled && ensureOpenAIResponsesImageGenerationToolChoiceAuto(decoded) {
			markDecodedModified()
		}
		if normalizeOpenAIResponsesImageGenerationTools(decoded) {
			markDecodedModified()
		}
		if normalizeOpenAIResponsesImageOnlyModel(decoded) {
			markDecodedModified()
			if model, ok := decoded["model"].(string); ok {
				upstreamModel = strings.TrimSpace(model)
			}
		}
		if err := validateOpenAIResponsesImageModel(decoded, upstreamModel); err != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "model", err)
		}
		if hasOpenAIImageGenerationTool(decoded) {
			imageIntent = true
		}
		if codexImageBridgeEnabled && applyCodexImageGenerationBridgeInstructions(decoded) {
			markDecodedModified()
		}
	}
	if isCodexSparkModel(upstreamModel) && openAIRequestBodyMayContainImageInput(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if err := validateCodexSparkInput(decoded, upstreamModel); err != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "input", err)
		}
	}
	if isCodexSparkModel(upstreamModel) && openAIRequestBodyHasImageGenerationDeclaration(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if stripCodexSparkImageGenerationTools(decoded) {
			markDecodedModified()
		}
	}

	if account.Type == AccountTypeOAuth {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		var codexResult codexTransformResult
		if compatMessagesBridge {
			codexResult = applyCodexOAuthTransformWithOptions(decoded, codexOAuthTransformOptions{IsCodexCLI: isCodexCLI, SkipDefaultInstructions: true, PreserveToolCallIDs: true})
			ensureCodexOAuthInstructionsField(decoded)
			markDecodedModified()
		} else {
			codexResult = applyCodexOAuthTransform(decoded, isCodexCLI, false)
		}
		if codexResult.Modified {
			markDecodedModified()
		}
		if applyCodexClientMetadata(decoded, account) {
			markDecodedModified()
		}
		if codexResult.NormalizedModel != "" {
			upstreamModel = codexResult.NormalizedModel
		}
		if codexResult.PromptCacheKey != "" {
			promptCacheKey = codexResult.PromptCacheKey
		}
	}

	if !SupportsVerbosity(upstreamModel) && gjson.GetBytes(body, "text.verbosity").Exists() {
		markPatchDelete("text.verbosity")
	}
	if !isCodexCLI {
		if maxTokens := gjson.GetBytes(body, "max_tokens"); maxTokens.Exists() {
			if !gjson.GetBytes(body, "max_output_tokens").Exists() {
				markPatchSet("max_output_tokens", maxTokens.Value())
			}
			markPatchDelete("max_tokens")
		}
		if account.Type == AccountTypeAPIKey && gjson.GetBytes(body, "max_completion_tokens").Exists() {
			markPatchDelete("max_completion_tokens")
		}
		for _, unsupportedField := range []string{"prompt_cache_retention", "safety_identifier", "prompt_cache_options"} {
			if gjson.GetBytes(body, unsupportedField).Exists() {
				markPatchDelete(unsupportedField)
			}
		}
	}
	if gjson.GetBytes(body, "previous_response_id").Exists() {
		markPatchDelete("previous_response_id")
	}
	if openAIRequestBodyMayContainEmptyBase64InputImage(body) {
		decoded, decodeErr := ensureReqBody()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if sanitizeEmptyBase64InputImagesInOpenAIRequestBodyMap(decoded) {
			markDecodedModified()
		}
	}

	if rawTier := requestView.ServiceTier; rawTier != "" {
		if normTier := normalizedOpenAIServiceTierValue(rawTier); normTier != "" {
			action, errMsg := s.evaluateOpenAIFastPolicy(ctx, account, upstreamModel, normTier)
			switch action {
			case BetaPolicyActionBlock:
				if errMsg == "" {
					errMsg = fmt.Sprintf("openai service_tier=%s is not allowed for model %s", normTier, upstreamModel)
				}
				blocked := &OpenAIFastBlockedError{Message: errMsg}
				exchange.SetState(OpsClientBusinessLimitedKey, true)
				exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
				writeRuntimeOpenAIResponsesError(exchange, http.StatusForbidden, "permission_error", blocked.Message)
				return nil, blocked
			case BetaPolicyActionFilter:
				markPatchDelete("service_tier")
			case OpenAIFastPolicyActionForcePriority:
				if rawTier != OpenAIFastTierPriority {
					markPatchSet("service_tier", OpenAIFastTierPriority)
				}
			default:
				if normTier != rawTier {
					markPatchSet("service_tier", normTier)
				}
			}
		}
	}

	if bodyModified {
		if requestView.HasPatches() {
			if patchedBody, patchErr := requestView.ApplyPatches(); patchErr == nil {
				body = patchedBody
				requestView = newOpenAIRequestView(body)
				reqBody = nil
				bodyModified = false
			}
		}
		if bodyModified {
			decoded, decodeErr := ensureReqBody()
			if decodeErr != nil {
				return nil, decodeErr
			}
			var marshalErr error
			body, marshalErr = marshalOpenAIUpstreamJSON(decoded)
			if marshalErr != nil {
				return nil, fmt.Errorf("serialize request body: %w", marshalErr)
			}
			requestView = newOpenAIRequestView(body)
		}
	}

	imageBillingModel, imageSizeTier, imageInputSize := "", "", ""
	if imageIntent {
		var imageCfg OpenAIResponsesImageBillingConfig
		if reqBody != nil {
			imageCfg, err = resolveOpenAIResponsesImageBillingConfigDetailed(reqBody, billingModel)
		} else {
			imageCfg, err = resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, billingModel)
		}
		if err != nil {
			return nil, s.runtimeOpenAIResponsesPreparationError(exchange, http.StatusBadRequest, "size", err)
		}
		imageBillingModel, imageSizeTier, imageInputSize = imageCfg.Model, imageCfg.SizeTier, imageCfg.InputSize
	}

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, upstreamModel)
	reasoningEffortValue := ""
	if reasoningEffort != nil {
		reasoningEffortValue = *reasoningEffort
	}
	logger.LegacyPrintf("service.openai_gateway", "[OpenAI] native Responses runtime preparation account_id=%d model=%s upstream_model=%s stream=%v", account.ID, originalModel, upstreamModel, stream)
	result, forwardErr := s.forwardOpenAIHTTPExchange(ctx, openAIHTTPExchangeForwardInput{
		Exchange: exchange, Account: account, Body: body, Token: token,
		OriginalModel: originalModel, UpstreamModel: upstreamModel, BillingModel: billingModel,
		ReasoningEffort: reasoningEffortValue, PromptCacheKey: promptCacheKey, APIKeyID: apiKeyID,
		IsCodexCLI: isCodexCLI, Stream: stream, StartTime: startTime,
		ImageBillingModel: imageBillingModel, ImageSizeTier: imageSizeTier, ImageInputSize: imageInputSize,
	})
	return result, forwardErr
}

func (s *OpenAIGatewayService) runtimeOpenAIResponsesPreparationError(
	exchange gatewayruntime.HTTPExchange,
	status int,
	param string,
	err error,
) error {
	if err == nil {
		return nil
	}
	setRuntimeOpsUpstreamError(exchange, status, err.Error(), "")
	message := err.Error()
	if param != "" {
		message = fmt.Sprintf("%s: %s", param, message)
	}
	writeRuntimeOpenAIResponsesError(exchange, status, "invalid_request_error", message)
	return err
}
