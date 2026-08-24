package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func requireOpenAICodexProbeHeaders(t *testing.T, h http.Header) {
	t.Helper()
	require.Equal(t, codexCLIUserAgent, h.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, h.Get("Originator"))
	require.Equal(t, codexCLIVersion, h.Get("Version"))
	require.Equal(t, "responses=experimental", h.Get("OpenAI-Beta"))
	require.NotEmpty(t, h.Get("X-Codex-Window-ID"))
}

func TestEnsureCodexIdentityHeaders(t *testing.T) {
	t.Run("补齐缺失身份头", func(t *testing.T) {
		h := make(http.Header)

		ensureCodexIdentityHeaders(h)
		enforceCodexIdentityHeaders(h)

		require.Equal(t, openai.CodexDefaultOriginator, h.Get("originator"))
		require.Equal(t, codexCLIUserAgent, h.Get("user-agent"))
		require.Equal(t, codexCLIVersion, h.Get("version"))
		require.Equal(t, "responses=experimental", h.Get("OpenAI-Beta"))
	})

	t.Run("已有官方客户端身份也统一为规范身份", func(t *testing.T) {
		const tuiUA = "codex-tui/9.9.9 (Mac OS X 14.0; arm64) iTerm (codex-tui; 9.9.9)"
		h := make(http.Header)
		h.Set("user-agent", tuiUA)
		h.Set("version", "9.9.9")
		h.Set("OpenAI-Beta", "assistants=v2")

		ensureCodexIdentityHeaders(h)
		enforceCodexIdentityHeaders(h)

		require.Equal(t, openai.CodexDefaultOriginator, h.Get("originator"))
		require.Equal(t, codexCLIUserAgent, h.Get("user-agent"))
		require.Equal(t, codexCLIVersion, h.Get("version"))
		require.Equal(t, "responses=experimental", h.Get("OpenAI-Beta"))
	})
}

func TestEnforceCodexIdentityHeaders(t *testing.T) {
	const tuiUA = "codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.140.2)"

	tests := []struct {
		name       string
		originator string
		userAgent  string
		version    string
	}{
		{
			name:       "错配 originator 按最终 UA 重配",
			originator: "codex_cli_rs",
			userAgent:  tuiUA,
		},
		{
			name:       "官方配套身份原样保留",
			originator: "codex-tui",
			userAgent:  tuiUA,
		},
		{
			name:       "第三方 UA 整体回退默认身份",
			originator: "opencode",
			userAgent:  "luna/1.0.0",
		},
		{
			name:       "UA 缺失回退默认身份",
			originator: "codex_vscode",
		},
		{
			name:       "originator override UA 首段被尾部真实身份重写",
			originator: "cccc",
			userAgent:  "cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)",
		},
		{
			name:       "低于门槛的 version 提升为内置版本",
			originator: "codex_cli_rs",
			userAgent:  "codex_cli_rs/0.125.0",
			version:    "0.125.0",
		},
		{
			name:       "达标 version 原样保留",
			originator: "codex_cli_rs",
			userAgent:  "codex_cli_rs/0.145.0",
			version:    "0.145.0",
		},
		{
			name:       "未携带 version 不注入",
			originator: "codex_cli_rs",
			userAgent:  "codex_cli_rs/0.98.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := make(http.Header)
			if tt.originator != "" {
				h.Set("originator", tt.originator)
			}
			if tt.userAgent != "" {
				h.Set("user-agent", tt.userAgent)
			}
			if tt.version != "" {
				h.Set("version", tt.version)
			}

			enforceCodexIdentityHeaders(h)

			require.Equal(t, openai.CodexDefaultOriginator, h.Get("originator"))
			require.Equal(t, codexCLIUserAgent, h.Get("user-agent"))
			require.Equal(t, codexCLIVersion, h.Get("version"))
		})
	}
}

func TestEnforceCodexIdentityHeadersWithAccountOverrideUA(t *testing.T) {
	h := make(http.Header)
	h.Set("originator", "codex-tui")
	h.Set("user-agent", "luna/1.0.0")
	h.Set("version", "2.1.0")

	enforceCodexIdentityHeadersWithUA(h, "codex_vscode/0.125.0 (Ubuntu 22.4.0; x86_64) vscode")

	require.Equal(t, "codex_vscode", h.Get("originator"))
	require.Equal(t, "codex_vscode/"+codexCLIVersion+" (Ubuntu 22.4.0; x86_64) vscode", h.Get("user-agent"))
	require.Equal(t, codexCLIVersion, h.Get("version"))
}

func TestEnforceCodexIdentityHeadersEnforcementDisabled(t *testing.T) {
	const tuiUA = "codex-tui/0.145.2 (Mac OS X 14.0; arm64) iTerm (codex-tui; 0.145.2)"
	SetCodexIdentityEnforcementEnabled(false)
	t.Cleanup(func() { SetCodexIdentityEnforcementEnabled(true) })

	h := make(http.Header)
	h.Set("originator", "codex-tui")
	h.Set("user-agent", tuiUA)
	h.Set("version", "0.145.2")
	enforceCodexIdentityHeaders(h)

	require.Equal(t, "codex-tui", h.Get("originator"))
	require.Equal(t, tuiUA, h.Get("user-agent"))
	require.Equal(t, "0.145.2", h.Get("version"))
}

// enforce 本身仍只负责收口：缺少 originator 时必须保持 no-op，由需要恢复身份的
// 调用方先显式调用 ensureCodexIdentityHeaders。
func TestEnforceCodexIdentityHeaders_NoOriginatorIsNoop(t *testing.T) {
	h := make(http.Header)
	h.Set("user-agent", "third-party-client/1.0.0")

	enforceCodexIdentityHeaders(h)

	require.Empty(t, h.Get("originator"))
	require.Equal(t, "third-party-client/1.0.0", h.Get("user-agent"))
}

func TestEnforceCodexIdentityHeadersUsesCanonicalSyncedVersion(t *testing.T) {
	SetCodexCanonicalUserAgentResolver(func() string {
		return "codex-tui/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.200.1)"
	})
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })

	h := make(http.Header)
	h.Set("originator", "codex_cli_rs")
	h.Set("user-agent", "codex_cli_rs/0.144.1")
	h.Set("version", "0.144.1")

	enforceCodexIdentityHeaders(h)

	require.Equal(t, "codex-tui", h.Get("originator"))
	require.Equal(t, "codex-tui/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.200.1)", h.Get("user-agent"))
	require.Equal(t, "0.200.1", h.Get("version"))
}
