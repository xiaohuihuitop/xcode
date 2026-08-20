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
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
)

// ForwardResponsesExchange handles the Responses-to-Chat compatibility path
// for OpenAI API-key accounts without the private Gin bridge.
func (s *OpenAIGatewayService) ForwardResponsesExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("openai responses exchange requires an API-key account")
	}
	startTime := time.Now()
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := strings.TrimSpace(responsesReq.Model)
	if originalModel == "" {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	effectiveTools, err := apicompat.EffectiveResponsesTools(&responsesReq)
	if err != nil {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("resolve responses tools: %w", err)
	}
	customTools := apicompat.CustomToolNames(effectiveTools)
	toolSearch := apicompat.HasToolSearchTool(effectiveTools)
	namespaceTools := apicompat.NamespaceToolNames(effectiveTools)
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}
	clientStream := responsesReq.Stream
	serviceTier := extractOpenAIServiceTierFromBody(body)
	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, originalModel, "")
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	chatReq.Model = upstreamModel
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions fallback request: %w", err)
	}
	chatBody, err = s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, chatBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			exchange.SetState(OpsClientBusinessLimitedKey, true)
			exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeRuntimeOpenAIResponsesError(exchange, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, err
	}
	if serviceTier == nil {
		serviceTier = extractOpenAIServiceTierFromBody(chatBody)
	}
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	const endpoint = grokChatRawEndpoint
	exchange.SetState(openAIUpstreamEndpointContextKey, endpoint)
	resp, err := s.sendCCUpstreamRequestExchange(ctx, exchange, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent())
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("upstream returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		respBody, upstreamMsg := s.readOpenAIUpstreamErrorExchange(resp)
		if failoverErr := s.failoverOpenAIUpstreamHTTPErrorExchange(ctx, exchange, account, resp, respBody, upstreamMsg, upstreamModel, targetURL); failoverErr != nil {
			return nil, failoverErr
		}
		return s.handleResponsesErrorResponseExchange(ctx, exchange, account, resp, respBody, originalModel)
	}
	if clientStream {
		return s.streamChatCompletionsAsResponsesExchange(exchange, resp, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsResponsesExchange(exchange, resp, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponsesExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(body, &ccResp); err != nil {
		writeRuntimeOpenAIResponsesError(exchange, http.StatusBadGateway, "api_error", "Failed to parse upstream response")
		return nil, fmt.Errorf("parse chat response: %w", err)
	}
	responsesResp := apicompat.ChatCompletionsResponseToResponses(&ccResp, originalModel, customTools, toolSearch, namespaceTools)
	encoded, err := json.Marshal(responsesResp)
	if err != nil {
		return nil, fmt.Errorf("marshal responses response: %w", err)
	}
	writeRuntimeJSONResponse(exchange, resp, http.StatusOK, encoded, s.responseHeaderFilter)
	usage := OpenAIUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = parsed
	}
	return &OpenAIForwardResult{
		RequestID: runtimeHeaderValue(resp.Header, "x-request-id"), Usage: usage,
		Model: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel,
		ReasoningEffort: reasoningEffort, ServiceTier: serviceTier, Stream: false,
		Duration: time.Since(startTime), UpstreamEndpoint: grokChatRawEndpoint,
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsResponsesExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	state := apicompat.NewChatCompletionsToResponsesStreamState(originalModel)
	state.CustomTools = customTools
	state.ToolSearchDeclared = toolSearch
	state.NamespaceTools = namespaceTools
	clientDisconnected := false
	headersWritten := false
	writeEvents := func(events []apicompat.ResponsesStreamEvent) {
		if clientDisconnected || len(events) == 0 || exchange == nil {
			return
		}
		if !headersWritten {
			writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
			headersWritten = true
		}
		for _, event := range events {
			sse, err := apicompat.ResponsesEventToSSE(event)
			if err != nil {
				continue
			}
			if _, err := exchange.Write([]byte(sse)); err != nil {
				clientDisconnected = true
				return
			}
		}
		exchange.Flush()
	}
	scan := s.scanCCStream(resp, "openai responses chat runtime", requestID, startTime, func(chunk *apicompat.ChatCompletionsChunk) {
		writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(chunk, state))
	})
	if scan.Err != nil {
		return &OpenAIForwardResult{
			RequestID: requestID, Usage: scan.Usage, Model: originalModel, BillingModel: billingModel,
			UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
			Stream: true, Duration: time.Since(startTime), FirstTokenMs: scan.FirstTokenMs,
			ClientDisconnect: clientDisconnected, UpstreamEndpoint: grokChatRawEndpoint,
		}, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	writeEvents(apicompat.FinalizeChatCompletionsResponsesStream(state))
	if !clientDisconnected && exchange != nil && !headersWritten {
		return nil, fmt.Errorf("responses stream did not write a response")
	}
	if !clientDisconnected {
		if _, err := exchange.Write([]byte("data: [DONE]\n\n")); err != nil {
			clientDisconnected = true
		} else {
			exchange.Flush()
		}
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai responses chat runtime", requestID)
	}
	return &OpenAIForwardResult{
		RequestID: requestID, Usage: scan.Usage, Model: originalModel, BillingModel: billingModel,
		UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
		Stream: true, Duration: time.Since(startTime), FirstTokenMs: scan.FirstTokenMs,
		ClientDisconnect: clientDisconnected, UpstreamEndpoint: grokChatRawEndpoint,
	}, nil
}

// ForwardAsAnthropicExchange handles the Messages-to-Chat compatibility path
// for OpenAI API-key accounts without the private Gin bridge.
func (s *OpenAIGatewayService) ForwardAsAnthropicExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("openai messages exchange requires an API-key account")
	}
	startTime := time.Now()
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}
	originalModel := strings.TrimSpace(anthropicReq.Model)
	if originalModel == "" {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	applyOpenAICompatModelNormalization(&anthropicReq)
	chatReq, err := apicompat.AnthropicToChatCompletionsRequest(&anthropicReq)
	if err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert anthropic to chat completions: %w", err)
	}
	clientStream := anthropicReq.Stream
	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, anthropicReq.Model, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	chatReq.Model = upstreamModel
	chatReq.ReasoningEffort = openAICompatAnthropicReasoningEffort(&anthropicReq, upstreamModel, chatReq.ReasoningEffort)
	convertedEffort := chatReq.ReasoningEffort
	reasoningEffort := ApplyThinkingEnabledFallback(&convertedEffort, body, billingModel)
	serviceTier := extractOpenAIServiceTierFromBody(body)
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(chatBody, upstreamModel); normalized {
		chatBody = normalizedBody
	}
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	const endpoint = grokChatRawEndpoint
	exchange.SetState(openAIUpstreamEndpointContextKey, endpoint)
	resp, err := s.sendCCUpstreamRequestExchange(ctx, exchange, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent())
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("upstream returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		respBody, upstreamMsg := s.readOpenAIUpstreamErrorExchange(resp)
		if failoverErr := s.failoverOpenAIUpstreamHTTPErrorExchange(ctx, exchange, account, resp, respBody, upstreamMsg, upstreamModel, targetURL); failoverErr != nil {
			return nil, failoverErr
		}
		return s.handleAnthropicErrorResponseExchange(ctx, exchange, account, resp, respBody, originalModel)
	}
	if clientStream {
		return s.streamChatCompletionsAsAnthropicExchange(exchange, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsAnthropicExchange(exchange, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsAnthropicExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(body, &ccResp); err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadGateway, "api_error", "Failed to parse upstream response")
		return nil, fmt.Errorf("parse chat response: %w", err)
	}
	anthropicResp := apicompat.ChatCompletionsResponseToAnthropic(&ccResp, originalModel)
	encoded, err := json.Marshal(anthropicResp)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic response: %w", err)
	}
	writeRuntimeJSONResponse(exchange, resp, http.StatusOK, encoded, s.responseHeaderFilter)
	usage := OpenAIUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = parsed
	}
	return &OpenAIForwardResult{
		RequestID: runtimeHeaderValue(resp.Header, "x-request-id"), Usage: usage,
		Model: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel,
		ReasoningEffort: reasoningEffort, ServiceTier: serviceTier, Stream: false,
		Duration: time.Since(startTime), UpstreamEndpoint: grokChatRawEndpoint,
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsAnthropicExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	state := apicompat.NewChatCompletionsToAnthropicStreamState(originalModel)
	clientDisconnected := false
	headersWritten := false
	writeEvents := func(events []apicompat.AnthropicStreamEvent) {
		if clientDisconnected || len(events) == 0 || exchange == nil {
			return
		}
		if !headersWritten {
			writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
			headersWritten = true
		}
		for _, event := range events {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(event)
			if err != nil {
				continue
			}
			if _, err := exchange.Write([]byte(sse)); err != nil {
				clientDisconnected = true
				return
			}
		}
		exchange.Flush()
	}
	scan := s.scanCCStream(resp, "openai messages chat runtime", requestID, startTime, func(chunk *apicompat.ChatCompletionsChunk) {
		writeEvents(apicompat.ChatCompletionsChunkToAnthropicEvents(chunk, state))
	})
	usage := scan.Usage
	if scan.Err != nil {
		return &OpenAIForwardResult{
			RequestID: requestID, Usage: usage, Model: originalModel, BillingModel: billingModel,
			UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
			Stream: true, Duration: time.Since(startTime), FirstTokenMs: scan.FirstTokenMs,
			ClientDisconnect: clientDisconnected, UpstreamEndpoint: grokChatRawEndpoint,
		}, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}
	writeEvents(apicompat.FinalizeChatCompletionsAnthropicStream(state))
	if !clientDisconnected && exchange != nil && !headersWritten {
		return nil, fmt.Errorf("messages stream did not write a response")
	}
	if !clientDisconnected {
		exchange.Flush()
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai messages chat runtime", requestID)
	}
	return &OpenAIForwardResult{
		RequestID: requestID, Usage: usage, Model: originalModel, BillingModel: billingModel,
		UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
		Stream: true, Duration: time.Since(startTime), FirstTokenMs: scan.FirstTokenMs,
		ClientDisconnect: clientDisconnected, UpstreamEndpoint: grokChatRawEndpoint,
	}, nil
}

