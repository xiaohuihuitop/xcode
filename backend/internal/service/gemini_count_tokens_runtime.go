package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
)

// ForwardNativeCountTokensExchange handles Gemini Native countTokens without
// constructing a Gin context. GenerateContent and streaming remain separate
// capabilities because they have different response aggregation semantics.
func (s *GeminiMessagesCompatService) ForwardNativeCountTokensExchange(ctx context.Context, exchange gatewayruntime.HTTPExchange, account *Account, originalModel string, body []byte) (*ForwardResult, error) {
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	startTime := time.Now()
	if strings.TrimSpace(originalModel) == "" || len(body) == 0 {
		writeRuntimeGoogleError(exchange, http.StatusBadRequest, "Request body and model are required")
		return nil, fmt.Errorf("gemini countTokens request is incomplete")
	}
	if filtered, err := filterEmptyPartsFromGeminiRequest(body); err == nil {
		body = filtered
	}
	body = ensureGeminiFunctionCallThoughtSignatures(body)
	mappedModel := originalModel
	if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		mappedModel = platformModel
	} else if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(originalModel)
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	var lastErr error
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, requestIDHeader, err := s.buildGeminiCountTokensRequest(ctx, account, mappedModel, body)
		if err != nil {
			writeRuntimeGoogleError(exchange, http.StatusBadGateway, err.Error())
			return nil, err
		}
		resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			lastErr = err
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, Kind: "request_error", Message: sanitizeUpstreamErrorMessage(err.Error())})
			if attempt < geminiMaxRetries {
				sleepGeminiBackoff(attempt)
				continue
			}
			estimated := estimateGeminiCountTokens(body)
			writeRuntimeJSON(exchange, http.StatusOK, map[string]any{"totalTokens": estimated})
			return &ForwardResult{Usage: ClaudeUsage{}, Model: originalModel, UpstreamModel: mappedModel, Duration: time.Since(startTime)}, nil
		}
		requestID := firstNonEmpty(resp.Header.Get(requestIDHeader), resp.Header.Get("x-goog-request-id"))
		if requestID != "" {
			exchange.Header().Set("x-request-id", requestID)
		}
		respBody, readErr := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		_ = resp.Body.Close()
		if readErr != nil {
			if attempt < geminiMaxRetries {
				sleepGeminiBackoff(attempt)
				continue
			}
			writeRuntimeGoogleError(exchange, http.StatusBadGateway, "Failed to read upstream response")
			return nil, readErr
		}
		if resp.StatusCode >= http.StatusBadRequest && s.shouldRetryGeminiUpstreamError(account, resp.StatusCode) && attempt < geminiMaxRetries {
			if resp.StatusCode == http.StatusTooManyRequests {
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: requestID, Kind: "retry", Message: sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(respBody))})
			sleepGeminiBackoff(attempt)
			continue
		}
		if resp.StatusCode >= http.StatusBadRequest {
			if account.Type == AccountTypeOAuth && isGeminiInsufficientScope(resp.Header, respBody) {
				estimated := estimateGeminiCountTokens(body)
				writeRuntimeJSON(exchange, http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{RequestID: requestID, Usage: ClaudeUsage{}, Model: originalModel, UpstreamModel: mappedModel, Duration: time.Since(startTime)}, nil
			}
			s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			setRuntimeOpsUpstreamError(exchange, resp.StatusCode, message, runtimeUpstreamErrorDetail(s.cfg, respBody))
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name, UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: requestID, Kind: "http_error", Message: message})
			if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
			}
			writeRuntimeRawResponse(exchange, resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
			return nil, fmt.Errorf("gemini upstream error: %d message=%s", resp.StatusCode, message)
		}
		writeRuntimeRawResponse(exchange, resp.StatusCode, "application/json", respBody)
		return &ForwardResult{RequestID: requestID, Usage: ClaudeUsage{}, Model: originalModel, UpstreamModel: mappedModel, Duration: time.Since(startTime)}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("gemini countTokens retries exhausted")
	}
	return nil, lastErr
}

func (s *GeminiMessagesCompatService) buildGeminiCountTokensRequest(ctx context.Context, account *Account, mappedModel string, body []byte) (*http.Request, string, error) {
	if account == nil {
		return nil, "", errors.New("gemini account is required")
	}
	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := account.GetCredential("api_key")
		if strings.TrimSpace(apiKey) == "" {
			return nil, "", errors.New("gemini api_key not configured")
		}
		baseURL, err := s.validateUpstreamBaseURL(account.GetGeminiBaseURL(geminicli.AIStudioBaseURL))
		if err != nil {
			return nil, "", err
		}
		fullURL, err := buildGeminiAIStudioModelActionURL(baseURL, mappedModel, "countTokens", false)
		if err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", apiKey)
		return req, "x-request-id", nil
	case AccountTypeOAuth:
		if s.tokenProvider == nil {
			return nil, "", errors.New("gemini token provider not configured")
		}
		accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return nil, "", err
		}
		baseURL, err := s.validateUpstreamBaseURL(account.GetGeminiBaseURL(geminicli.AIStudioBaseURL))
		if err != nil {
			return nil, "", err
		}
		fullURL, err := buildGeminiAIStudioModelActionURL(baseURL, mappedModel, "countTokens", false)
		if err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return req, "x-request-id", nil
	case AccountTypeServiceAccount:
		if s.tokenProvider == nil {
			return nil, "", errors.New("gemini token provider not configured")
		}
		accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return nil, "", err
		}
		fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, "countTokens", false)
		if err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return req, "x-request-id", nil
	default:
		return nil, "", fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func writeRuntimeGoogleError(exchange gatewayruntime.HTTPExchange, status int, message string) {
	writeRuntimeJSON(exchange, status, map[string]any{"error": map[string]any{"code": status, "message": message, "status": "UNKNOWN"}})
}

func writeRuntimeJSON(exchange gatewayruntime.HTTPExchange, status int, value any) {
	if exchange == nil || exchange.Written() {
		return
	}
	body, _ := json.Marshal(value)
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write(body)
}
