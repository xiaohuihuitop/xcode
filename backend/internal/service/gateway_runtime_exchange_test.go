//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type runtimeExchangeTestDouble struct {
	request    *http.Request
	header     http.Header
	status     int
	body       []byte
	state      map[string]any
	flushCount int
}

func newRuntimeExchangeTestDouble(t *testing.T, body io.Reader) *runtimeExchangeTestDouble {
	t.Helper()
	return &runtimeExchangeTestDouble{
		request: httptest.NewRequest(http.MethodPost, "/v1/test", body),
		header:  make(http.Header),
		state:   make(map[string]any),
	}
}

func (e *runtimeExchangeTestDouble) Request() *http.Request { return e.request }
func (e *runtimeExchangeTestDouble) Header() http.Header    { return e.header }
func (e *runtimeExchangeTestDouble) WriteHeader(status int) { e.status = status }
func (e *runtimeExchangeTestDouble) Write(body []byte) (int, error) {
	e.body = append(e.body, body...)
	return len(body), nil
}
func (e *runtimeExchangeTestDouble) Flush()                         { e.flushCount++ }
func (e *runtimeExchangeTestDouble) Written() bool                  { return e.status != 0 || len(e.body) > 0 }
func (e *runtimeExchangeTestDouble) Size() int                      { return len(e.body) }
func (e *runtimeExchangeTestDouble) SetState(key string, value any) { e.state[key] = value }
func (e *runtimeExchangeTestDouble) State(key string) (any, bool) {
	value, ok := e.state[key]
	return value, ok
}

var _ gatewayruntime.HTTPExchange = (*runtimeExchangeTestDouble)(nil)

func TestWithRuntimeGinContextCopiesResponseAndState(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)

	err := withRuntimeGinContext(context.Background(), exchange, 42, func(c runtimeGinContext) error {
		gotExchange, ok := runtimeHTTPExchangeFromGinContext(c)
		require.True(t, ok)
		require.Same(t, exchange, gotExchange)
		c.Set("marker", "copied")
		c.JSON(http.StatusAccepted, map[string]string{"ok": "yes"})
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, exchange.status)
	require.JSONEq(t, `{"ok":"yes"}`, string(exchange.body))
	require.Equal(t, "copied", exchange.state["marker"])
}

func TestWithRuntimeGinContextWritesStreamingResponseDirectly(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)

	err := withRuntimeGinContext(context.Background(), exchange, 0, func(c runtimeGinContext) error {
		c.Header("Content-Type", "text/event-stream")
		c.Writer.WriteHeader(http.StatusOK)
		_, err := c.Writer.Write([]byte("data: first\n\n"))
		if err != nil {
			return err
		}
		c.Writer.Flush()
		_, err = c.Writer.Write([]byte("data: done\n\n"))
		return err
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "text/event-stream", exchange.header.Get("Content-Type"))
	require.Equal(t, 1, exchange.flushCount)
	require.Equal(t, "data: first\n\ndata: done\n\n", string(exchange.body))
}

func TestOpenAIRuntimeForwardCopiesActualEndpointToExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = openAIResponsesEndpoint
	upstream := &httpUpstreamRecorder{resp: openAIEndpointMarkerErrorResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	_, err := svc.ForwardRuntime(context.Background(), exchange, account, body, 42)

	require.Error(t, err)
	require.Equal(t, grokChatRawEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, grokChatRawEndpoint, upstream.lastReq.URL.Path)
}

func TestOpenAIRuntimeForwardAsChatCompletionsOverwritesPreviousEndpoint(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	exchange.SetState(openAIUpstreamEndpointContextKey, grokChatRawEndpoint)
	upstream := &httpUpstreamRecorder{resp: openAIEndpointMarkerErrorResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_responses"}

	_, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 42)

	require.Error(t, err)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, openAIResponsesEndpoint, upstream.lastReq.URL.Path)
}

func TestOpenAIRuntimeForwardAsAnthropicUsesChatFallbackMarker(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: openAIEndpointMarkerErrorResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	_, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 42)

	require.Error(t, err)
	require.Equal(t, grokChatRawEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, grokChatRawEndpoint, upstream.lastReq.URL.Path)
}

