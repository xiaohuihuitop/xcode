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
)

// handleOpenAIAnthropicNonStreamingResponseExchange converts the Responses
// SSE terminal event into an Anthropic Messages JSON response. The upstream
// remains streamed so OAuth protocol requirements are unchanged.
func (s *OpenAIGatewayService) handleOpenAIAnthropicNonStreamingResponseExchange(
	ctx context.Context,
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	startTime time.Time,
	originalModel string,
	billingModel string,
	upstreamModel string,
) (*openaiNonStreamingResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	finalResponse, usage, accumulator, err := s.readOpenAICompatBufferedTerminal(resp, "openai messages runtime", requestID)
	if err != nil {
		return nil, newOpenAICompatBufferedReadFailoverError(err)
	}
	if finalResponse == nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
		return nil, errors.New("upstream stream ended without terminal event")
	}
	if strings.TrimSpace(finalResponse.Status) == "failed" {
		return nil, s.handleOpenAIAnthropicResponsesFailureExchange(ctx, exchange, account, resp, finalResponse, usage, false)
	}
	if accumulator != nil {
		accumulator.SupplementResponseOutput(finalResponse)
	}
	encoded, err := json.Marshal(apicompat.ResponsesToAnthropic(finalResponse, originalModel))
	if err != nil {
		writeRuntimeAnthropicError(exchange, http.StatusBadGateway, "api_error", "Failed to encode upstream response")
		return nil, fmt.Errorf("marshal anthropic response: %w", err)
	}
	writeRuntimeJSONResponse(exchange, resp, http.StatusOK, encoded, s.responseHeaderFilter)
	return &openaiNonStreamingResult{
		OpenAIUsage: &usage,
		usage:       &usage,
		responseID:  strings.TrimSpace(finalResponse.ID),
	}, nil
}

// handleOpenAIAnthropicStreamingResponseExchange converts Responses SSE
// events to Anthropic SSE events while preserving terminal usage semantics.
func (s *OpenAIGatewayService) handleOpenAIAnthropicStreamingResponseExchange(
	ctx context.Context,
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	startTime time.Time,
	originalModel string,
	billingModel string,
	upstreamModel string,
) (*openaiStreamingResult, error) {
	requestID := runtimeHeaderValue(resp.Header, "x-request-id")
	state := apicompat.NewResponsesEventToAnthropicState()
	state.Model = originalModel
	usage := OpenAIUsage{}
	firstTokenMs := (*int)(nil)
	responseID := ""
	clientDisconnected := false
	streamStarted := false
	sawDone := false
	var terminalErr error

	writeHeaders := func() {
		if streamStarted || exchange == nil || exchange.Written() {
			return
		}
		writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
		streamStarted = true
	}
	emit := func(events []apicompat.AnthropicStreamEvent) {
		if len(events) == 0 || clientDisconnected {
			return
		}
		writeHeaders()
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
		if !clientDisconnected {
			exchange.Flush()
		}
	}

	process := func(payload []byte) bool {
		var event apicompat.ResponsesStreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return false
		}
		if firstTokenMs == nil {
			elapsed := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &elapsed
		}
		if event.Response != nil {
			responseID = strings.TrimSpace(event.Response.ID)
			if event.Response.Usage != nil {
				usage = copyOpenAIUsageFromResponsesUsage(event.Response.Usage)
			}
		}
		if event.Usage != nil {
			usage = copyOpenAIUsageFromResponsesUsage(event.Usage)
		}
		if strings.TrimSpace(event.Type) == "response.failed" {
			terminalErr = s.handleOpenAIAnthropicResponsesFailureExchange(ctx, exchange, account, resp, event.Response, usage, true)
			return true
		}
		emit(apicompat.ResponsesEventToAnthropicEvents(&event, state))
		return false
	}

	scanner := s.newUpstreamSSEScanner(resp.Body)
	var parser openAICompatSSEFrameParser
	for scanner.Scan() {
		line := scanner.Text()
		if isOpenAICompatDoneSentinelLine(line) {
			sawDone = true
			break
		}
		frame, ok := parser.AddLine(line)
		if !ok {
			continue
		}
		payload := openAICompatPayloadWithEventType(frame.Data, frame.EventType)
		if process([]byte(payload)) {
			break
		}
	}
	if terminalErr == nil {
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			terminalErr = fmt.Errorf("stream usage incomplete: %w", err)
		}
	}
	if terminalErr == nil {
		if frame, ok := parser.Finish(); ok {
			payload := openAICompatPayloadWithEventType(frame.Data, frame.EventType)
			process([]byte(payload))
		}
	}
	if terminalErr != nil {
		return &openaiStreamingResult{usage: &usage, firstTokenMs: firstTokenMs, responseID: responseID}, terminalErr
	}
	if !sawDone && !clientDisconnected {
		logCCStreamMissingDoneSentinel("openai messages runtime", requestID)
	}
	if !state.MessageStopSent {
		emit(apicompat.FinalizeResponsesAnthropicStream(state))
	}
	return &openaiStreamingResult{
		usage:        &usage,
		firstTokenMs: firstTokenMs,
		responseID:   responseID,
	}, nil
}

