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
	"github.com/tidwall/gjson"
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
		Concurrency: 1, Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-account",
			"user_agent":         "codex_vscode/0.125.0 (Mac OS X 15.1.0; arm64) vscode",
		},
	}

	result, err := svc.forwardOpenAIResponsesHTTPRuntime(context.Background(), exchange, account, body, 7)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "pure-prep-rid", result.RequestID)
	require.Equal(t, openAIResponsesEndpoint, ActualOpenAIUpstreamEndpointFromExchange(exchange))
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	requireOpenAICodexAccountCustomIdentity(t, upstream.lastReq.Header)
}

func TestForwardOpenAIResponsesHTTPRuntimeAppliesCodexFingerprint(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"hello","stream":false,"client_metadata":{"session_id":"client-body-session","x-codex-turn-metadata":"{\"session_id\":\"client-body-session\",\"sandbox\":\"seccomp\"}"}}`)
	exchange := newRuntimeExchangeTestDouble(t, bytes.NewReader(body))
	exchange.request.URL.Path = "/v1/responses"
	exchange.request.Header.Set("session-id", "client-header-session")
	exchange.request.Header.Set("x-codex-installation-id", "client-installation")
	exchange.request.Header.Set("x-codex-turn-metadata", `{"session_id":"client-header-session","sandbox":"seccomp"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp_fingerprint","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := &Account{
		ID: 102, Name: "fingerprint-prep", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
		Extra: map[string]any{
			codexFingerprintModeExtraKey: "session",
			codexFingerprintSeedExtraKey: testCodexFingerprintSeed,
		},
	}

	_, err := svc.forwardOpenAIResponsesHTTPRuntime(context.Background(), exchange, account, body, 7)

	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	wantInstallationID := resolveConvergedInstallationID(account, seed)
	wantSessionID := resolveConvergedSessionID(seed)
	wantThreadID := resolveConvergedThreadID(seed, "client-header-session")
	require.Equal(t, wantInstallationID, upstream.lastReq.Header.Get("x-codex-installation-id"))
	require.Equal(t, wantSessionID, upstream.lastReq.Header.Get("session-id"))
	require.Equal(t, wantThreadID, upstream.lastReq.Header.Get("thread-id"))
	require.Equal(t, wantInstallationID, gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String())
	require.Equal(t, wantSessionID, gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String())
	require.Equal(t, wantThreadID, gjson.GetBytes(upstream.lastBody, "client_metadata.thread_id").String())
	require.Equal(t,
		gjson.Get(upstream.lastReq.Header.Get("x-codex-turn-metadata"), "turn_id").String(),
		gjson.GetBytes(upstream.lastBody, "client_metadata.turn_id").String(),
	)
}
