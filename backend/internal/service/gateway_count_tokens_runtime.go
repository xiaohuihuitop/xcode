package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// ForwardCountTokensExchange is the transport-neutral Anthropic/Gemini
// count_tokens path. It preserves request normalization, beta policy,
// fingerprinting, signature retry and upstream error behavior.
func (s *GatewayService) ForwardCountTokensExchange(ctx context.Context, exchange gatewayruntime.HTTPExchange, account *Account, parsed *ParsedRequest) error {
	if exchange == nil || exchange.Request() == nil {
		return ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	if parsed == nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return fmt.Errorf("parse request: empty request")
	}

	if account != nil && account.IsAnthropicAPIKeyPassthroughEnabled() {
		passthroughBody := parsed.Body.Bytes()
		if requestModel := parsed.Model; requestModel != "" {
			mappedModel, _ := resolveRequestUpstreamModel(ctx, account, requestModel)
			if mappedModel != requestModel {
				passthroughBody = s.replaceModelInBody(passthroughBody, mappedModel)
				logger.LegacyPrintf("service.gateway", "CountTokens passthrough model mapping: %s -> %s (account: %s)", requestModel, mappedModel, account.Name)
			}
		}
		return s.forwardCountTokensAnthropicAPIKeyPassthroughExchange(ctx, exchange, account, passthroughBody)
	}
	if account != nil && account.IsBedrock() {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported for Bedrock")
		return nil
	}

	body := parsed.Body.Bytes()
	replaceBody := func(next []byte) error {
		if err := parsed.ReplaceBody(next); err != nil {
			return fmt.Errorf("rewrite count_tokens body: %w", err)
		}
		body = parsed.Body.Bytes()
		return nil
	}
	requestModel := parsed.Model
	if err := replaceBody(StripEmptyTextBlocks(body)); err != nil {
		return err
	}
	isClaudeCodeCountTokens := IsClaudeCodeClient(ctx) || isClaudeCodeClient(exchange.Request().Header.Get("User-Agent"), parsed.MetadataUserID)
	shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCodeCountTokens
	if shouldMimicClaudeCode {
		normalizedBody, normalizedModel := normalizeClaudeOAuthRequestBody(body, requestModel, claudeOAuthNormalizeOptions{stripSystemCacheControl: true})
		requestModel = normalizedModel
		if err := replaceBody(normalizedBody); err != nil {
			return err
		}
		if err := replaceBody(s.rewriteMessageCacheControlIfEnabled(ctx, body)); err != nil {
			return err
		}
		if rewrite := buildToolNameRewriteFromBody(body); rewrite != nil {
			if err := replaceBody(applyToolNameRewriteToBody(body, rewrite)); err != nil {
				return err
			}
		} else if err := replaceBody(applyToolsLastCacheBreakpoint(body)); err != nil {
			return err
		}
	}

	if account.Platform == PlatformAntigravity {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported for this platform")
		return nil
	}
	if requestModel != "" {
		mappedModel := requestModel
		mappingSource := ""
		if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
			mappedModel, mappingSource = platformModel, "platform"
		} else if account.Type == AccountTypeAPIKey {
			mappedModel = account.GetMappedModel(requestModel)
			if mappedModel != requestModel {
				mappingSource = "account"
			}
		}
		if mappingSource == "" && account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
			if normalized := claude.NormalizeModelID(requestModel); normalized != requestModel {
				mappedModel, mappingSource = normalized, "prefix"
			}
		}
		if mappedModel != requestModel {
			originalModel := requestModel
			if err := replaceBody(s.replaceModelInBody(body, mappedModel)); err != nil {
				return err
			}
			requestModel, parsed.Model = mappedModel, mappedModel
			logger.LegacyPrintf("service.gateway", "CountTokens model mapping applied: %s -> %s (account: %s, source=%s)", originalModel, mappedModel, account.Name, mappingSource)
		}
	}

	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return err
	}
	filterSet := s.evaluateBetaPolicy(ctx, "", account, requestModel).filterSet
	upstreamReq, acceptedWireBody, err := s.buildCountTokensRequestFromHeaders(ctx, exchange.Request().Header, filterSet, exchange.SetState, account, body, token, tokenType, requestModel, shouldMimicClaudeCode)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil && (!account.IsCustomBaseURLEnabled() || account.GetCustomBaseURL() == "") {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		setRuntimeOpsUpstreamError(exchange, 0, sanitizeUpstreamErrorMessage(err.Error()), "")
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Request failed")
		return fmt.Errorf("upstream request failed: %w", err)
	}
	respBody, err := readRuntimeCountTokensBody(resp, s, exchange)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusBadRequest && s.shouldRectifySignatureError(ctx, account, respBody, requestModel) {
		filteredBody := FilterThinkingBlocksForRetry(body, requestModel)
		retryReq, retryWireBody, buildErr := s.buildCountTokensRequestFromHeaders(ctx, exchange.Request().Header, filterSet, exchange.SetState, account, filteredBody, token, tokenType, requestModel, shouldMimicClaudeCode)
		if buildErr == nil {
			if retryResp, retryErr := s.httpUpstream.DoWithTLS(retryReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account)); retryErr == nil {
				if retryResp.StatusCode < http.StatusBadRequest {
					acceptedWireBody = retryWireBody
				}
				resp = retryResp
				respBody, err = readRuntimeCountTokensBody(resp, s, exchange)
				if err != nil {
					return err
				}
			}
		}
	}
	if resp.StatusCode < http.StatusBadRequest && !bytes.Equal(acceptedWireBody, body) {
		if err := replaceBody(acceptedWireBody); err != nil {
			return err
		}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		upstreamMessage := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		upstreamDetail := runtimeUpstreamErrorDetail(s.cfg, respBody)
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMessage, upstreamDetail)
		errMessage := "Upstream request failed"
		if resp.StatusCode == http.StatusTooManyRequests {
			errMessage = "Rate limit exceeded"
		} else if resp.StatusCode == 529 {
			errMessage = "Service overloaded"
		}
		writeRuntimeAnthropicCountTokensError(exchange, resp.StatusCode, "upstream_error", errMessage)
		if upstreamMessage == "" {
			return fmt.Errorf("upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMessage)
	}
	writeRuntimeRawResponse(exchange, resp.StatusCode, "application/json", respBody)
	return nil
}