func (s *OpenAIGatewayService) handleOpenAIAnthropicResponsesFailureExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	resp *http.Response,
	response *apicompat.ResponsesResponse,
	usage OpenAIUsage,
	streaming bool,
) error {
	var payload []byte
	if response != nil {
		payload, _ = json.Marshal(map[string]any{"type": "response.failed", "response": response})
	}
	message := openAICompatFailedResponseMessage(response)
	if hit, code, cyberMsg := detectOpenAICyberPolicy(payload); hit {
		markRuntimeCyberPolicy(exchange, CyberPolicyMark{
			Code: code, Message: cyberMsg, Body: truncateString(string(payload), 4096),
			UpstreamStatus: http.StatusOK, UpstreamInTok: usage.InputTokens, UpstreamOutTok: usage.OutputTokens,
		})
		if cyberMsg == "" {
			cyberMsg = "Request blocked by upstream cyber-security policy"
		}
		if streaming {
			writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
			_, _ = exchange.Write(runtimeAnthropicStreamErrorSSE("invalid_request_error", cyberMsg))
			exchange.Flush()
		} else {
			writeRuntimeAnthropicError(exchange, http.StatusBadRequest, "invalid_request_error", cyberMsg)
		}
		return errOpenAICyberPolicyForwarded
	}
	if openAIStreamFailedEventShouldFailover(payload, message) {
		return s.newOpenAIStreamFailoverErrorExchange(exchange, account, false, runtimeHeaderValue(resp.Header, "x-request-id"), payload, message, resp.Header)
	}
	message = s.recordOpenAIStreamUpstreamErrorExchange(exchange, account, false, runtimeHeaderValue(resp.Header, "x-request-id"), "http_error", payload, message)
	if status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRuleExchange(exchange, account.Platform, payload, message); matched {
		if errMsg == "" {
			errMsg = message
		}
		if streaming {
			writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
			_, _ = exchange.Write(runtimeAnthropicStreamErrorSSE(errType, errMsg))
			exchange.Flush()
		} else {
			exchange.SetState(ResponseCommittedKey, true)
			writeRuntimeAnthropicError(exchange, status, errType, errMsg)
		}
		return fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
	}
	if streaming {
		writeRuntimeSSEHeaders(exchange, resp, s.responseHeaderFilter)
		_, _ = exchange.Write(runtimeAnthropicStreamErrorSSE("upstream_error", message))
		exchange.Flush()
	} else {
		writeRuntimeAnthropicError(exchange, http.StatusBadGateway, "upstream_error", message)
	}
	return fmt.Errorf("upstream response failed: %s", message)
}

func runtimeAnthropicStreamErrorSSE(errType, message string) []byte {
	payload, err := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
	if err != nil {
		return []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"upstream_error\",\"message\":\"Upstream request failed\"}}\n\n")
	}
	return []byte("event: error\ndata: " + string(payload) + "\n\n")
}