func (s *OpenAIGatewayService) handleResponsesErrorResponseExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	resp *http.Response,
	body []byte,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	body = s.redactAgentIdentitySensitiveBody(ctx, account, body)
	if hit, code, cyberMsg := detectOpenAICyberPolicy(body); hit {
		markRuntimeCyberPolicy(exchange, CyberPolicyMark{Code: code, Message: cyberMsg, Body: truncateString(string(body), 4096), UpstreamStatus: resp.StatusCode})
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, cyberMsg, truncateString(string(body), 2048))
		if cyberMsg == "" {
			cyberMsg = "Request blocked by upstream cyber-security policy"
		}
		writeRuntimeOpenAIResponsesError(exchange, resp.StatusCode, "invalid_request_error", cyberMsg)
		return nil, fmt.Errorf("openai cyber_policy: %s", cyberMsg)
	}
	return s.handleCompatRuntimeError(ctx, exchange, account, resp, body, requestedModel, writeRuntimeOpenAIResponsesError)
}

func (s *OpenAIGatewayService) handleAnthropicErrorResponseExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	resp *http.Response,
	body []byte,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	body = s.redactAgentIdentitySensitiveBody(ctx, account, body)
	if hit, code, cyberMsg := detectOpenAICyberPolicy(body); hit {
		markRuntimeCyberPolicy(exchange, CyberPolicyMark{Code: code, Message: cyberMsg, Body: truncateString(string(body), 4096), UpstreamStatus: resp.StatusCode})
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, cyberMsg, truncateString(string(body), 2048))
		if cyberMsg == "" {
			cyberMsg = "Request blocked by upstream cyber-security policy"
		}
		writeRuntimeAnthropicError(exchange, resp.StatusCode, "invalid_request_error", cyberMsg)
		return nil, fmt.Errorf("openai cyber_policy: %s", cyberMsg)
	}
	return s.handleCompatRuntimeError(ctx, exchange, account, resp, body, requestedModel, writeRuntimeAnthropicError)
}

