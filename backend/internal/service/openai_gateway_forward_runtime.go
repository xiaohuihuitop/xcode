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
)

// openAIHTTPExchangeForwardInput contains only the facts produced by the
// request-preparation stage. It deliberately excludes Gin and product-side
// billing objects so the HTTP attempt loop can run on HTTPExchange directly.
type openAIHTTPExchangeForwardInput struct {
	Exchange                   gatewayruntime.HTTPExchange
	Account                    *Account
	Body                       []byte
	Token                      string
	OriginalModel              string
	UpstreamModel              string
	BillingModel               string
	ReasoningEffort            string
	PromptCacheKey             string
	APIKeyID                   int64
	IsCodexCLI                 bool
	Stream                     bool
	StartTime                  time.Time
	ImageBillingModel          string
	ImageSizeTier              string
	ImageInputSize             string
	ResponseFormat             openAIHTTPResponseFormat
	PreviousResponseID         string
	OnPreviousResponseRecovery func(unsupported bool)
	TurnState                  string
}

type openAIHTTPResponseFormat uint8

const (
	openAIHTTPResponseFormatResponses openAIHTTPResponseFormat = iota
	openAIHTTPResponseFormatChatCompletions
	openAIHTTPResponseFormatAnthropic
)

// doOpenAIUpstreamRequestExchange is the transport-neutral boundary for a
// native Responses HTTP attempt. Request construction and response handling
// stay separate so the exchange owns only transport facts and Ops state.
func (s *OpenAIGatewayService) doOpenAIUpstreamRequestExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	request *http.Request,
	proxyURL string,
) (*http.Response, error) {
	if exchange == nil || account == nil || request == nil || s == nil || s.httpUpstream == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx != nil && request.Context() != ctx {
		request = request.WithContext(ctx)
	}
	startedAt := time.Now()
	resp, err := s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
	exchange.SetState(OpsUpstreamLatencyMsKey, time.Since(startedAt).Milliseconds())
	return resp, err
}