func TestOpenAIRuntimeChatCompletionsAPIKeyUsesPureExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"runtime-chat-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_runtime","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	result, err := svc.ForwardAsChatCompletionsExchange(context.Background(), exchange, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-chat-rid", result.RequestID)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.JSONEq(t, `{"id":"chatcmpl_runtime","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`, string(exchange.body))
}

func TestOpenAIRuntimeResponsesAPIKeyBufferedReadFailureIsRetryable(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	partial := `{"id":"chatcmpl_partial","model":"gpt-5.4","choices":[]}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"runtime-responses-buffered-read"}},
		Body:       &errTailReader{data: []byte(partial), err: errors.New("simulated responses buffered read failure")},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()

	result, err := svc.ForwardResponsesExchange(context.Background(), exchange, account, body)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, exchange.Written(), "buffered read failures must not commit a partial Responses response")
}

func TestOpenAIRuntimeMessagesAPIKeyBufferedReadFailureIsRetryable(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	partial := `{"id":"chatcmpl_partial","model":"gpt-4o","choices":[]}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"runtime-messages-buffered-read"}},
		Body:       &errTailReader{data: []byte(partial), err: errors.New("simulated messages buffered read failure")},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()

	result, err := svc.ForwardAsAnthropicExchange(context.Background(), exchange, account, body, "")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, exchange.Written(), "buffered read failures must not commit a partial Messages response")
}

func TestOpenAIRuntimeResponsesFallbackRestoresEncryptedReasoningFromCache(t *testing.T) {
	firstBody := []byte(`{"model":"glm-5.2","input":[{"role":"user","content":"inspect"}],"stream":false}`)
	secondExchangeBody := []byte(`{"model":"glm-5.2","input":[{"role":"user","content":"inspect"},{"type":"reasoning","id":"`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"reasoning-cache-first"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_reasoning_cache","model":"glm-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"","reasoning_content":"inspect the repository","tool_calls":[{"id":"call_a","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"reasoning-cache-second"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_reasoning_cache_2","model":"glm-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`)),
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	firstExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(firstBody))
	firstExchange.request.URL.Path = "/v1/responses"
	firstExchange.SetState(openAICompatReasoningAPIKeyIDStateKey, int64(123))

	firstResult, err := svc.ForwardResponsesExchange(context.Background(), firstExchange, account, firstBody)
	require.NoError(t, err)
	require.NotNil(t, firstResult)
	reasoningID := gjson.GetBytes(firstExchange.body, "output.#(type==reasoning).id").String()
	require.NotEmpty(t, reasoningID)

	secondBody := append(secondExchangeBody, []byte(reasoningID+`","encrypted_content":"opaque"},{"type":"function_call","call_id":"call_a","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_a","output":"done"}],"stream":false}`)...)
	secondExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(secondBody))
	secondExchange.request.URL.Path = "/v1/responses"
	secondExchange.SetState(openAICompatReasoningAPIKeyIDStateKey, int64(123))

	secondResult, err := svc.ForwardResponsesExchange(context.Background(), secondExchange, account, secondBody)
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.Equal(t, "inspect the repository", gjson.GetBytes(upstream.bodies[1], "messages.1.reasoning_content").String())
}

func TestOpenAIRuntimeResponsesFallbackStreamingCachesReasoningForLaterTurn(t *testing.T) {
	firstBody := []byte(`{"model":"glm-5.2","input":[{"role":"user","content":"inspect"}],"stream":true}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"reasoning-cache-stream"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"id":"chatcmpl_reasoning_stream","object":"chat.completion.chunk","model":"glm-5.2","choices":[{"index":0,"delta":{"reasoning_content":"inspect the repository"},"finish_reason":null}]}`,
				"",
				`data: {"id":"chatcmpl_reasoning_stream","object":"chat.completion.chunk","model":"glm-5.2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_stream","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
				"",
				"data: [DONE]",
				"",
			}, "\n"))),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"reasoning-cache-stream-second"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_reasoning_stream_2","model":"glm-5.2","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`)),
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	firstExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(firstBody))
	firstExchange.request.URL.Path = "/v1/responses"
	firstExchange.SetState(openAICompatReasoningAPIKeyIDStateKey, int64(456))

	firstResult, err := svc.ForwardResponsesExchange(context.Background(), firstExchange, account, firstBody)
	require.NoError(t, err)
	require.NotNil(t, firstResult)
	var reasoningID string
	for _, line := range strings.Split(string(firstExchange.body), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil && event.Type == "response.output_item.added" && event.Item.Type == "reasoning" {
			reasoningID = event.Item.ID
			break
		}
	}
	require.NotEmpty(t, reasoningID)

	secondBody := []byte(`{"model":"glm-5.2","input":[{"role":"user","content":"inspect"},{"type":"reasoning","id":"` + reasoningID + `","encrypted_content":"opaque"},{"type":"function_call","call_id":"call_stream","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_stream","output":"done"}],"stream":false}`)
	secondExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(secondBody))
	secondExchange.request.URL.Path = "/v1/responses"
	secondExchange.SetState(openAICompatReasoningAPIKeyIDStateKey, int64(456))

	secondResult, err := svc.ForwardResponsesExchange(context.Background(), secondExchange, account, secondBody)
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.Equal(t, "inspect the repository", gjson.GetBytes(upstream.bodies[1], "messages.1.reasoning_content").String())
}

