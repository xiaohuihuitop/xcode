//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestHandleOpenAINonStreamingResponseExchange(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_runtime_1","model":"gpt-5.6","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`)),
	}
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	result, err := svc.handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, account, "gpt-5.6", "gpt-5.6",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 3, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.Equal(t, "resp_runtime_1", result.responseID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), `"total_tokens":5`)
}

func TestHandleOpenAINonStreamingResponseExchangeUsesFinalContentType(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_content_type","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{Security: config.SecurityConfig{ResponseHeaders: config.ResponseHeaderConfig{Enabled: true}}}}

	_, err := svc.handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "gpt-5.6", "gpt-5.6",
	)

	require.NoError(t, err)
	require.Equal(t, "application/json", exchange.header.Get("Content-Type"))
}

func TestHandleNonStreamingResponseUsesRuntimeExchangeWhenAvailable(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_runtime_bridge","model":"gpt-5.6","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}
	svc := &OpenAIGatewayService{}

	var result *openaiNonStreamingResult
	err := withRuntimeGinContext(context.Background(), exchange, 0, func(c runtimeGinContext) error {
		var err error
		result, err = svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "gpt-5.6", "gpt-5.6")
		return err
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "resp_runtime_bridge", result.responseID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.JSONEq(t, `{"id":"resp_runtime_bridge","model":"gpt-5.6","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, string(exchange.body))
}

func TestHandleStreamingResponseUsesRuntimeStreamErrorHelpers(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"stream-runtime-1"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.failed\n" +
				`data: {"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"slow down"}}}` + "\n\n",
		)),
	}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	var streamErr error
	err := withRuntimeGinContext(context.Background(), exchange, 0, func(c runtimeGinContext) error {
		_, streamErr = svc.handleStreamingResponseWithReasoning(
			context.Background(), resp, c, account, time.Now(), "gpt-5.6", "gpt-5.6", "",
		)
		return streamErr
	})

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, streamErr, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Equal(t, http.StatusTooManyRequests, exchange.state[OpsUpstreamStatusCodeKey])
	events, ok := exchange.state[OpsUpstreamErrorsKey].([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "stream-runtime-1", events[0].UpstreamRequestID)
}

func TestHandleOpenAIStreamingResponseExchangeWritesSSEAndUsage(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_stream_1","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}` + "\n\n",
		)),
	}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	result, err := svc.handleOpenAIStreamingResponseExchange(
		context.Background(), resp, exchange,
		&Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		time.Now(), "gpt-5.6", "gpt-5.6", "",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 4, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	require.Equal(t, "resp_stream_1", result.responseID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "text/event-stream", exchange.header.Get("Content-Type"))
	require.Contains(t, string(exchange.body), "response.completed")
}

func TestHandleOpenAIStreamingResponseExchangeUsesFirstOutputTimeout(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	reader, writer := io.Pipe()
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"stream-timeout-1"}},
		Body:       reader,
	}
	svc := &OpenAIGatewayService{
		cfg:           &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 30}},
		toolCorrector: NewCodexToolCorrector(),
	}
	_, err := svc.handleOpenAIStreamingResponseExchange(
		context.Background(), resp, exchange,
		&Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		time.Now().Add(-31*time.Second), "gpt-5.6", "gpt-5.6", "",
	)

	require.Error(t, err)
	var timeoutErr *UpstreamFailoverError
	require.ErrorAs(t, err, &timeoutErr)
	require.Equal(t, http.StatusGatewayTimeout, timeoutErr.StatusCode)
	events, ok := exchange.state[OpsUpstreamErrorsKey].([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "first_output_timeout", events[0].Kind)
	require.Equal(t, "stream-timeout-1", events[0].UpstreamRequestID)
}

func TestHandleStreamingResponseSyncsGinNamespaceStateBeforeExchangeCore(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.output_item.done\n" +
				`data: {"type":"response.output_item.done","item":{"type":"function_call","name":"tools__run","arguments":"{}"}}` + "\n\n" +
				"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_namespace_stream","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n",
		)),
	}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	err := withRuntimeGinContext(context.Background(), exchange, 0, func(c runtimeGinContext) error {
		c.Set(openAIResponsesNamespaceNamesContextKey, map[string]apicompat.ResponsesNamespaceName{
			"tools__run": {Namespace: "tools", Name: "run"},
		})
		_, err := svc.handleStreamingResponseWithReasoning(
			context.Background(), resp, c, account, time.Now(), "gpt-5.6", "gpt-5.6", "",
		)
		return err
	})

	require.NoError(t, err)
	require.Contains(t, string(exchange.body), `"name":"run"`)
	require.Contains(t, string(exchange.body), `"namespace":"tools"`)
}

func TestHandleOpenAINonStreamingResponseExchangeConvertsSSE(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_sse_1\",\"model\":\"mapped-model\",\"output\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":1,\"total_tokens\":5}}}\n\n")),
	}
	svc := &OpenAIGatewayService{}

	result, err := svc.handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "client-model", "mapped-model",
	)

	require.NoError(t, err)
	require.Equal(t, 4, result.usage.InputTokens)
	require.Equal(t, 1, result.usage.OutputTokens)
	require.Equal(t, "resp_sse_1", result.responseID)
	require.Equal(t, "application/json; charset=utf-8", exchange.header.Get("Content-Type"))
	require.JSONEq(t, `{"id":"resp_sse_1","model":"client-model","output":[],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`, string(exchange.body))
}

func TestHandleOpenAINonStreamingResponseExchangeRestoresNamespace(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	exchange.SetState(openAIResponsesNamespaceNamesContextKey, map[string]apicompat.ResponsesNamespaceName{
		"tools__run": {Namespace: "tools", Name: "run"},
	})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_ns_1","model":"gpt-5.6","output":[{"type":"function_call","name":"tools__run","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}

	result, err := (&OpenAIGatewayService{}).handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "gpt-5.6", "gpt-5.6",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.JSONEq(t, `{"id":"resp_ns_1","model":"gpt-5.6","output":[{"type":"function_call","name":"run","namespace":"tools","arguments":"{}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, string(exchange.body))
}

func TestHandleOpenAINonStreamingResponseExchangeBridgesCompactStream(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	exchange.request.URL.Path = "/v1/responses/compact"
	exchange.SetState(openAICompactClientStreamKey, true)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_compact_1","model":"gpt-5.6","output":[{"type":"compaction","id":"cmp_1","encrypted_content":"opaque"}],"usage":{"input_tokens":2,"output_tokens":0,"total_tokens":2}}`)),
	}

	result, err := (&OpenAIGatewayService{}).handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "gpt-5.6", "gpt-5.6",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "text/event-stream", exchange.header.Get("Content-Type"))
	require.Contains(t, string(exchange.body), "event: response.output_item.done")
	require.Contains(t, string(exchange.body), "event: response.completed")
	require.Contains(t, string(exchange.body), `"type":"compaction"`)
	require.Equal(t, 1, exchange.flushCount)
}

func TestHandleOpenAINonStreamingResponseExchangeWritesFailedSSEAsError(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"upstream denied\"}}}\n\n")),
	}

	result, err := (&OpenAIGatewayService{}).handleOpenAINonStreamingResponseExchange(
		context.Background(), resp, exchange, &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "gpt-5.6", "gpt-5.6",
	)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadGateway, exchange.status)
	require.Contains(t, string(exchange.body), "upstream denied")
	require.Equal(t, http.StatusBadGateway, exchange.state[OpsUpstreamStatusCodeKey])
	require.Equal(t, true, exchange.state[ResponseCommittedKey])
}
