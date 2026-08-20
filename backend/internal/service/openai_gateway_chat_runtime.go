package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ForwardAsChatCompletionsExchange handles the OpenAI API-key raw Chat path
// without creating a Gin context. Conversion paths and OAuth transports keep
// using the existing service implementation until their exchange variants are
// migrated separately.
func (s *OpenAIGatewayService) ForwardAsChatCompletionsExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	_ = promptCacheKey
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("openai chat exchange requires an API-key account")
	}
	return s.forwardAsRawChatCompletionsExchange(ctx, exchange, account, body, defaultMappedModel)
}

func (s *OpenAIGatewayService) forwardAsRawChatCompletionsExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()
	serviceTier := extractOpenAIServiceTierFromBody(body)
	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)

	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}
	if normalizedBody, normalized := NormalizeGLMOpenAIReasoningEffort(upstreamBody, upstreamModel); normalized {
		upstreamBody = normalizedBody
	}
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			exchange.SetState(OpsClientBusinessLimitedKey, true)
			exchange.SetState(OpsClientBusinessLimitedReasonKey, OpsClientBusinessLimitedReasonLocalPolicyDenied)
			writeRuntimeOpenAIChatError(exchange, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	if clientStream {
		var err error
		upstreamBody, err = ensureOpenAIChatStreamUsage(upstreamBody)
		if err != nil {
			return nil, fmt.Errorf("enable stream usage: %w", err)
		}
	}

	token := strings.TrimSpace(account.GetOpenAIApiKey())
	if token == "" {
		return nil, fmt.Errorf("account %d missing api_key credential", account.ID)
	}
	targetURL, err := s.openAIChatCompletionsTargetURL(account)
	if err != nil {
		return nil, err
	}
	const endpoint = grokChatRawEndpoint
	exchange.SetState(openAIUpstreamEndpointContextKey, endpoint)
	resp, err := s.sendCCUpstreamRequestExchange(ctx, exchange, account, targetURL, upstreamBody, clientStream, token, account.GetOpenAIUserAgent())
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
		return s.handleChatCompletionsErrorResponseExchange(ctx, exchange, account, resp, respBody, originalModel)
	}

	if clientStream {
		result, forwardErr := s.streamRawChatCompletionsExchange(exchange, resp, account, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, len(body))
		if result != nil {
			result.UpstreamEndpoint = endpoint
		}
		return result, forwardErr
	}
	result, forwardErr := s.bufferRawChatCompletionsExchange(exchange, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	if result != nil {
		result.UpstreamEndpoint = endpoint
	}
	return result, forwardErr
}

func (s *OpenAIGatewayService) sendCCUpstreamRequestExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	targetURL string,
	body []byte,
	stream bool,
	bearerToken string,
	userAgent string,
) (*http.Response, error) {
	upstreamCtx, release := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	release()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+bearerToken)
	if stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if exchange != nil && exchange.Request() != nil {
		for key, values := range exchange.Request().Header {
			if !openaiCCRawAllowedHeaders[strings.ToLower(key)] {
				continue
			}
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}
	if strings.TrimSpace(userAgent) != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportErrorExchange(ctx, exchange, account, err, false)
	}
	return resp, nil
}

func (s *OpenAIGatewayService) readOpenAIUpstreamErrorExchange(resp *http.Response) ([]byte, string) {
	if resp == nil || resp.Body == nil {
		return nil, ""
	}
	body := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	return body, message
}

func (s *OpenAIGatewayService) failoverOpenAIUpstreamHTTPErrorExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
	upstreamURL string,
) *UpstreamFailoverError {
	if resp == nil || account == nil || !s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
		return nil
	}
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(respBody), maxBytes)
	}
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"),
		UpstreamURL: safeUpstreamURL(upstreamURL), Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
	})
	shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, upstreamModel)
	return newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg,
		!shouldDisable && account.IsPoolMode() && (account.IsPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)))
}