func TestOpenAIRuntimeChatCompletionsOAuthUsesPureExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-oauth-chat-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_runtime_oauth_chat","model":"gpt-5.4","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_runtime_oauth_chat","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","id":"msg_runtime_oauth_chat","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          44,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 77)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, exchange.state["openai_runtime_chat_responses_pure"])
	require.Equal(t, "runtime-oauth-chat-rid", result.RequestID)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "chat.completion", gjson.Get(string(exchange.body), "object").String())
	require.Equal(t, "ok", gjson.Get(string(exchange.body), "choices.0.message.content").String())
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeChatCompletionsOAuthStreamsThroughPureExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-oauth-chat-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_runtime_oauth_chat_stream","model":"gpt-5.4","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_runtime_oauth_chat_stream","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","id":"msg_runtime_oauth_chat_stream","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          45,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 78)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, exchange.state["openai_runtime_chat_responses_pure"])
	require.Equal(t, "runtime-oauth-chat-stream-rid", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.True(t, result.Stream)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), `"object":"chat.completion.chunk"`)
	require.Contains(t, string(exchange.body), `"content":"ok"`)
	require.Contains(t, string(exchange.body), "data: [DONE]")
	require.GreaterOrEqual(t, exchange.flushCount, 1)
}

func TestOpenAIRuntimeChatCompletionsOAuthReturnsFailoverWithoutWritingResponse(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"runtime-oauth-chat-error-rid"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream unavailable"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          46,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 79)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.False(t, exchange.Written())
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Contains(t, string(failoverErr.ResponseBody), "upstream unavailable")
}

func TestOpenAIRuntimeChatCompletionsOAuthBufferedReadFailureIsRetryable(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	partial := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_buffered_read","model":"gpt-5.4","status":"in_progress","output":[]}}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"runtime-buffered-read"}},
		Body:       &errTailReader{data: []byte(partial), err: errors.New("simulated buffered read failure")},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID: 56, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
	}

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 82)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, exchange.Written(), "buffered read failures must not commit a partial client response")
}

func TestOpenAIRuntimeMessagesOAuthUsesPureResponsesExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-oauth-messages-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_runtime_oauth_messages","model":"gpt-4o","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_runtime_oauth_messages","object":"response","model":"gpt-4o","status":"completed","output":[{"type":"message","id":"msg_runtime_oauth_messages","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          47,
		Name:        "openai-oauth-messages",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 80)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, exchange.state["openai_runtime_messages_responses_pure"])
	require.Equal(t, "runtime-oauth-messages-rid", result.RequestID)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "message", gjson.Get(string(exchange.body), "type").String())
	require.Equal(t, "ok", gjson.Get(string(exchange.body), "content.0.text").String())
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeMessagesOAuthCodexUsesPureResponsesExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-oauth-codex-messages-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.completed","response":{"id":"resp_runtime_oauth_codex_messages","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          55,
		Name:        "openai-oauth-codex-messages",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
	}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 904)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, exchange.state["openai_runtime_messages_responses_pure"])
	require.Equal(t, "runtime-oauth-codex-messages-rid", result.RequestID)
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Contains(t, string(upstream.lastBody), "sub2api-claude-code-todo-guard")
}

