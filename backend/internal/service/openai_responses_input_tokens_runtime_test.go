//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardResponsesInputTokensExchangeCustomRelayUsesLocalEstimate(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{ID: 159, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "relay-key", "base_url": "https://relay.example/v1",
	}}
	err := svc.ForwardResponsesInputTokensExchange(context.Background(), exchange, account, []byte(`{"model":"gpt-5.4","instructions":"Be concise.","input":"hello world"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "response.input_tokens", gjson.GetBytes(exchange.body, "object").String())
	require.Positive(t, gjson.GetBytes(exchange.body, "input_tokens").Int())
	require.Nil(t, upstream.lastReq)
}

func TestForwardResponsesInputTokensExchangeGrokUsesLocalEstimate(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 160, Platform: PlatformGrok, Type: AccountTypeOAuth}
	err := svc.ForwardResponsesInputTokensExchange(context.Background(), exchange, account, []byte(`{"model":"grok-4.1","input":"hello world"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Positive(t, gjson.GetBytes(exchange.body, "input_tokens").Int())
	require.Nil(t, upstream.lastReq)
}

func TestForwardResponsesInputTokensExchangeUpstreamSuccess(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":23}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{ID: 171, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "official-key", "base_url": "https://api.openai.com/v1",
	}}
	err := svc.ForwardResponsesInputTokensExchange(context.Background(), exchange, account, []byte(`{"model":"gpt-5.4","input":"hello world"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "response.input_tokens", gjson.GetBytes(exchange.body, "object").String())
	require.EqualValues(t, 23, gjson.GetBytes(exchange.body, "input_tokens").Int())
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://api.openai.com/v1/responses/input_tokens", upstream.lastReq.URL.String())
}

func TestForwardResponsesInputTokensExchangeUpstream404FallsBackLocally(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusNotFound, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Invalid URL (POST /v1/responses/input_tokens)"}}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{ID: 172, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "official-key", "base_url": "https://api.openai.com/v1",
	}}
	err := svc.ForwardResponsesInputTokensExchange(context.Background(), exchange, account, []byte(`{"model":"gpt-5.4","input":"hello world"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, exchange.status)
	require.Equal(t, "response.input_tokens", gjson.GetBytes(exchange.body, "object").String())
	require.Positive(t, gjson.GetBytes(exchange.body, "input_tokens").Int())
}

func TestForwardResponsesInputTokensExchangeRejectsMissingModel(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	svc := &OpenAIGatewayService{}
	err := svc.ForwardResponsesInputTokensExchange(context.Background(), exchange, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, []byte(`{"input":"hello"}`))
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, exchange.status)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(exchange.body, "error.type").String())
}
