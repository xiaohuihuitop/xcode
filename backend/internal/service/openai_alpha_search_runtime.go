package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/tidwall/gjson"
)

// ForwardAlphaSearchExchange is the transport-neutral Alpha Search executor.
// It preserves the standalone search protocol and the PAT Responses fallback,
// while keeping Gin out of the runtime service boundary.
func (s *OpenAIGatewayService) ForwardAlphaSearchExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	modelResult := gjson.GetBytes(body, "model")
	requestedModel := strings.TrimSpace(modelResult.String())
	if modelResult.Type != gjson.String || requestedModel == "" {
		return nil, fmt.Errorf("model is required")
	}

	upstreamModel := normalizeOpenAIModelForUpstream(account, resolveOpenAIForwardModelWithContext(ctx, account, requestedModel, ""))
	if upstreamModel != "" && upstreamModel != requestedModel {
		body = ReplaceModelInBody(body, upstreamModel)
	}
	sanitizedBody, err := sanitizeOpenAIAlphaSearchBody(body)
	if err != nil {
		return nil, fmt.Errorf("sanitize alpha search request body: %w", err)
	}
	body = sanitizedBody

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if err := s.ensureOpenAIAlphaSearchAuthMetadata(ctx, account, token, proxyURL); err != nil {
		return nil, err
	}

	if account.IsOpenAIPersonalAccessToken() {
		return s.forwardAlphaSearchViaResponsesWebSearchExchange(ctx, exchange, account, body, token, proxyURL, requestedModel, upstreamModel, apiKeyID)
	}

	request := exchange.Request()
	upstreamReq, err := s.buildOpenAIAlphaSearchRequestFromHeaders(ctx, request.Header, request.URL.Query(), account, body, token)
	if err != nil {
		return nil, err
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportErrorExchange(ctx, exchange, account, err, true)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
			writeRuntimeAlphaError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
		} else {
			writeRuntimeAlphaError(exchange, http.StatusBadGateway, "upstream_error", "Failed to read response")
		}
		return nil, fmt.Errorf("read alpha search response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		upstreamMessage := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMessage, respBody) || isOpenAIAlphaSearchEndpointUnsupported(account, resp.StatusCode) {
			shouldDisable := false
			if shouldApplyOpenAIAlphaSearchAccountErrorSideEffects(resp.StatusCode) {
				shouldDisable = s.handleFailoverSideEffects(ctx, resp, account, respBody, upstreamModel)
			}
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "failover", Message: upstreamMessage,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: resp.StatusCode, ResponseBody: respBody,
				RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
	}

	if !account.IsShadow() {
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	}
	writeRuntimeAlphaResponse(exchange, resp, respBody, s.responseHeaderFilter)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil
	}
	return &OpenAIForwardResult{
		RequestID: strings.TrimSpace(resp.Header.Get("x-request-id")), Model: requestedModel,
		UpstreamModel: upstreamModel, Duration: time.Since(upstreamStart), WebSearchCalls: 1,
	}, nil
}

func (s *OpenAIGatewayService) forwardAlphaSearchViaResponsesWebSearchExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	alphaBody []byte,
	token, proxyURL, requestedModel, upstreamModel string,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if upstreamModel == "" {
		upstreamModel = requestedModel
	}
	responsesBody, err := buildOpenAIAlphaSearchResponsesWebSearchBody(alphaBody, upstreamModel)
	if err != nil {
		return nil, err
	}
	upstreamReq, err := s.buildOpenAIAlphaSearchResponsesWebSearchRequestFromHeaders(ctx, exchange.Request().Header, apiKeyID, account, alphaBody, responsesBody, token)
	if err != nil {
		return nil, err
	}
	exchange.SetState(openAIUpstreamEndpointContextKey, "/v1/responses")
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportErrorExchange(ctx, exchange, account, err, true)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeRuntimeAlphaError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
		} else {
			writeRuntimeAlphaError(exchange, http.StatusBadGateway, "upstream_error", "Failed to read response")
		}
		return nil, fmt.Errorf("read alpha search responses fallback response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		upstreamMessage := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMessage, respBody) {
			shouldDisable := false
			if shouldApplyOpenAIAlphaSearchAccountErrorSideEffects(resp.StatusCode) {
				shouldDisable = s.handleFailoverSideEffects(ctx, resp, account, respBody, upstreamModel)
			}
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody, RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode)}
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		writeRuntimeAlphaResponse(exchange, resp, respBody, s.responseHeaderFilter)
		return nil, nil
	}
	if !account.IsShadow() {
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	}
	alphaRespBody, err := openAIAlphaSearchResponseFromResponsesSSE(respBody)
	if err != nil {
		return nil, err
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(http.StatusOK)
	_, _ = exchange.Write(alphaRespBody)
	return &OpenAIForwardResult{
		RequestID: strings.TrimSpace(resp.Header.Get("x-request-id")), Model: requestedModel,
		UpstreamModel: upstreamModel, UpstreamEndpoint: "/v1/responses", ResponseHeaders: resp.Header.Clone(),
		Duration: time.Since(upstreamStart), WebSearchCalls: 1,
	}, nil
}

func (s *OpenAIGatewayService) handleOpenAIUpstreamTransportErrorExchange(ctx context.Context, exchange gatewayruntime.HTTPExchange, account *Account, err error, passthrough bool) error {
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, Passthrough: passthrough, Kind: "request_error", Message: safeErr})
	if errors.Is(err, context.Canceled) {
		return err
	}
	if s != nil {
		scheduleOllamaCloudUsageActivity(s.deferredService, account)
	}
	if classifyOpenAITransportError(err).Persistent {
		s.tempUnscheduleOpenAITransportError(ctx, account, safeErr)
	}
	return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: openAITransportFailoverBody}
}

func writeRuntimeAlphaResponse(exchange gatewayruntime.HTTPExchange, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if exchange == nil || resp == nil || exchange.Written() {
		return
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, filter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	exchange.Header().Set("Content-Type", contentType)
	exchange.WriteHeader(resp.StatusCode)
	_, _ = exchange.Write(body)
}

func writeRuntimeAlphaError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, errType, message)))
}