func TestOpenAIRuntimeMessagesOAuthStreamsThroughPureResponsesExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-oauth-messages-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_runtime_oauth_messages_stream","model":"gpt-4o","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_runtime_oauth_messages_stream","object":"response","model":"gpt-4o","status":"completed","output":[{"type":"message","id":"msg_runtime_oauth_messages_stream","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          48,
		Name:        "openai-oauth-messages",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 81)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, true, exchange.state["openai_runtime_messages_responses_pure"])
	require.Equal(t, "runtime-oauth-messages-stream-rid", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), `"type":"message_start"`)
	require.Contains(t, string(exchange.body), `"text_delta"`)
	require.Contains(t, string(exchange.body), `"text":"ok"`)
	require.Contains(t, string(exchange.body), `"type":"message_stop"`)
	require.GreaterOrEqual(t, exchange.flushCount, 1)
}

func TestOpenAIRuntimeMessagesOAuthReturnsFailoverWithoutWritingResponse(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"runtime-oauth-messages-error-rid"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream unavailable"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          49,
		Name:        "openai-oauth-messages",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 82)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.False(t, exchange.Written())
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Contains(t, string(failoverErr.ResponseBody), "upstream unavailable")
}

func TestOpenAIRuntimeMessagesAPIKeyResponsesUsesPureExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-apikey-messages-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_runtime_apikey_messages","model":"gpt-4o","status":"in_progress","output":[]}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_runtime_apikey_messages","object":"response","model":"gpt-4o","status":"completed","output":[{"type":"message","id":"msg_runtime_apikey_messages","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.ID = 50
	account.Extra = map[string]any{"openai_responses_mode": "force_responses"}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 83)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, true, exchange.state["openai_runtime_messages_responses_pure"])
	require.Equal(t, "runtime-apikey-messages-rid", result.RequestID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "message", gjson.Get(string(exchange.body), "type").String())
	require.Equal(t, "ok", gjson.Get(string(exchange.body), "content.0.text").String())
	require.Equal(t, "/v1/responses", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/responses", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeMessagesOAuthCodexCompatUsesPureResponsesExchangePipeline(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	account := &Account{
		ID:       51,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	require.True(t, svc.shouldUseOpenAIMessagesHTTPRuntime(context.Background(), exchange, account, body, ""))
}

func TestOpenAIRuntimeMessagesAPIKeyCompatCarriesContinuationStateAcrossRequests(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	first := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":       []string{"text/event-stream"},
			"x-request-id":       []string{"runtime-messages-state-1"},
			"x-codex-turn-state": []string{"turn-state-1"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.completed","response":{"id":"resp_state_1","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"first"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}
	second := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-messages-state-2"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.completed","response":{"id":"resp_state_2","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"second"}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{first, second}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.ID = 52
	account.Extra = map[string]any{"openai_responses_mode": "force_responses"}

	firstExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	firstExchange.request.URL.Path = "/v1/messages"
	firstResult, err := svc.ForwardAsAnthropicRuntime(context.Background(), firstExchange, account, body, "", "", 901)
	require.NoError(t, err)
	require.NotNil(t, firstResult)

	secondExchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	secondExchange.request.URL.Path = "/v1/messages"
	secondResult, err := svc.ForwardAsAnthropicRuntime(context.Background(), secondExchange, account, body, "", "", 901)
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "resp_state_1", gjson.GetBytes(upstream.bodies[1], "previous_response_id").String())
}

func TestOpenAIRuntimeMessagesOAuthCompatCarriesTurnStateWithoutGin(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-messages-oauth-state"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.completed","response":{"id":"resp_oauth_state","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          53,
		Name:        "openai-oauth-state",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
	}
	const promptKey = "anthropic-metadata-runtime-state"
	svc.bindOpenAICompatSessionTurnStateRuntime(context.Background(), account, 902, promptKey, "turn-state-oauth")

	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, promptKey, "", 902)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "turn-state-oauth", upstream.lastReq.Header.Get("x-codex-turn-state"))
}

