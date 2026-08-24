//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenAICodexAccountIdentityTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "luna/1.0.0")
	return c
}

func newOpenAICodexAccountWithCustomUserAgent() *Account {
	return &Account{
		ID:       991,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-account",
			"user_agent":         "codex_vscode/0.125.0 (Mac OS X 15.1.0; arm64) vscode",
		},
	}
}

func requireOpenAICodexAccountCustomIdentity(t *testing.T, header http.Header) {
	t.Helper()
	require.Equal(t, "codex_vscode", header.Get("originator"))
	require.Equal(t, "codex_vscode/"+codexCLIVersion+" (Mac OS X 15.1.0; arm64) vscode", header.Get("user-agent"))
	require.Equal(t, codexCLIVersion, header.Get("version"))
}

func TestBuildUpstreamRequestLegacyHonorsOpenAIAccountUserAgent(t *testing.T) {
	svc := &OpenAIGatewayService{}
	req, err := svc.buildUpstreamRequestLegacy(
		context.Background(),
		newOpenAICodexAccountIdentityTestContext(),
		newOpenAICodexAccountWithCustomUserAgent(),
		[]byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
		"oauth-token",
		true,
		"",
		true,
	)
	require.NoError(t, err)
	requireOpenAICodexAccountCustomIdentity(t, req.Header)
}

func TestBuildUpstreamRequestPassthroughHonorsOpenAIAccountUserAgent(t *testing.T) {
	svc := &OpenAIGatewayService{}
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(
		context.Background(),
		newOpenAICodexAccountIdentityTestContext(),
		newOpenAICodexAccountWithCustomUserAgent(),
		[]byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
		"oauth-token",
	)
	require.NoError(t, err)
	requireOpenAICodexAccountCustomIdentity(t, req.Header)
}
