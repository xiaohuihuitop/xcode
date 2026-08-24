package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/tidwall/gjson"
)

// ForwardResponsesInputTokensExchange forwards the native Responses token
// preflight without coupling Runtime to Gin. Non-OpenAI endpoints and custom
// relays use the same local estimator as the official implementation.
func (s *OpenAIGatewayService) ForwardResponsesInputTokensExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
) error {
	if exchange == nil || exchange.Request() == nil {
		return ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if account == nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusServiceUnavailable, "api_error", "No available OpenAI accounts")
		return fmt.Errorf("responses input_tokens: missing account")
	}
	prepared, err := prepareNativeOpenAIInputTokensCountRequest(body, account)
	if err != nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return err
	}
	if shouldEstimateOpenAIInputTokensLocally(account) {
		writeRuntimeResponsesInputTokensFallback(exchange, prepared)
		return nil
	}
	if s == nil || s.httpUpstream == nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return ErrRuntimeExchangeUnavailable
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return fmt.Errorf("responses input_tokens: get access token: %w", err)
	}
	upstreamBody := ReplaceModelInBody(body, prepared.UpstreamModel)
	upstreamReq, err := s.buildInputTokensUpstreamRequestFromHeaders(ctx, exchange.Request().Header, account, upstreamBody, token)
	if err != nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusInternalServerError, "api_error", "Failed to build request")
		return fmt.Errorf("responses input_tokens: build upstream request: %w", err)
	}
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return fmt.Errorf("responses input_tokens: upstream request failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return fmt.Errorf("responses input_tokens: upstream returned no response")
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to read response")
		return fmt.Errorf("responses input_tokens: read upstream response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if isOpenAIResponsesInputTokensUnsupported(account, resp.StatusCode, respBody) {
			writeRuntimeResponsesInputTokensFallback(exchange, prepared)
			return nil
		}
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMsg, "")
		writeRuntimeResponsesInputTokensError(exchange, resp.StatusCode, "upstream_error", "Upstream request failed")
		if upstreamMsg == "" {
			return fmt.Errorf("responses input_tokens: upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("responses input_tokens: upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
	}
	if !gjson.GetBytes(respBody, "input_tokens").Exists() {
		writeRuntimeResponsesInputTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response missing input_tokens")
		return fmt.Errorf("responses input_tokens: upstream response missing input_tokens")
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	exchange.Header().Set("Content-Type", contentType)
	exchange.WriteHeader(http.StatusOK)
	_, err = exchange.Write(respBody)
	return err
}

func prepareNativeOpenAIInputTokensCountRequest(body []byte, account *Account) (*openAIInputTokensCountPrepared, error) {
	var req openAIInputTokensCountRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse responses input_tokens request: %w", err)
	}
	originalModel := strings.TrimSpace(req.Model)
	if originalModel == "" {
		return nil, fmt.Errorf("parse responses input_tokens request: model is required")
	}
	billingModel := resolveOpenAIForwardModel(account, originalModel, "")
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	req.Model = upstreamModel
	return &openAIInputTokensCountPrepared{Request: req, OriginalModel: originalModel, NormalizedModel: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel}, nil
}

func shouldEstimateOpenAIInputTokensLocally(account *Account) bool {
	if account == nil || account.IsGrok() || account.Platform != PlatformOpenAI || account.Type == AccountTypeUpstream {
		return true
	}
	if account.Type != AccountTypeAPIKey {
		return false
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		return false
	}
	parsed, err := url.Parse(baseURL)
	return err != nil || !strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func isOpenAIResponsesInputTokensUnsupported(account *Account, statusCode int, body []byte) bool {
	if statusCode == http.StatusNotFound {
		return true
	}
	return account != nil && account.Type != AccountTypeAPIKey && isOpenAIOAuthInputTokensUnsupported(statusCode, body)
}

func writeRuntimeResponsesInputTokensFallback(exchange gatewayruntime.HTTPExchange, prepared *openAIInputTokensCountPrepared) {
	estimated := openAIInputTokensFallbackMinimum
	if prepared != nil {
		if got, err := estimateOpenAIInputTokens(prepared.Request); err == nil && got > 0 {
			estimated = got
		}
	}
	writeRuntimeResponsesInputTokensJSON(exchange, http.StatusOK, estimated)
}

func writeRuntimeResponsesInputTokensJSON(exchange gatewayruntime.HTTPExchange, status, inputTokens int) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"object":"response.input_tokens","input_tokens":%d}`, inputTokens)))
}

func writeRuntimeResponsesInputTokensError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, errType, message)))
}