func TestOpenAIRuntimeMessagesCompatRecoversMissingPreviousResponseWithoutWritingTwice(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	first := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"runtime-prev-missing"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"previous_response_not_found","message":"previous response not found"}}`)),
	}
	second := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"runtime-prev-recovered"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.completed","response":{"id":"resp_recovered","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered"}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{first, second}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.ID = 54
	account.Extra = map[string]any{"openai_responses_mode": "force_responses"}
	const promptKey = "anthropic-digest-prev-recovery"
	svc.bindOpenAICompatSessionResponseIDRuntime(context.Background(), account, 903, promptKey, "resp_stale")

	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, promptKey, "", 903)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "resp_stale", gjson.GetBytes(upstream.bodies[0], "previous_response_id").String())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "previous_response_id").Exists())
	require.Equal(t, "recovered", gjson.Get(string(exchange.body), "content.0.text").String())
}

func TestOpenAIRuntimeChatCompletionsPureExchangeStreamsUsage(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-chat-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"content":"ok"}}]}`,
			"",
			`data: {"id":"chatcmpl_runtime_stream","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11,"prompt_tokens_details":{"cached_tokens":2}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 42)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-chat-stream-rid", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.True(t, result.Stream)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), "data: [DONE]")
	require.GreaterOrEqual(t, exchange.flushCount, 1)
}

func TestOpenAIRuntimeChatCompletionsPureExchangeReturnsFailoverError(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/chat/completions"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"runtime-chat-error-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"upstream unavailable"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()

	result, err := svc.ForwardAsChatCompletionsRuntime(context.Background(), exchange, account, body, "", "", 42)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.False(t, exchange.Written())
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Contains(t, string(failoverErr.ResponseBody), "upstream unavailable")
}

func TestOpenAIRuntimeResponsesForceChatUsesPureExchangeConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"runtime-responses-chat-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_runtime","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	result, err := svc.ForwardRuntime(context.Background(), exchange, account, body, 42)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-responses-chat-rid", result.RequestID)
	require.Equal(t, "response", gjson.Get(string(exchange.body), "object").String())
	require.Equal(t, "ok", gjson.Get(string(exchange.body), "output.0.content.0.text").String())
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeResponsesForceChatStreamsPureExchangeConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-responses-chat-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
			"",
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			"",
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	result, err := svc.ForwardRuntime(context.Background(), exchange, account, body, 42)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, "runtime-responses-chat-stream-rid", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), "response.output_text.delta")
	require.Contains(t, string(exchange.body), "response.completed")
	require.Contains(t, string(exchange.body), "data: [DONE]")
	require.GreaterOrEqual(t, exchange.flushCount, 1)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeMessagesForceChatUsesPureExchangeConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"runtime-messages-chat-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_runtime","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 42)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-messages-chat-rid", result.RequestID)
	require.Equal(t, "assistant", gjson.Get(string(exchange.body), "role").String())
	require.Equal(t, "ok", gjson.Get(string(exchange.body), "content.0.text").String())
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestOpenAIRuntimeMessagesForceChatStreamsPureExchangeConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/messages"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"runtime-messages-chat-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
			"",
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"content":" world"}}]}`,
			"",
			`data: {"id":"chatcmpl_runtime_stream","choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{"openai_responses_mode": "force_chat_completions"}

	result, err := svc.ForwardAsAnthropicRuntime(context.Background(), exchange, account, body, "", "", 42)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, "runtime-messages-chat-stream-rid", result.RequestID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), "event: message_start")
	require.Contains(t, string(exchange.body), "event: content_block_delta")
	require.Contains(t, string(exchange.body), "event: message_stop")
	require.GreaterOrEqual(t, exchange.flushCount, 1)
	require.Equal(t, "/v1/chat/completions", ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestForwardEmbeddingsRuntimeUsesPureHTTPExchange(t *testing.T) {
	reqBody := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(reqBody))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"runtime-emb-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"data":[{"embedding":[0.1]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:       43,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-runtime",
			"base_url": "https://api.openai.com",
		},
	}

	result, err := svc.ForwardEmbeddingsRuntime(context.Background(), exchange, account, reqBody, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-emb-rid", result.RequestID)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "application/json", exchange.header.Get("Content-Type"))
	require.Equal(t, "text-embedding-3-small", gjson.GetBytes(exchange.body, "model").String())
	require.Equal(t, "https://api.openai.com/v1/embeddings", upstream.lastReq.URL.String())
}

func TestForwardImagesRuntimeUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat","n":1,"size":"1024x1024"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.Path = "/v1/images/generations"
	exchange.request.Header.Set("Content-Type", "application/json")
	exchange.request.Header.Set("User-Agent", "runtime-image-client/1.0")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body,
	)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"runtime-image-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"created":123,"data":[{"b64_json":"abc","size":"1024x1024"}]}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 50, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body, parsed, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-image-rid", result.RequestID)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "abc", gjson.GetBytes(exchange.body, "data.0.b64_json").String())
	require.Equal(t, "http://upstream.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "runtime-image-client/1.0", upstream.lastReq.Header.Get("User-Agent"))
}

func TestForwardImagesRuntimeStreamsThroughPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat","stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.Path = "/v1/images/generations"
	exchange.request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body,
	)
	require.NoError(t, err)
	require.True(t, parsed.Stream)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"runtime-image-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"image_generation.completed\",\"data\":[{\"b64_json\":\"abc\",\"size\":\"1024x1024\"}]}\n\ndata: [DONE]\n\n")),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 51, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image-stream-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body, parsed, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-image-stream-rid", result.RequestID)
	require.Equal(t, 1, result.ImageCount)
	require.GreaterOrEqual(t, exchange.flushCount, 1)
	require.Contains(t, string(exchange.body), "image_generation.completed")
}

func TestForwardImagesRuntimeRewritesMultipartModelThroughPureHTTPExchange(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "replace background"))
	part, err := writer.CreateFormFile("image", "source.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake-image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body.Bytes()))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.Path = "/v1/images/edits"
	exchange.request.Header.Set("Content-Type", writer.FormDataContentType())
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body.Bytes(),
	)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"created":123,"data":[{"b64_json":"abc"}]}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 54, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image-edit-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body.Bytes(), parsed, "gpt-image-2")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/images/edits", upstream.lastReq.URL.String())
	_, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	multipartReader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
	form, err := multipartReader.ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	require.Equal(t, []string{"gpt-image-2"}, form.Value["model"])
}

func TestForwardImagesRuntimeOAuthNonStreamingUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.Path = "/v1/images/generations"
	exchange.request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body,
	)
	require.NoError(t, err)

	upstreamBody := `data: {"type":"response.completed","response":{"created_at":123,"output":[{"type":"image_generation_call","result":"abc","size":"1024x1024","output_format":"png"}],"tool_usage":{"image_gen":{"input_tokens":2,"output_tokens":3,"output_tokens_details":{"image_tokens":1}}}}}`

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"runtime-image-oauth-rid"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 52, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-image-runtime", "refresh_token": "refresh-image-runtime"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body, parsed, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-image-oauth-rid", result.RequestID)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "abc", gjson.GetBytes(exchange.body, "data.0.b64_json").String())
	require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", upstream.lastReq.URL.String())
}

func TestForwardImagesRuntimeOAuthStreamingUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.Path = "/v1/images/generations"
	exchange.request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body,
	)
	require.NoError(t, err)
	require.True(t, parsed.Stream)

	upstreamBody := "data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"part\",\"partial_image_index\":0}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":123,\"output\":[{\"type\":\"image_generation_call\",\"result\":\"abc\",\"size\":\"1024x1024\",\"output_format\":\"png\"}],\"tool_usage\":{\"image_gen\":{\"input_tokens\":2,\"output_tokens\":3,\"output_tokens_details\":{\"image_tokens\":1}}}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"runtime-image-oauth-stream-rid"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 53, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-image-runtime", "refresh_token": "refresh-image-runtime"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body, parsed, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "runtime-image-oauth-stream-rid", result.RequestID)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.GreaterOrEqual(t, exchange.flushCount, 2)
	require.Contains(t, string(exchange.body), "image_generation.partial_image")
	require.Contains(t, string(exchange.body), "image_generation.completed")
}

func TestForwardImagesRuntimeReturnsFailoverWithoutWritingResponse(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/images/generations"
	exchange.request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequestFromMetadata(
		exchange.request.URL.Path,
		exchange.request.Header.Get("Content-Type"),
		body,
	)
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary upstream failure"}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 55, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardImagesRuntime(context.Background(), exchange, account, body, parsed, "")

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, exchange.Written())
}

func TestForwardCountTokensAsAnthropicRuntimeUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.Header.Set("Content-Type", "application/json")
	exchange.request.Header.Set("User-Agent", "runtime-client/1.0")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"input_tokens":17}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 44, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-runtime", "base_url": "http://upstream.example"},
	}

	err := svc.ForwardCountTokensAsAnthropicRuntime(context.Background(), exchange, account, body, "gpt-5.4", 123)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.JSONEq(t, `{"input_tokens":17}`, string(exchange.body))
	require.Equal(t, "http://upstream.example/v1/responses/input_tokens", upstream.lastReq.URL.String())
	require.Equal(t, "runtime-client/1.0", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestForwardCountTokensAsAnthropicRuntimePreservesOAuthFallback(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-1","messages":[{"role":"user","content":"hello"}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"missing_scope","message":"Missing scopes: api.responses.write"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "oauth-runtime", "refresh_token": "refresh-runtime"},
	}

	err := svc.ForwardCountTokensAsAnthropicRuntime(context.Background(), exchange, account, body, "gpt-5.4", 0)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Greater(t, int(gjson.GetBytes(exchange.body, "input_tokens").Int()), 0)
}

func TestForwardAlphaSearchRuntimeUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-5","commands":[{"type":"search","query":"sub2api"}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request = exchange.request.WithContext(context.Background())
	exchange.request.URL.RawQuery = "locale=zh-CN"
	exchange.request.Header.Set("User-Agent", "runtime-client/1.0")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"alpha-runtime-rid"},
		},
		Body: io.NopCloser(strings.NewReader(`{"results":[]}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 46, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-alpha-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardAlphaSearchRuntime(context.Background(), exchange, account, body, 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "alpha-runtime-rid", result.RequestID)
	require.Equal(t, 1, result.WebSearchCalls)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "http://upstream.example/v1/alpha/search?locale=zh-CN", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-alpha-runtime", upstream.lastReq.Header.Get("Authorization"))
}

