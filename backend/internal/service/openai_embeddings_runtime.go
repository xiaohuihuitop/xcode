package service

import (
	"bytes"
	"context"
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

// ForwardEmbeddingsExchange is the transport-neutral embeddings executor used
// by the runtime ingress. It owns upstream I/O and writes only through the
// gatewayruntime.HTTPExchange contract; Gin is intentionally absent here.
func (s *OpenAIGatewayService) ForwardEmbeddingsExchange(
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
	startTime := time.Now()

	originalModel := strings.TrimSpace(gjsonGetBytesString(body, "model"))
	if originalModel == "" {
		writeRuntimeOpenAIEmbeddingsError(exchange, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	billingModel := resolveOpenAIForwardModelWithContext(ctx, account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	upstreamBody := body
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(body, upstreamModel)
	}

	logger.L().Debug("openai embeddings: forwarding",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
	)

	apiKey := account.GetOpenAIApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIEmbeddingsURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	upstreamReq.Header.Set("Accept", "application/json")
	for key, values := range exchange.Request().Header {
		if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
			for _, value := range values {
				upstreamReq.Header.Add(key, value)
			}
		}
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		upstreamReq.Header.Set("user-agent", customUA)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()),
			Kind:        "request_error", Message: safeErr,
		})
		writeRuntimeOpenAIEmbeddingsError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
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
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()),
				Kind:        "failover", Message: upstreamMsg, Detail: upstreamDetail,
			})
			shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, upstreamModel)
			return nil, &UpstreamFailoverError{
				StatusCode: resp.StatusCode, ResponseBody: respBody,
				RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		writeRuntimeOpenAIEmbeddingsResponse(exchange, resp, respBody, s.responseHeaderFilter)
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	respBody, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeRuntimeOpenAIEmbeddingsError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		} else {
			setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
			writeRuntimeOpenAIEmbeddingsError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}

	writeRuntimeOpenAIEmbeddingsResponse(exchange, resp, respBody, s.responseHeaderFilter)
	return &OpenAIForwardResult{
		RequestID: firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		Usage:     extractOpenAIEmbeddingsUsage(respBody), Model: originalModel,
		BillingModel: billingModel, UpstreamModel: upstreamModel, Stream: false,
		Duration: time.Since(startTime),
	}, nil
}

func gjsonGetBytesString(body []byte, path string) string {
	return gjson.GetBytes(body, path).String()
}

func writeRuntimeOpenAIEmbeddingsResponse(exchange gatewayruntime.HTTPExchange, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if exchange == nil || resp == nil || exchange.Written() {
		return
	}
	if resp.Header != nil {
		responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, filter)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		exchange.Header().Set("Content-Type", contentType)
	} else {
		exchange.Header().Set("Content-Type", "application/json")
	}
	exchange.WriteHeader(resp.StatusCode)
	_, _ = exchange.Write(body)
}

func writeRuntimeOpenAIEmbeddingsError(exchange gatewayruntime.HTTPExchange, statusCode int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(statusCode)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, errType, message)))
}

func setRuntimeOpsUpstreamError(exchange gatewayruntime.HTTPExchange, statusCode int, message, detail string) {
	if exchange == nil {
		return
	}
	if statusCode > 0 {
		exchange.SetState(OpsUpstreamStatusCodeKey, statusCode)
	}
	if strings.TrimSpace(message) != "" {
		exchange.SetState(OpsUpstreamErrorMessageKey, strings.TrimSpace(message))
	}
	if strings.TrimSpace(detail) != "" {
		exchange.SetState(OpsUpstreamErrorDetailKey, strings.TrimSpace(detail))
	}
}

func appendRuntimeOpsUpstreamError(exchange gatewayruntime.HTTPExchange, event OpsUpstreamErrorEvent) {
	if exchange == nil {
		return
	}
	event.AtUnixMs = time.Now().UnixMilli()
	event.Message = sanitizeUpstreamErrorMessage(strings.TrimSpace(event.Message))
	var events []*OpsUpstreamErrorEvent
	if value, ok := exchange.State(OpsUpstreamErrorsKey); ok {
		events, _ = value.([]*OpsUpstreamErrorEvent)
	}
	copyEvent := event
	events = append(events, &copyEvent)
	exchange.SetState(OpsUpstreamErrorsKey, events)
}
