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
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
)

// handleOpenAIChatNonStreamingResponseExchange converts the upstream Responses
// SSE terminal event into a Chat Completions JSON response. The upstream is
// deliberately still streamed so OAuth accounts keep their existing protocol;
// only the client-facing envelope is buffered here.
func (s *OpenAIGatewayService) handleOpenAIChatNonStreamingResponseExchange(
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
	finalResponse, usage, accumulator, err := s.readOpenAICompatBufferedTerminal(resp, "openai chat runtime", requestID)
	if err != nil {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		return nil, err
	}
	if finalResponse == nil {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "api_error", "Upstream stream ended without a terminal response event")
		return nil, errors.New("upstream stream ended without terminal event")
	}
	if strings.TrimSpace(finalResponse.Status) == "failed" {
		return nil, s.handleOpenAIChatResponsesFailureExchange(ctx, exchange, account, resp, finalResponse, usage, false)
	}
	if accumulator != nil {
		accumulator.SupplementResponseOutput(finalResponse)
	}
	encoded, err := json.Marshal(apicompat.ResponsesToChatCompletions(finalResponse, originalModel))
	if err != nil {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "api_error", "Failed to encode upstream response")
		return nil, fmt.Errorf("marshal chat response: %w", err)
	}
	writeRuntimeJSONResponse(exchange, resp, http.StatusOK, encoded, s.responseHeaderFilter)
	return &openaiNonStreamingResult{
		OpenAIUsage: &usage,
		usage:       &usage,
		responseID:  strings.TrimSpace(finalResponse.ID),
	}, nil
}

// handleOpenAIChatStreamingResponseExchange converts Responses SSE events into
// Chat Completions chunks while continuing to consume the upstream stream after
// a client disconnect so usage and failover classification remain accurate.
func (s *OpenAIGatewayService) handleOpenAIChatStreamingResponseExchange(
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
	state := apicompat.NewResponsesEventToChatState()
	state.Model = originalModel
	state.IncludeUsage = true
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
		responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
		exchange.Header().Set("Content-Type", "text/event-stream")
		exchange.Header().Set("Cache-Control", "no-cache")
		exchange.Header().Set("Connection", "keep-alive")
		exchange.Header().Set("X-Accel-Buffering", "no")
		exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, http.StatusOK)
		exchange.WriteHeader(http.StatusOK)
		streamStarted = true
	}

	emit := func(chunks []apicompat.ChatCompletionsChunk) {
		if len(chunks) == 0 || clientDisconnected {
			return
		}
		writeHeaders()
		for _, chunk := range chunks {
			encoded, err := apicompat.ChatChunkToSSE(chunk)
			if err != nil {
				continue
			}
			if _, err := exchange.Write([]byte(encoded)); err != nil {
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
			terminalErr = s.handleOpenAIChatResponsesFailureExchange(ctx, exchange, account, resp, event.Response, usage, true)
			return true
		}
		emit(apicompat.ResponsesEventToChatChunks(&event, state))
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
		logCCStreamMissingDoneSentinel("openai chat runtime", requestID)
	}
	if !state.Finalized {
		emit(apicompat.FinalizeResponsesChatStream(state))
	}
	if !clientDisconnected {
		writeHeaders()
		if _, err := exchange.Write([]byte("data: [DONE]\n\n")); err != nil {
			clientDisconnected = true
		} else {
			exchange.Flush()
		}
	}
	return &openaiStreamingResult{
		usage:        &usage,
		firstTokenMs: firstTokenMs,
		responseID:   responseID,
	}, nil
}

func (s *OpenAIGatewayService) handleOpenAIChatResponsesFailureExchange(
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
			if !exchange.Written() {
				writeRuntimeOpenAIChatStreamHeaders(exchange)
			}
			_, _ = exchange.Write([]byte(runtimeChatStreamErrorSSE("cyber_policy", cyberMsg)))
			_, _ = exchange.Write([]byte("data: [DONE]\n\n"))
			exchange.Flush()
		} else {
			writeRuntimeOpenAIChatError(exchange, http.StatusBadRequest, "invalid_request_error", cyberMsg)
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
			if !exchange.Written() {
				writeRuntimeOpenAIChatStreamHeaders(exchange)
			}
			_, _ = exchange.Write([]byte(runtimeChatStreamErrorSSE(errType, errMsg)))
			_, _ = exchange.Write([]byte("data: [DONE]\n\n"))
			exchange.Flush()
		} else {
			exchange.SetState(ResponseCommittedKey, true)
			writeRuntimeOpenAIChatError(exchange, status, errType, errMsg)
		}
		return fmt.Errorf("upstream response failed (passthrough): %s", errMsg)
	}
	if streaming {
		if !exchange.Written() {
			writeRuntimeOpenAIChatStreamHeaders(exchange)
		}
		_, _ = exchange.Write([]byte(runtimeChatStreamErrorSSE("upstream_error", message)))
		_, _ = exchange.Write([]byte("data: [DONE]\n\n"))
		exchange.Flush()
	} else {
		writeRuntimeOpenAIChatError(exchange, http.StatusBadGateway, "upstream_error", message)
	}
	return fmt.Errorf("upstream response failed: %s", message)
}

func writeRuntimeOpenAIChatStreamHeaders(exchange gatewayruntime.HTTPExchange) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "text/event-stream")
	exchange.Header().Set("Cache-Control", "no-cache")
	exchange.Header().Set("Connection", "keep-alive")
	exchange.Header().Set("X-Accel-Buffering", "no")
	exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, http.StatusOK)
	exchange.WriteHeader(http.StatusOK)
}

func runtimeChatStreamErrorSSE(code, message string) string {
	payload, err := json.Marshal(map[string]any{
		"error": map[string]string{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	})
	if err != nil {
		return "data: {\"error\":{\"type\":\"invalid_request_error\",\"code\":\"upstream_error\",\"message\":\"upstream error\"}}\n\n"
	}
	return "data: " + string(payload) + "\n\n"
}
