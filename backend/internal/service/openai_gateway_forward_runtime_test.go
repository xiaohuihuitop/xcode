//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDoOpenAIUpstreamRequestExchangeUsesRuntimeTransportSurface(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	account := &Account{ID: 73, Concurrency: 4}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://upstream.example/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5.5"}`)))
	require.NoError(t, err)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"x-request-id": []string{"runtime-forward-rid"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"ok":true}`))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	resp, err := svc.doOpenAIUpstreamRequestExchange(context.Background(), exchange, account, request, "http://proxy.example")

	require.NoError(t, err)
	require.Same(t, upstream.resp, resp)
	require.Same(t, request, upstream.lastReq)
	require.Equal(t, "http://proxy.example", upstream.lastProxyURL)
	require.Equal(t, int64(73), upstream.lastAccountID)
	require.Equal(t, 4, upstream.lastConcurrency)
	require.NotNil(t, exchange.state[OpsUpstreamLatencyMsKey])
	_, ok := exchange.state[OpsUpstreamLatencyMsKey].(int64)
	require.True(t, ok)
}

func TestForwardOpenAIHTTPExchangeHandlesNativeOAuthResponses(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"native-oauth-rid"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(`{"id":"resp_native","model":"gpt-5.5","output":[],"usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7}}`))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          88,
		Name:        "oauth-native",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
	}

	result, err := svc.forwardOpenAIHTTPExchange(context.Background(), openAIHTTPExchangeForwardInput{
		Exchange:       exchange,
		Account:        account,
		Body:           body,
		Token:          "oauth-token",
		OriginalModel:  "gpt-5.5",
		UpstreamModel:  "gpt-5.5",
		BillingModel:   "gpt-5.5",
		StartTime:      time.Now(),
		APIKeyID:       42,
		IsCodexCLI:     false,
		PromptCacheKey: "",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "native-oauth-rid", result.RequestID)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), `"resp_native"`)
}

func TestOpenAIRuntimeForwardUsesNativeHTTPExchange(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello","instructions":"runtime"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"native-oauth-runtime-rid"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(`{"id":"resp_runtime","model":"gpt-5.5","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID:          89,
		Name:        "apikey-native-runtime",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{"api_key": "api-key"},
	}

	result, err := svc.ForwardRuntime(context.Background(), exchange, account, body, 43)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "native-oauth-runtime-rid", result.RequestID)
	require.Equal(t, openAIResponsesEndpoint, upstream.lastReq.URL.Path)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, http.StatusOK, exchange.status)
}

func TestForwardOpenAIHTTPExchangeReturnsFailoverWithoutCommittingResponse(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"x-request-id": []string{"native-failover-rid"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"message":"upstream unavailable"}}`))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{ID: 90, Name: "native-failover", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "api-key"}}

	result, err := svc.forwardOpenAIHTTPExchange(context.Background(), openAIHTTPExchangeForwardInput{
		Exchange:      exchange,
		Account:       account,
		Body:          body,
		Token:         "api-key",
		OriginalModel: "gpt-5.5",
		UpstreamModel: "gpt-5.5",
		BillingModel:  "gpt-5.5",
		StartTime:     time.Now(),
	})

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Nil(t, result)
	require.False(t, exchange.Written())
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
}

func TestForwardOpenAIHTTPExchangeStreamsNativeResponses(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"native-stream-rid"}},
		Body: io.NopCloser(bytes.NewBufferString(
			"event: response.completed\n" +
				`data: {"type":"response.completed","response":{"id":"resp_stream_native","output":[],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}` + "\n\n",
		)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{ID: 91, Name: "native-stream", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "api-key"}}

	result, err := svc.forwardOpenAIHTTPExchange(context.Background(), openAIHTTPExchangeForwardInput{
		Exchange:      exchange,
		Account:       account,
		Body:          body,
		Token:         "api-key",
		OriginalModel: "gpt-5.5",
		UpstreamModel: "gpt-5.5",
		BillingModel:  "gpt-5.5",
		Stream:        true,
		StartTime:     time.Now(),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, "native-stream-rid", result.RequestID)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Contains(t, string(exchange.body), "response.completed")
}
