//go:build unit

package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenAIUpstreamRequestFromRuntimeRequest_APIKeyCompact(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.local/v1/responses/compact", bytes.NewReader(body))
	inbound.Header.Set("Content-Type", "application/json")
	inbound.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	inbound.Header.Set("session_id", "client-session")
	inbound.Header.Set("x-codex-turn-state", "turn-1")
	inbound.Header.Set("Authorization", "client-must-not-forward")

	account := rawChatCompletionsTestAccount()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}

	req, err := svc.buildOpenAIUpstreamRequestFromRuntimeRequest(
		context.Background(),
		inbound,
		account,
		body,
		"sk-test",
		false,
		"",
		true,
		42,
	)

	require.NoError(t, err)
	require.Equal(t, "http://upstream.example/v1/responses/compact", req.URL.String())
	require.Equal(t, "Bearer sk-test", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "application/json", req.Header.Get("Accept"))
	require.Equal(t, "turn-1", req.Header.Get("x-codex-turn-state"))
	require.Equal(t, "client-session", req.Header.Get("session_id"))
	require.NotContains(t, req.Header.Get("Authorization"), "client-must-not-forward")
}

func TestBuildOpenAIUpstreamRequestFromRuntimeRequest_MatchesLegacyAPIKeyBuilder(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.local/v1/responses/compact", bytes.NewReader(body))
	inbound.Header.Set("Content-Type", "application/json")
	inbound.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	inbound.Header.Set("session_id", "client-session")
	inbound.Header.Set("x-codex-turn-state", "turn-1")

	account := rawChatCompletionsTestAccount()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = inbound.Clone(context.Background())
	legacy, err := svc.buildUpstreamRequestLegacy(context.Background(), c, account, body, "sk-test", false, "", true)
	require.NoError(t, err)

	runtimeReq, err := svc.buildOpenAIUpstreamRequestFromRuntimeRequest(
		context.Background(), inbound, account, body, "sk-test", false, "", true, 0,
	)
	require.NoError(t, err)
	require.Equal(t, legacy.URL.String(), runtimeReq.URL.String())
	for _, key := range []string{"Authorization", "Content-Type", "Accept", "User-Agent", "session_id", "x-codex-turn-state"} {
		require.Equal(t, legacy.Header.Get(key), runtimeReq.Header.Get(key), key)
	}
}

func TestBuildOpenAIUpstreamRequestFromRuntimeStateAppliesCodexFingerprint(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[]}`)
	inbound := httptest.NewRequest(http.MethodPost, "http://gateway.local/v1/responses", bytes.NewReader(body))
	inbound.Header.Set("Content-Type", "application/json")
	account := newTestOAuthAccount(77, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "client-session", codexFingerprintSession)
	require.NotNil(t, ids)

	req, err := (&OpenAIGatewayService{}).buildOpenAIUpstreamRequestFromRuntimeRequestWithState(
		context.Background(), inbound, account, body, "token", false, "", true,
		openAIRequestBuildRuntimeState{CodexFingerprintIDs: ids},
	)

	require.NoError(t, err)
	require.Equal(t, ids.installationID, req.Header.Get("x-codex-installation-id"))
	require.Equal(t, ids.sessionID, req.Header.Get("session_id"))
	require.Equal(t, ids.threadID, req.Header.Get("x-client-request-id"))
}
