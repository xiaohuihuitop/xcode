package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// handleOpenAINonStreamingResponseExchange handles a non-streaming OpenAI
// response without creating a Gin context. It keeps SSE conversion, namespace
// restoration and compact-stream bridging inside the same exchange boundary.
func (s *OpenAIGatewayService) handleOpenAINonStreamingResponseExchange(
	_ context.Context,
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	originalModel string,
	mappedModel string,
) (*openaiNonStreamingResult, error) {
	if exchange == nil || resp == nil || resp.Body == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		return nil, err
	}
	if isEventStreamResponse(resp.Header) || bodyHasSSEFraming(body) {
		return s.handleOpenAISSEToJSONResponseExchange(resp, exchange, body, originalModel, mappedModel)
	}
	if account != nil && account.IsGrok() && isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		body, err = convertGrokResponseToOpenAICompact(body)
		if err != nil {
			return nil, fmt.Errorf("convert Grok compact response: %w", err)
		}
	}

	usageValue, usageOK := extractOpenAIUsageFromJSONBytes(body)
	if !usageOK {
		return nil, fmt.Errorf("parse response: invalid json response")
	}
	if originalModel != mappedModel {
		body = s.replaceModelInResponseBody(body, mappedModel, originalModel)
	}
	var restoreErr error
	body, restoreErr = restoreGrokResponsesClientToolPayloadFromExchange(exchange, body)
	if restoreErr != nil {
		return nil, fmt.Errorf("restore Grok Responses client tool response: %w", restoreErr)
	}
	body, restoreErr = restoreOpenAIResponsesNamespacePayloadFromExchange(exchange, body)
	if restoreErr != nil {
		return nil, fmt.Errorf("restore OpenAI namespace response: %w", restoreErr)
	}

	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	contentType := "application/json"
	if s.cfg != nil && !s.cfg.Security.ResponseHeaders.Enabled {
		if upstreamType := resp.Header.Get("Content-Type"); upstreamType != "" {
			contentType = upstreamType
		}
	}
	exchange.Header().Set("Content-Type", contentType)
	if !writeOpenAICompactSSEBridgeExchange(exchange, resp.StatusCode, body) {
		exchange.WriteHeader(resp.StatusCode)
		if _, err := exchange.Write(body); err != nil {
			return nil, err
		}
	}

	usage := &usageValue
	return &openaiNonStreamingResult{
		OpenAIUsage:      usage,
		usage:            usage,
		responseID:       extractOpenAIResponseIDFromJSONBytes(body),
		imageCount:       countOpenAIResponseImageOutputsFromJSONBytes(body),
		imageOutputSizes: collectOpenAIResponseImageOutputSizesFromJSONBytes(body),
	}, nil
}

func (s *OpenAIGatewayService) handleOpenAISSEToJSONResponseExchange(
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
	body []byte,
	originalModel string,
	mappedModel string,
) (*openaiNonStreamingResult, error) {
	bodyText := string(body)
	finalResponse, ok := extractCodexFinalResponse(bodyText)
	usage := &OpenAIUsage{}
	if ok {
		if parsedUsage, parsed := extractOpenAIUsageFromJSONBytes(finalResponse); parsed {
			*usage = parsedUsage
		}
		if len(gjson.GetBytes(finalResponse, "output").Array()) == 0 {
			if outputJSON, reconstructed := reconstructResponseOutputFromSSE(bodyText); reconstructed {
				if patched, err := sjson.SetRawBytes(finalResponse, "output", outputJSON); err == nil {
					finalResponse = patched
				}
			}
		}
		finalResponse = supplementCompactionItemFromSSEExchange(exchange, finalResponse, bodyText)
		body = finalResponse
		if originalModel != mappedModel {
			body = s.replaceModelInResponseBody(body, mappedModel, originalModel)
		}
		body = s.correctToolCallsInResponseBody(body)
		var err error
		body, err = restoreGrokResponsesClientToolPayloadFromExchange(exchange, body)
		if err != nil {
			return nil, fmt.Errorf("restore Grok Responses client tool response: %w", err)
		}
		body, err = restoreOpenAIResponsesNamespacePayloadFromExchange(exchange, body)
		if err != nil {
			return nil, fmt.Errorf("restore OpenAI namespace response: %w", err)
		}
	} else {
		terminalType, terminalPayload, terminalOK := extractOpenAISSETerminalEvent(bodyText)
		if terminalOK && terminalType == "response.failed" {
			message := extractOpenAISSEErrorMessage(terminalPayload)
			if message == "" {
				message = "Upstream compact response failed"
			}
			return nil, s.writeOpenAINonStreamingProtocolErrorExchange(resp, exchange, message)
		}
		usage = s.parseSSEUsageFromBody(bodyText)
		if originalModel != mappedModel {
			bodyText = s.replaceModelInSSEBody(bodyText, mappedModel, originalModel)
		}
		body = []byte(bodyText)
	}

	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	contentType := "application/json; charset=utf-8"
	if !ok {
		contentType = resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "text/event-stream"
		}
	}
	if !writeOpenAICompactSSEBridgeExchange(exchange, resp.StatusCode, body) {
		exchange.Header().Set("Content-Type", contentType)
		exchange.WriteHeader(resp.StatusCode)
		if _, err := exchange.Write(body); err != nil {
			return nil, err
		}
	}

	return &openaiNonStreamingResult{
		OpenAIUsage:      usage,
		usage:            usage,
		responseID:       extractOpenAIResponseIDFromJSONBytes(body),
		imageCount:       countOpenAIImageOutputsFromSSEBody(bodyText),
		imageOutputSizes: collectOpenAIImageOutputSizesFromSSEBody(bodyText),
	}, nil
}

