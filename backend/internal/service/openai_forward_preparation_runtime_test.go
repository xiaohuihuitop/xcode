//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexRestrictionDetectRequestMatchesGinSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_cli_only": true},
	}
	policy := CodexRestrictionPolicy{}
	body := []byte(`{"model":"gpt-5.5"}`)
	req, err := http.NewRequest(http.MethodPost, "http://runtime.test/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("User-Agent", "codex_cli_rs/0.141.0")
	req.Header.Set("originator", "codex_cli_rs")

	c, _ := gin.CreateTestContext(nil)
	c.Request = req
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	fromGin := detector.Detect(c, account, policy, body)
	fromRequest := detector.DetectRequest(req.Header, account, policy, body)
	require.Equal(t, fromGin, fromRequest)
}

func TestOpenAIClientTransportExchangeStateRoundTrip(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	require.Equal(t, OpenAIClientTransportUnknown, GetOpenAIClientTransportExchange(exchange))
	SetOpenAIClientTransportExchange(exchange, OpenAIClientTransportHTTP)
	require.Equal(t, OpenAIClientTransportHTTP, GetOpenAIClientTransportExchange(exchange))
	SetOpenAIClientTransportExchange(exchange, OpenAIClientTransportWS)
	require.Equal(t, OpenAIClientTransportWS, GetOpenAIClientTransportExchange(exchange))
}

func TestFlattenOpenAIResponsesNamespacesExchangeStoresMapping(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	body := []byte(`{"model":"gpt-5.5","tools":[{"type":"namespace","name":"demo","tools":[{"type":"function","name":"run"}]}]}`)

	flattened, err := flattenOpenAIResponsesNamespacesExchange(exchange, body)

	require.NoError(t, err)
	require.NotEqual(t, string(body), string(flattened))
	value, ok := exchange.State(openAIResponsesNamespaceNamesContextKey)
	require.True(t, ok)
	require.NotNil(t, value)
}

func TestForwardOpenAIResponsesHTTPRuntimeUsesPurePreparation(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"hello","stream":false}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"pure-prep-rid"},
		},
		Body: io.NopCloser(bytes.NewBufferString(`{"id":"resp_pure_prep","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID: 101, Name: "pure-prep", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1, Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
	}

	result, err := svc.forwardOpenAIResponsesHTTPRuntime(context.Background(), exchange, account, body, 7)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "pure-prep-rid", result.RequestID)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
}