// forwardOpenAIHTTPExchange owns one native Responses HTTP attempt sequence.
// Account selection remains in the executor; this layer retains only the
// existing account-level retries, failover classification and response facts.
func (s *OpenAIGatewayService) forwardOpenAIHTTPExchange(
	ctx context.Context,
	input openAIHTTPExchangeForwardInput,
) (*OpenAIForwardResult, error) {
	if s == nil || input.Exchange == nil || input.Account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		if request := input.Exchange.Request(); request != nil {
			ctx = request.Context()
		} else {
			ctx = context.Background()
		}
	}
	if input.StartTime.IsZero() {
		input.StartTime = time.Now()
	}
	body := append([]byte(nil), input.Body...)
	stream := input.Stream
	requestModel := strings.TrimSpace(input.OriginalModel)
	upstreamModel := strings.TrimSpace(input.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = requestModel
	}
	input.BillingModel = strings.TrimSpace(input.BillingModel)
	if input.BillingModel == "" {
		input.BillingModel = requestModel
	}
	input.Token = strings.TrimSpace(input.Token)
	if input.Token == "" {
		token, _, err := s.GetAccessToken(ctx, input.Account)
		if err != nil {
			return nil, err
		}
		input.Token = token
	}

	endpoint := appendOpenAIResponsesRequestPathSuffix(
		openAIResponsesEndpoint,
		openAIResponsesRequestPathSuffixFromRuntimeRequest(input.Exchange.Request()),
	)
	ClearActualOpenAIUpstreamEndpointExchange(input.Exchange)
	input.Exchange.SetState(openAIUpstreamEndpointContextKey, endpoint)

	firstOutputTimeout := time.Duration(0)
	if stream && input.Account.Platform == PlatformOpenAI {
		firstOutputTimeout = s.openAIFirstOutputTimeout(input.ReasoningEffort)
	}
	httpInvalidEncryptedContentRetryTried := false
	previousResponseRetryTried := false
	agentTaskRecoveryTried := false
	rejectedFieldRetryState := newOpenAIResponsesRejectedFieldRetryState(body)
	for {
		upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
		var headerGuard *openAIFirstOutputHeaderGuard
		if firstOutputTimeout > 0 {
			upstreamCtx, headerGuard = newOpenAIFirstOutputHeaderGuard(
				upstreamCtx,
				releaseUpstreamCtx,
				input.StartTime.Add(firstOutputTimeout),
			)
		}
		upstreamReq, err := s.buildOpenAIUpstreamRequestFromExchange(
			upstreamCtx,
			input.Exchange,
			input.Account,
			body,
			input.Token,
			stream,
			input.PromptCacheKey,
			input.IsCodexCLI,
			input.APIKeyID,
		)
		if headerGuard == nil {
			releaseUpstreamCtx()
		}
		if err != nil {
			if headerGuard != nil {
				headerGuard.close()
			}
			return nil, err
		}
		if turnState := strings.TrimSpace(input.TurnState); turnState != "" && upstreamReq.Header.Get("x-codex-turn-state") == "" {
			upstreamReq.Header.Set("x-codex-turn-state", turnState)
		}

		proxyURL := ""
		if input.Account.ProxyID != nil && input.Account.Proxy != nil {
			proxyURL = input.Account.Proxy.URL()
		}
		resp, err := s.doOpenAIUpstreamRequestExchange(upstreamCtx, input.Exchange, input.Account, upstreamReq, proxyURL)
		if headerGuard != nil && headerGuard.stopHeaderWait() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			headerGuard.close()
			return nil, s.newOpenAIFirstOutputTimeoutErrorExchange(
				ctx, input.Exchange, input.Account, input.StartTime, requestModel,
				input.ReasoningEffort, firstOutputTimeout, "response_headers", nil,
			)
		}
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if headerGuard != nil {
				headerGuard.close()
			}
			return nil, s.handleOpenAIUpstreamTransportErrorExchange(ctx, input.Exchange, input.Account, err, false)
		}
		if resp == nil {
			if headerGuard != nil {
				headerGuard.close()
			}
			return nil, errors.New("upstream returned no response")
		}
		if headerGuard != nil {
			resp.Body = &openAIRequestContextReadCloser{ReadCloser: resp.Body, cleanup: headerGuard.close}
		}
		requestID := runtimeHeaderValue(resp.Header, "x-request-id")

		if resp.StatusCode >= http.StatusBadRequest {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			upstreamCode := extractUpstreamErrorCode(respBody)
			if !previousResponseRetryTried && strings.TrimSpace(input.PreviousResponseID) != "" &&
				(isOpenAICompatPreviousResponseNotFound(resp.StatusCode, upstreamMsg, respBody) ||
					isOpenAICompatPreviousResponseUnsupported(resp.StatusCode, upstreamMsg, respBody)) {
				retryBody := RemovePreviousResponseIDFromBody(body)
				if !bytes.Equal(retryBody, body) {
					previousResponseRetryTried = true
					if input.OnPreviousResponseRecovery != nil {
						input.OnPreviousResponseRecovery(isOpenAICompatPreviousResponseUnsupported(resp.StatusCode, upstreamMsg, respBody))
					}
					body = retryBody
					continue
				}
			}
			if !agentTaskRecoveryTried && s.isAgentIdentityAccount(ctx, input.Account) && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
				agentTaskRecoveryTried = true
				expectedTaskID := input.Account.GetCredential("task_id")
				if recoverErr := s.recoverAgentIdentityTask(ctx, input.Account, expectedTaskID); recoverErr != nil {
					return nil, fmt.Errorf("agent identity task recovery failed: %w", recoverErr)
				}
				continue
			}
			respBody = s.redactAgentIdentitySensitiveBody(ctx, input.Account, respBody)
			if !httpInvalidEncryptedContentRetryTried && resp.StatusCode == http.StatusBadRequest && upstreamCode == "invalid_encrypted_content" {
				var decoded map[string]any
				if decodeErr := json.Unmarshal(body, &decoded); decodeErr != nil {
					return nil, fmt.Errorf("decode invalid_encrypted_content retry body: %w", decodeErr)
				}
				if trimOpenAIEncryptedReasoningItems(decoded) {
					body, err = json.Marshal(decoded)
					if err != nil {
						return nil, fmt.Errorf("serialize invalid_encrypted_content retry body: %w", err)
					}
					httpInvalidEncryptedContentRetryTried = true
					rejectedFieldRetryState.remember(body)
					continue
				}
			}
			if retryBody, _, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(resp.StatusCode, body, respBody); retryErr != nil {
				return nil, fmt.Errorf("normalize rejected Responses field retry body: %w", retryErr)
			} else if changed && rejectedFieldRetryState.Allow(retryBody) {
				body = retryBody
				continue
			}
			if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendRuntimeOpsUpstreamError(input.Exchange, OpsUpstreamErrorEvent{
					Platform: input.Account.Platform, AccountID: input.Account.ID, AccountName: input.Account.Name,
					UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: runtimeHeaderValue(resp.Header, "x-request-id"),
					Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
				})
				shouldDisable := s.handleFailoverSideEffects(ctx, resp, input.Account, respBody, upstreamModel)
				return nil, newOpenAIUpstreamFailoverError(
					resp.StatusCode, resp.Header, respBody, upstreamMsg,
					!shouldDisable && input.Account.IsPoolMode() &&
						(input.Account.IsPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)),
				)
			}
			if input.ResponseFormat == openAIHTTPResponseFormatChatCompletions {
				return s.handleChatCompletionsErrorResponseExchange(ctx, input.Exchange, input.Account, resp, respBody, requestModel)
			}
			if input.ResponseFormat == openAIHTTPResponseFormatAnthropic {
				return s.handleAnthropicErrorResponseExchange(ctx, input.Exchange, input.Account, resp, respBody, requestModel)
			}
			return s.handleResponsesErrorResponseExchange(ctx, input.Exchange, input.Account, resp, respBody, requestModel)
		}

		defer func() { _ = resp.Body.Close() }()
		var responseResult *openaiStreamingResult
		var nonStreamResult *openaiNonStreamingResult
		if stream {
			if input.ResponseFormat == openAIHTTPResponseFormatChatCompletions {
				responseResult, err = s.handleOpenAIChatStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, input.StartTime, requestModel, input.BillingModel, upstreamModel)
			} else if input.ResponseFormat == openAIHTTPResponseFormatAnthropic {
				responseResult, err = s.handleOpenAIAnthropicStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, input.StartTime, requestModel, input.BillingModel, upstreamModel)
			} else {
				responseResult, err = s.handleOpenAIStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, input.StartTime, requestModel, upstreamModel, input.ReasoningEffort)
			}
		} else {
			if input.ResponseFormat == openAIHTTPResponseFormatChatCompletions {
				nonStreamResult, err = s.handleOpenAIChatNonStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, input.StartTime, requestModel, input.BillingModel, upstreamModel)
			} else if input.ResponseFormat == openAIHTTPResponseFormatAnthropic {
				nonStreamResult, err = s.handleOpenAIAnthropicNonStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, input.StartTime, requestModel, input.BillingModel, upstreamModel)
			} else {
				nonStreamResult, err = s.handleOpenAINonStreamingResponseExchange(ctx, resp, input.Exchange, input.Account, requestModel, upstreamModel)
			}
		}
		if err != nil {
			return nil, err
		}
		var usage *OpenAIUsage
		responseID := ""
		imageCount := 0
		var imageOutputSizes []string
		var firstTokenMs *int
		if stream {
			usage = responseResult.usage
			responseID = strings.TrimSpace(responseResult.responseID)
			imageCount = responseResult.imageCount
			imageOutputSizes = responseResult.imageOutputSizes
			firstTokenMs = responseResult.firstTokenMs
		} else {
			usage = nonStreamResult.usage
			responseID = strings.TrimSpace(nonStreamResult.responseID)
			imageCount = nonStreamResult.imageCount
			imageOutputSizes = nonStreamResult.imageOutputSizes
		}
		if usage == nil {
			usage = &OpenAIUsage{}
		}
		s.bindHTTPResponseAccountExchange(ctx, input.Exchange, input.Account, responseID)
		if input.Account.Type == AccountTypeOAuth && !input.Account.IsShadow() {
			if snapshot := ParseCodexRateLimitHeaders(resp.Header); snapshot != nil {
				s.updateCodexUsageSnapshot(ctx, input.Account.ID, snapshot)
			}
		}
		var reasoningEffort *string
		if strings.TrimSpace(input.ReasoningEffort) != "" {
			value := input.ReasoningEffort
			reasoningEffort = &value
		}
		result := &OpenAIForwardResult{
			RequestID: requestID, ResponseID: responseID, Usage: *usage,
			Model: requestModel, BillingModel: input.BillingModel, UpstreamModel: upstreamModel,
			ServiceTier: extractOpenAIServiceTierFromBody(body), ReasoningEffort: reasoningEffort,
			Stream: stream, OpenAIWSMode: false, Duration: time.Since(input.StartTime), FirstTokenMs: firstTokenMs,
			UpstreamEndpoint: endpoint, ResponseHeaders: resp.Header.Clone(),
		}
		if imageCount > 0 {
			result.ImageCount = imageCount
			result.ImageSize = input.ImageSizeTier
			result.ImageInputSize = input.ImageInputSize
			result.ImageOutputSizes = imageOutputSizes
			result.BillingModel = input.ImageBillingModel
		}
		return result, nil
	}
}

func (s *OpenAIGatewayService) bindHTTPResponseAccountExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	responseID string,
) {
	if s == nil || exchange == nil || account == nil || account.ID <= 0 || strings.TrimSpace(responseID) == "" {
		return
	}
	store := s.getOpenAIWSStateStore()
	if store == nil {
		return
	}
	platformID := int64(0)
	if schedulingID := PlatformSchedulingID(ctx); schedulingID != nil {
		platformID = *schedulingID
	}
	logOpenAIWSBindResponseAccountWarn(platformID, account.ID, responseID, store.BindResponseAccount(ctx, platformID, responseID, account.ID, s.openAIWSResponseStickyTTL()))
}