func (s *OpenAIGatewayService) handleChatCompletionsErrorResponseExchange(
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
		writeRuntimeOpenAIChatError(exchange, resp.StatusCode, "invalid_request_error", cyberMsg)
		return nil, fmt.Errorf("openai cyber_policy: %s", cyberMsg)
	}

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
		writeRuntimeOpenAIChatError(exchange, status, errType, errMsg)
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched)", resp.StatusCode)
	}
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"),
			Kind: "http_error", Message: upstreamMsg, Detail: upstreamDetail,
		})
		exchange.SetState(ResponseCommittedKey, true)
		writeRuntimeOpenAIChatError(exchange, http.StatusInternalServerError, "api_error", "Upstream gateway error")
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes)", resp.StatusCode)
	}
	shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, requestedModel)
	kind := "http_error"
	if shouldDisable {
		kind = "failover"
	}
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"),
		Kind: kind, Message: upstreamMsg, Detail: upstreamDetail,
	})
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
	writeRuntimeOpenAIChatError(exchange, resp.StatusCode, errType, upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func (s *OpenAIGatewayService) streamRawChatCompletionsExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	account *Account,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
	requestBodyLen int,
) (*OpenAIForwardResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	writeHeaders := func() {
		if exchange == nil || exchange.Written() {
			return
		}
		responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
		exchange.Header().Set("Content-Type", "text/event-stream")
		exchange.Header().Set("Cache-Control", "no-cache")
		exchange.Header().Set("Connection", "keep-alive")
		exchange.Header().Set("X-Accel-Buffering", "no")
		exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, http.StatusOK)
		exchange.WriteHeader(http.StatusOK)
	}
	scanner := s.newUpstreamSSEScanner(resp.Body)
	refusalDetector := newOpenAIChatSilentRefusalDetector(requestBodyLen)
	var usage OpenAIUsage
	var firstTokenMs *int
	clientDisconnected := false
	clientOutputStarted := false
	pendingLines := make([]string, 0, 8)
	writeLine := func(line string) {
		if clientDisconnected {
			return
		}
		if !clientOutputStarted && !refusalDetector.ShouldReleaseClientOutput() {
			pendingLines = append(pendingLines, line)
			return
		}
		if !clientOutputStarted {
			writeHeaders()
			for _, pending := range pendingLines {
				if _, err := exchange.Write([]byte(pending + "\n")); err != nil {
					clientDisconnected = true
					logger.L().Debug("openai chat runtime: client disconnected while flushing pending data", zap.Error(err), zap.String("request_id", requestID))
					return
				}
			}
			pendingLines = pendingLines[:0]
			clientOutputStarted = true
		}
		if _, err := exchange.Write([]byte(line + "\n")); err != nil {
			clientDisconnected = true
			logger.L().Debug("openai chat runtime: client disconnected", zap.Error(err), zap.String("request_id", requestID))
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		refusalDetector.ObserveSSELine(line)
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			if strings.TrimSpace(payload) != "[DONE]" {
				usageOnly := isOpenAIChatUsageOnlyStreamChunk(payload)
				if parsed := extractCCStreamUsage(payload); parsed != nil {
					usage = *parsed
				}
				if firstTokenMs == nil && !usageOnly {
					elapsed := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &elapsed
				}
			}
		}
		writeLine(line)
		if !clientDisconnected && clientOutputStarted {
			exchange.Flush()
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		logger.L().Warn("openai chat runtime: stream read error", zap.Error(err), zap.String("request_id", requestID))
	} else if err == nil && !clientDisconnected && !clientOutputStarted {
		if refusalDetector.IsSilentRefusal() {
			return nil, newOpenAISilentRefusalFailoverErrorExchange(exchange, account, requestID)
		}
		if len(pendingLines) > 0 {
			writeHeaders()
			for _, pending := range pendingLines {
				if _, writeErr := exchange.Write([]byte(pending + "\n")); writeErr != nil {
					clientDisconnected = true
					break
				}
			}
			if !clientDisconnected {
				exchange.Flush()
				clientOutputStarted = true
			}
		}
	}
	return &OpenAIForwardResult{
		RequestID: requestID, Usage: usage, Model: originalModel, BillingModel: billingModel,
		UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
		Stream: true, Duration: time.Since(startTime), FirstTokenMs: firstTokenMs, ClientDisconnect: clientDisconnected,
	}, nil
}

func (s *OpenAIGatewayService) bufferRawChatCompletionsExchange(
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	originalModel, billingModel, upstreamModel string,
	reasoningEffort, serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
			writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		} else {
			writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}
	usage := OpenAIUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = parsed
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	exchange.Header().Set("Content-Type", contentType)
	exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, http.StatusOK)
	exchange.WriteHeader(http.StatusOK)
	_, _ = exchange.Write(body)
	return &OpenAIForwardResult{
		RequestID: requestID, Usage: usage, Model: originalModel, BillingModel: billingModel,
		UpstreamModel: upstreamModel, ReasoningEffort: reasoningEffort, ServiceTier: serviceTier,
		Stream: false, Duration: time.Since(startTime), ResponseHeaders: resp.Header.Clone(),
	}, nil
}

func writeRuntimeOpenAIChatError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	body, err := json.Marshal(map[string]any{"error": map[string]string{"type": errType, "message": message}})
	if err != nil {
		body = []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, status)
	exchange.WriteHeader(status)
	_, _ = exchange.Write(body)
}

func markRuntimeCyberPolicy(exchange gatewayruntime.HTTPExchange, mark CyberPolicyMark) {
	if exchange == nil {
		return
	}
	mark.Code = "cyber_policy"
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	if _, exists := exchange.State(opsCyberPolicyKey); !exists {
		exchange.SetState(opsCyberPolicyKey, &mark)
	}
}

func newOpenAISilentRefusalFailoverErrorExchange(exchange gatewayruntime.HTTPExchange, account *Account, requestID string) *UpstreamFailoverError {
	platform := PlatformOpenAI
	accountID := int64(0)
	accountName := ""
	if account != nil {
		platform = account.Platform
		accountID = account.ID
		accountName = account.Name
	}
	setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, openAISilentRefusalUpstreamMessage, "")
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
		Platform: platform, AccountID: accountID, AccountName: accountName,
		UpstreamStatusCode: http.StatusBadGateway, UpstreamRequestID: requestID,
		Kind: "failover", Message: openAISilentRefusalUpstreamMessage,
	})
	headers := http.Header{}
	if strings.TrimSpace(requestID) != "" {
		headers.Set("x-request-id", strings.TrimSpace(requestID))
	}
	return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: openAISilentRefusalErrorBody(), ResponseHeaders: headers}
}

func runtimeHeaderValue(header http.Header, key string) string {
	if header == nil {
		return ""
	}
	if value := strings.TrimSpace(header.Get(key)); value != "" {
		return value
	}
	for name, values := range header {
		if !strings.EqualFold(name, key) || len(values) == 0 {
			continue
		}
		return strings.TrimSpace(values[0])
	}
	return ""
}
