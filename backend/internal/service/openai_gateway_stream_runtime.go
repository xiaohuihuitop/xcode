package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// openAIStreamResponseSink is the transport-neutral response surface used by
// the streaming state machine. Gin and gatewayruntime each provide a thin
// adapter; the state machine itself only writes bytes and flushes events.
type openAIStreamResponseSink interface {
	Header() http.Header
	WriteHeader(int)
	Write([]byte) (int, error)
	Flush()
	Written() bool
}

type openAIStreamHooks struct {
	clientOutputStarted func(bool) bool
	failoverError       func(*Account, bool, string, []byte, string, ...http.Header) *UpstreamFailoverError
	recordUpstreamError func(*Account, bool, string, string, []byte, string) string
	applyPassthrough    func(string, []byte, string) (int, string, string, bool)
	firstOutputTimeout  func(*Account, time.Time, string, string, time.Duration, string, http.Header) *UpstreamFailoverError
	markCyberPolicy     func(CyberPolicyMark)
	markResponseCommit  func()
	restorePayload      func([]byte) ([]byte, error)
}

type runtimeOpenAIStreamSink struct {
	exchange gatewayruntime.HTTPExchange
	status   int
}

func newRuntimeOpenAIStreamSink(exchange gatewayruntime.HTTPExchange) *runtimeOpenAIStreamSink {
	return &runtimeOpenAIStreamSink{exchange: exchange, status: http.StatusOK}
}

func (s *runtimeOpenAIStreamSink) Header() http.Header {
	if s == nil || s.exchange == nil {
		return make(http.Header)
	}
	return s.exchange.Header()
}

func (s *runtimeOpenAIStreamSink) WriteHeader(status int) {
	if s == nil || s.exchange == nil || status <= 0 || s.exchange.Written() {
		return
	}
	s.status = status
	s.exchange.WriteHeader(status)
}

func (s *runtimeOpenAIStreamSink) Write(body []byte) (int, error) {
	if s == nil || s.exchange == nil {
		return 0, io.ErrClosedPipe
	}
	if !s.exchange.Written() {
		s.exchange.WriteHeader(s.status)
	}
	return s.exchange.Write(body)
}

func (s *runtimeOpenAIStreamSink) Flush() {
	if s != nil && s.exchange != nil {
		s.exchange.Flush()
	}
}

func (s *runtimeOpenAIStreamSink) Written() bool {
	return s != nil && s.exchange != nil && s.exchange.Written()
}

func writeOpenAIStreamJSONError(sink openAIStreamResponseSink, status int, errType, message string) error {
	if sink == nil {
		return ErrRuntimeExchangeUnavailable
	}
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{"type": errType, "message": message},
	})
	if err != nil {
		return err
	}
	sink.Header().Set("Content-Type", "application/json; charset=utf-8")
	sink.WriteHeader(status)
	_, err = sink.Write(body)
	return err
}

func newOpenAIStreamExchangeHooks(
	s *OpenAIGatewayService,
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
) openAIStreamHooks {
	return openAIStreamHooks{
		clientOutputStarted: func(localStarted bool) bool {
			return openAIStreamClientOutputStartedExchange(exchange, localStarted)
		},
		failoverError: func(account *Account, passthrough bool, upstreamRequestID string, payload []byte, message string, responseHeaders ...http.Header) *UpstreamFailoverError {
			return s.newOpenAIStreamFailoverErrorExchange(exchange, account, passthrough, upstreamRequestID, payload, message, responseHeaders...)
		},
		recordUpstreamError: func(account *Account, passthrough bool, upstreamRequestID, kind string, payload []byte, message string) string {
			return s.recordOpenAIStreamUpstreamErrorExchange(exchange, account, passthrough, upstreamRequestID, kind, payload, message)
		},
		applyPassthrough: func(platform string, payload []byte, failedMessage string) (int, string, string, bool) {
			return applyOpenAIStreamFailedErrorPassthroughRuleExchange(exchange, platform, payload, failedMessage)
		},
		firstOutputTimeout: func(account *Account, startTime time.Time, originalModel, reasoningEffort string, timeout time.Duration, phase string, responseHeaders http.Header) *UpstreamFailoverError {
			return s.newOpenAIFirstOutputTimeoutErrorExchange(ctx, exchange, account, startTime, originalModel, reasoningEffort, timeout, phase, responseHeaders)
		},
		markCyberPolicy: func(mark CyberPolicyMark) {
			markRuntimeCyberPolicy(exchange, mark)
		},
		markResponseCommit: func() {
			if exchange != nil {
				exchange.SetState(ResponseCommittedKey, true)
			}
		},
		restorePayload: func(payload []byte) ([]byte, error) {
			return restoreOpenAIResponsesNamespacePayloadFromExchange(exchange, payload)
		},
	}
}

func (s *OpenAIGatewayService) handleOpenAIStreamingResponseExchange(
	ctx context.Context,
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	startTime time.Time,
	originalModel string,
	mappedModel string,
	reasoningEffort string,
) (*openaiStreamingResult, error) {
	if exchange == nil || resp == nil || resp.Body == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
		if request := exchange.Request(); request != nil {
			ctx = request.Context()
		}
	}
	return s.handleStreamingResponseWithReasoningCore(
		ctx,
		resp,
		newRuntimeOpenAIStreamSink(exchange),
		newOpenAIStreamExchangeHooks(s, ctx, exchange),
		account,
		startTime,
		originalModel,
		mappedModel,
		reasoningEffort,
	)
}