func restoreOpenAIResponsesNamespacePayloadFromExchange(exchange gatewayruntime.HTTPExchange, payload []byte) ([]byte, error) {
	if exchange == nil || len(payload) == 0 || !json.Valid(payload) {
		return payload, nil
	}
	value, ok := exchange.State(openAIResponsesNamespaceNamesContextKey)
	if !ok {
		return payload, nil
	}
	names, ok := value.(map[string]apicompat.ResponsesNamespaceName)
	if !ok || len(names) == 0 {
		return payload, nil
	}
	restored, changed, err := apicompat.RestoreResponsesNamespaceCalls(payload, names)
	if err != nil {
		return payload, err
	}
	if changed {
		return restored, nil
	}
	return payload, nil
}

func restoreGrokResponsesClientToolPayloadFromExchange(exchange gatewayruntime.HTTPExchange, payload []byte) ([]byte, error) {
	if exchange == nil || len(payload) == 0 || !bytesContains(payload, `"function_call"`) || !json.Valid(payload) {
		return payload, nil
	}
	value, ok := exchange.State(grokResponsesClientToolMappingContextKey)
	if !ok {
		return payload, nil
	}
	mapping, ok := value.(apicompat.ResponsesClientToolMapping)
	if !ok || !hasGrokResponsesClientToolMapping(mapping) {
		return payload, nil
	}
	restored, _, err := apicompat.RestoreResponsesClientToolPayload(payload, mapping)
	return restored, err
}

func supplementCompactionItemFromSSEExchange(exchange gatewayruntime.HTTPExchange, finalResponse []byte, bodyText string) []byte {
	if exchange == nil || !isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return finalResponse
	}
	if len(gjson.GetBytes(finalResponse, "output").Array()) == 0 || responsesOutputHasCompactionItem(finalResponse) {
		return finalResponse
	}
	item, found := findRawCompactionItemFromSSE(bodyText)
	if !found {
		return finalResponse
	}
	patched, err := sjson.SetRawBytes(finalResponse, "output.-1", item)
	if err != nil {
		return finalResponse
	}
	return patched
}

func (s *OpenAIGatewayService) writeOpenAINonStreamingProtocolErrorExchange(resp *http.Response, exchange gatewayruntime.HTTPExchange, message string) error {
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = "Upstream returned an invalid non-streaming response"
	}
	setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, message, "")
	if writeOpenAICompactSSEFailureExchange(exchange, http.StatusBadGateway, "upstream_error", message) {
		return fmt.Errorf("non-streaming openai protocol error: %s", message)
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	exchange.Header().Set("Content-Type", "application/json; charset=utf-8")
	exchange.WriteHeader(http.StatusBadGateway)
	exchange.SetState(ResponseCommittedKey, true)
	payload, err := json.Marshal(map[string]any{
		"error": map[string]string{"type": "upstream_error", "message": message},
	})
	if err != nil {
		return err
	}
	if _, err := exchange.Write(payload); err != nil {
		return err
	}
	return fmt.Errorf("non-streaming openai protocol error: %s", message)
}

func bytesContains(body []byte, value string) bool {
	return len(body) > 0 && bytes.Contains(body, []byte(value))
}

func writeOpenAICompactSSEBridgeExchange(exchange gatewayruntime.HTTPExchange, statusCode int, finalResponse []byte) bool {
	if exchange == nil {
		return false
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices || exchange.Written() {
		return false
	}
	value, ok := exchange.State(openAICompactClientStreamKey)
	wantsStream, _ := value.(bool)
	if !ok || !wantsStream {
		return false
	}
	payload, valid := buildOpenAICompactSSEPayload(finalResponse)
	if !valid {
		return false
	}
	exchange.Header().Set("Content-Type", "text/event-stream")
	exchange.Header().Set("Cache-Control", "no-cache")
	exchange.Header().Set("Connection", "keep-alive")
	exchange.Header().Set("X-Accel-Buffering", "no")
	exchange.WriteHeader(statusCode)
	exchange.SetState(ResponseCommittedKey, true)
	_, _ = exchange.Write(payload)
	exchange.Flush()
	return true
}

func writeOpenAICompactSSEFailureExchange(exchange gatewayruntime.HTTPExchange, statusCode int, errType, message string) bool {
	if exchange == nil {
		return false
	}
	value, ok := exchange.State(openAICompactClientStreamKey)
	wantsStream, _ := value.(bool)
	if !ok || !wantsStream {
		return false
	}
	payload, err := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id":     "resp_" + strings.ReplaceAll(newUUID(), "-", ""),
			"object": "response",
			"status": "failed",
			"output": []any{},
			"error": map[string]any{
				"code":    errType,
				"message": message,
			},
		},
	})
	if err != nil {
		return false
	}
	exchange.Header().Set("Content-Type", "text/event-stream")
	exchange.WriteHeader(statusCode)
	exchange.SetState(ResponseCommittedKey, true)
	_, _ = exchange.Write([]byte("event: response.failed\ndata: "))
	_, _ = exchange.Write(payload)
	_, _ = exchange.Write([]byte("\n\n"))
	exchange.Flush()
	return true
}

func newUUID() string {
	return uuid.NewString()
}