func (s *GatewayService) forwardCountTokensAnthropicAPIKeyPassthroughExchange(ctx context.Context, exchange gatewayruntime.HTTPExchange, account *Account, body []byte) error {
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return err
	}
	if tokenType != "apikey" {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Invalid account token type")
		return fmt.Errorf("anthropic api key passthrough requires apikey token, got: %s", tokenType)
	}
	upstreamReq, err := s.buildCountTokensRequestAnthropicAPIKeyPassthroughFromHeaders(ctx, exchange.Request().Header, account, body, token)
	if err != nil {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Passthrough: true, Kind: "request_error", Message: safeErr})
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Request failed")
		return fmt.Errorf("upstream request failed: %w", err)
	}
	respBody, err := readRuntimeCountTokensBody(resp, s, exchange)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		upstreamMessage := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if isCountTokensUnsupported404(resp.StatusCode, respBody) {
			writeRuntimeAnthropicCountTokensError(exchange, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported by upstream")
			return nil
		}
		upstreamDetail := runtimeUpstreamErrorDetail(s.cfg, respBody)
		setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMessage, upstreamDetail)
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"), UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Passthrough: true, Kind: "http_error", Message: upstreamMessage, Detail: upstreamDetail})
		errMessage := "Upstream request failed"
		if resp.StatusCode == http.StatusTooManyRequests {
			errMessage = "Rate limit exceeded"
		} else if resp.StatusCode == 529 {
			errMessage = "Service overloaded"
		}
		writeRuntimeAnthropicCountTokensError(exchange, resp.StatusCode, "upstream_error", errMessage)
		if upstreamMessage == "" {
			return fmt.Errorf("upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMessage)
	}
	writeAnthropicPassthroughResponseHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	writeRuntimeRawResponse(exchange, resp.StatusCode, contentType, respBody)
	return nil
}

func readRuntimeCountTokensBody(resp *http.Response, s *GatewayService, exchange gatewayruntime.HTTPExchange) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err == nil {
		return body, nil
	}
	if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
		setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
	} else {
		writeRuntimeAnthropicCountTokensError(exchange, http.StatusBadGateway, "upstream_error", "Failed to read response")
	}
	return nil, err
}

func runtimeUpstreamErrorDetail(cfg *config.Config, body []byte) string {
	if cfg == nil || !cfg.Gateway.LogUpstreamErrorBody {
		return ""
	}
	maxBytes := cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	return truncateString(string(body), maxBytes)
}

func writeRuntimeRawResponse(exchange gatewayruntime.HTTPExchange, status int, contentType string, body []byte) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", contentType)
	exchange.WriteHeader(status)
	_, _ = exchange.Write(body)
}
