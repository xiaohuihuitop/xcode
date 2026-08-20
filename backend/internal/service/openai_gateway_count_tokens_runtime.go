package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ForwardCountTokensAsAnthropicExchange bridges Anthropic count_tokens to the
// OpenAI input_tokens endpoint without constructing a Gin context.
func (s *OpenAIGatewayService) ForwardCountTokensAsAnthropicExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
) error {
	if exchange == nil || exchange.Request() == nil {
		return ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if account == nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusServiceUnavailable, "api_error", "No available OpenAI accounts")
		return fmt.Errorf("count_tokens: missing account")
	}

	prepared, err := prepareOpenAIInputTokensCountRequest(body, account, defaultMappedModel, ctx)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return err
	}
	upstreamBody, err := marshalOpenAIUpstreamJSON(prepared.Request)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusInternalServerError, "api_error", "Failed to build request")
		return fmt.Errorf("marshal openai input_tokens body: %w", err)
	}

	logger.L().Debug("openai count_tokens: model mapping applied",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", prepared.OriginalModel),
		zap.String("normalized_model", prepared.NormalizedModel),
		zap.String("billing_model", prepared.BillingModel),
		zap.String("upstream_model", prepared.UpstreamModel),
	)

	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return fmt.Errorf("get access token: %w", err)
	}
	upstreamReq, err := s.buildInputTokensUpstreamRequestFromHeaders(ctx, exchange.Request().Header, account, upstreamBody, token)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusInternalServerError, "api_error", "Failed to build request")
		return fmt.Errorf("build input_tokens request: %w", err)
	}

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return fmt.Errorf("openai input_tokens upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to read response")
		return fmt.Errorf("read input_tokens response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if account.Type == AccountTypeOAuth && isOpenAIOAuthInputTokensUnsupported(resp.StatusCode, respBody) {
			writeRuntimeOpenAIOAuthInputTokensFallback(exchange, account, prepared, resp.StatusCode)
			return nil
		}
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		if isOpenAIInputTokensUnsupported(resp.StatusCode, respBody) {
			writeRuntimeAnthropicCountTokensError(exchange, http.StatusNotFound, "not_found_error", "Token counting is not supported by upstream")
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
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMsg, upstreamDetail)
		errMsg := "Upstream request failed"
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			errMsg = "Rate limit exceeded"
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
			errMsg = "Upstream service temporarily unavailable"
		}
		writeRuntimeAnthropicCountTokensError(exchange, resp.StatusCode, "upstream_error", errMsg)
		if upstreamMsg == "" {
			return fmt.Errorf("input_tokens upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("input_tokens upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
	}

	inputTokens := gjson.GetBytes(respBody, "input_tokens")
	if !inputTokens.Exists() {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response missing input_tokens")
		return fmt.Errorf("input_tokens response missing input_tokens field")
	}
	writeRuntimeCountTokensJSON(exchange, http.StatusOK, int(inputTokens.Int()))
	return nil
}

func writeRuntimeAnthropicCountTokensError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":%q}}`, errType, message)))
}

func writeRuntimeCountTokensJSON(exchange gatewayruntime.HTTPExchange, status, inputTokens int) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"input_tokens":%d}`, inputTokens)))
}

func writeRuntimeOpenAIOAuthInputTokensFallback(exchange gatewayruntime.HTTPExchange, account *Account, prepared *openAIInputTokensCountPrepared, statusCode int) {
	estimated := openAIInputTokensFallbackMinimum
	if got, err := estimateOpenAIInputTokens(prepared.Request); err == nil {
		if got > 0 {
			estimated = got
		}
		logger.L().Info("openai count_tokens: oauth fallback to local tiktoken estimate",
			zap.Int64("account_id", account.ID), zap.Int("upstream_status", statusCode),
			zap.Int("estimated_input_tokens", estimated), zap.String("upstream_model", prepared.UpstreamModel))
	} else {
		logger.L().Warn("openai count_tokens: oauth local tiktoken fallback failed, using minimum estimate",
			zap.Int64("account_id", account.ID), zap.Int("upstream_status", statusCode),
			zap.Int("estimated_input_tokens", estimated), zap.String("upstream_model", prepared.UpstreamModel), zap.Error(err))
	}
	writeRuntimeCountTokensJSON(exchange, http.StatusOK, estimated)
}