func TestForwardAlphaSearchRuntimePreservesPersonalAccessTokenResponsesFallback(t *testing.T) {
	body := []byte(`{"model":"gpt-5","commands":[{"type":"search","query":"sub2api"}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"alpha-pat-runtime-rid"},
		},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"result\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n")),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 47, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "at-runtime", "auth_mode": "personal_access_token",
		},
	}

	result, err := svc.ForwardAlphaSearchRuntime(context.Background(), exchange, account, body, 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "/v1/responses", result.UpstreamEndpoint)
	require.Equal(t, "alpha-pat-runtime-rid", result.RequestID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.JSONEq(t, `{"output":"result"}`, string(exchange.body))
	require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", upstream.lastReq.URL.String())
}

func TestForwardCountTokensRuntimeUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.Header.Set("Content-Type", "application/json")
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), domain.PlatformAnthropic)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"input_tokens":23}`)),
	}}
	svc := &GatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 48, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-ant-runtime", "base_url": "http://upstream.example"},
	}

	err = svc.ForwardCountTokensRuntime(context.Background(), exchange, account, parsed)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.JSONEq(t, `{"input_tokens":23}`, string(exchange.body))
	require.Equal(t, "http://upstream.example/v1/messages/count_tokens?beta=true", upstream.lastReq.URL.String())
	require.Equal(t, "sk-ant-runtime", getHeaderRaw(upstream.lastReq.Header, "x-api-key"))
}

func TestForwardGeminiNativeCountTokensRuntimeUsesPureHTTPExchange(t *testing.T) {
	body := []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"gemini-ct-runtime"}},
		Body:       io.NopCloser(strings.NewReader(`{"totalTokens":11}`)),
	}}
	svc := &GeminiMessagesCompatService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 49, Platform: PlatformGemini, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "gemini-runtime", "base_url": "http://upstream.example"},
	}

	result, err := svc.ForwardNativeRuntime(context.Background(), exchange, account, "gemini-2.5-flash", "countTokens", false, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gemini-ct-runtime", result.RequestID)
	require.Equal(t, http.StatusOK, exchange.status)
	require.JSONEq(t, `{"totalTokens":11}`, string(exchange.body))
	require.Equal(t, "http://upstream.example/v1beta/models/gemini-2.5-flash:countTokens", upstream.lastReq.URL.String())
}