type runtimeCompatErrorWriter func(gatewayruntime.HTTPExchange, int, string, string)

func (s *OpenAIGatewayService) handleCompatRuntimeError(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	resp *http.Response,
	body []byte,
	requestedModel string,
	writeError runtimeCompatErrorWriter,
) (*OpenAIForwardResult, error) {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("Upstream error: %d", resp.StatusCode)
	}
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMsg, upstreamDetail)
	if status, errType, errMsg, matched := applyErrorPassthroughRuleExchange(exchange, account.Platform, resp.StatusCode, body, http.StatusBadGateway, "api_error", "Upstream request failed"); matched {
		exchange.SetState(ResponseCommittedKey, true)
		writeError(exchange, status, errType, errMsg)
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched)", resp.StatusCode)
	}
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"), Kind: "http_error", Message: upstreamMsg, Detail: upstreamDetail})
		exchange.SetState(ResponseCommittedKey, true)
		writeError(exchange, http.StatusInternalServerError, "api_error", "Upstream gateway error")
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes)", resp.StatusCode)
	}
	shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, requestedModel)
	kind := "http_error"
	if shouldDisable {
		kind = "failover"
	}
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"), Kind: kind, Message: upstreamMsg, Detail: upstreamDetail})
	if shouldDisable {
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: body}
	}
	errType := "api_error"
	switch resp.StatusCode {
	case http.StatusBadRequest:
		errType = "invalid_request_error"
	case http.StatusNotFound:
		errType = "not_found_error"
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
	}
	exchange.SetState(ResponseCommittedKey, true)
	writeError(exchange, resp.StatusCode, errType, upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func writeRuntimeJSONResponse(exchange gatewayruntime.HTTPExchange, resp *http.Response, status int, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if exchange == nil || exchange.Written() {
		return
	}
	if resp != nil {
		responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, filter)
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, status)
	exchange.WriteHeader(status)
	_, _ = exchange.Write(body)
}

func writeRuntimeSSEHeaders(exchange gatewayruntime.HTTPExchange, resp *http.Response, filter *responseheaders.CompiledHeaderFilter) {
	if exchange == nil || exchange.Written() {
		return
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, filter)
	exchange.Header().Set("Content-Type", "text/event-stream")
	exchange.Header().Set("Cache-Control", "no-cache")
	exchange.Header().Set("Connection", "keep-alive")
	exchange.Header().Set("X-Accel-Buffering", "no")
	exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, http.StatusOK)
	exchange.WriteHeader(http.StatusOK)
}

func writeRuntimeOpenAIResponsesError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	writeRuntimeJSONResponse(exchange, nil, status, mustRuntimeErrorBody(map[string]any{"error": map[string]string{"type": errType, "message": message}}), nil)
}

func writeRuntimeAnthropicError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	writeRuntimeJSONResponse(exchange, nil, status, mustRuntimeErrorBody(map[string]any{"type": "error", "error": map[string]string{"type": errType, "message": message}}), nil)
}

func mustRuntimeErrorBody(value any) []byte {
	body, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
	}
	return body
}