// openAIStreamClientOutputStartedExchange mirrors the legacy semantic-output
// check without inspecting a Gin response writer. The caller's local flag is
// authoritative; exchange.Written covers bytes already committed downstream.
func openAIStreamClientOutputStartedExchange(exchange gatewayruntime.HTTPExchange, localStarted bool) bool {
	if localStarted {
		return true
	}
	return exchange != nil && exchange.Written()
}

func (s *OpenAIGatewayService) recordOpenAIStreamUpstreamErrorExchange(
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	passthrough bool,
	upstreamRequestID string,
	kind string,
	payload []byte,
	message string,
) string {
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = "OpenAI upstream response failed"
	}
	statusCode := openAIStreamFailureStatus(payload, message)
	detail := ""
	if len(payload) > 0 && s != nil && s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		detail = truncateString(string(payload), maxBytes)
	}
	if exchange == nil {
		return message
	}
	setRuntimeOpsUpstreamError(exchange, statusCode, message, detail)
	event := OpsUpstreamErrorEvent{
		Platform:           PlatformOpenAI,
		UpstreamStatusCode: statusCode,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Passthrough:        passthrough,
		Kind:               kind,
		Message:            message,
		Detail:             detail,
	}
	if account != nil {
		event.Platform = account.Platform
		event.AccountID = account.ID
		event.AccountName = account.Name
	}
	appendRuntimeOpsUpstreamError(exchange, event)
	return message
}

func (s *OpenAIGatewayService) newOpenAIStreamFailoverErrorExchange(
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	passthrough bool,
	upstreamRequestID string,
	payload []byte,
	message string,
	responseHeaders ...http.Header,
) *UpstreamFailoverError {
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = "OpenAI stream disconnected before completion"
	}
	statusCode := openAIStreamFailureStatus(payload, message)
	var headers http.Header
	if len(responseHeaders) > 0 && responseHeaders[0] != nil {
		headers = responseHeaders[0].Clone()
	}
	message = s.recordOpenAIStreamUpstreamErrorExchange(
		exchange, account, passthrough, upstreamRequestID, "failover", payload, message,
	)
	errType := "upstream_error"
	if statusCode == http.StatusTooManyRequests {
		errType = "rate_limit_error"
	}
	body, _ := json.Marshal(map[string]any{
		"error": map[string]string{"type": errType, "message": message},
	})
	failoverErr := &UpstreamFailoverError{
		StatusCode:             statusCode,
		ResponseBody:           body,
		ResponseHeaders:        headers,
		RetryableOnSameAccount: openAIStreamFailedEventRetryableOnSameAccount(account, payload, message),
	}
	if isOpenAIUpstreamCapacityShedEvent(payload) {
		failoverErr.Scope = GatewayFailureScopeRequest
	}
	return failoverErr
}

func applyOpenAIStreamFailedErrorPassthroughRuleExchange(
	exchange gatewayruntime.HTTPExchange,
	platform string,
	payload []byte,
	failedMessage string,
) (status int, errType string, errMsg string, matched bool) {
	ruleBody := openAIStreamFailedEventPassthroughBody(payload, failedMessage)
	upstreamStatus := openAIStreamFailedEventSemanticStatus(payload, failedMessage)
	return applyErrorPassthroughRuleExchange(
		exchange,
		platform,
		upstreamStatus,
		ruleBody,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	)
}

func (s *OpenAIGatewayService) newOpenAIFirstOutputTimeoutErrorExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	startTime time.Time,
	originalModel string,
	reasoningEffort string,
	timeout time.Duration,
	phase string,
	responseHeaders http.Header,
) *UpstreamFailoverError {
	elapsed := time.Since(startTime)
	accountID := int64(0)
	accountPlatform := PlatformOpenAI
	accountName := ""
	if account != nil {
		accountID = account.ID
		accountPlatform = account.Platform
		accountName = account.Name
	}
	logger.LegacyPrintf(
		"service.openai_gateway",
		"OpenAI first output timeout: account=%d model=%s effort=%s phase=%s elapsed=%s limit=%s",
		accountID, originalModel, reasoningEffort, phase, elapsed, timeout,
	)
	requestID := strings.TrimSpace(responseHeaders.Get("x-request-id"))
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
		Platform:           accountPlatform,
		AccountID:          accountID,
		AccountName:        accountName,
		UpstreamStatusCode: http.StatusGatewayTimeout,
		UpstreamRequestID:  requestID,
		Kind:               "first_output_timeout",
		Message:            "OpenAI upstream produced no semantic output before the deadline",
		Detail:             fmt.Sprintf("phase=%s elapsed_ms=%d timeout_ms=%d", phase, elapsed.Milliseconds(), timeout.Milliseconds()),
	})
	if s != nil && s.rateLimitService != nil && account != nil {
		s.rateLimitService.HandleStreamTimeout(ctx, account, originalModel)
	}
	return &UpstreamFailoverError{
		StatusCode:               http.StatusGatewayTimeout,
		ResponseBody:             []byte(`{"error":{"type":"first_output_timeout","message":"Upstream produced no output before the deadline"}}`),
		ResponseHeaders:          responseHeaders.Clone(),
		SafeToFailoverAfterWrite: true,
	}
}
