package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexUserAgentVersion(t *testing.T) {
	require.Equal(t, "0.146.0", CodexUserAgentVersion("codex_cli_rs/0.146.0 (Ubuntu 22.4.0; x86_64) xterm-256color"))
	require.Equal(t, "0.147.0-alpha.4", CodexUserAgentVersion("codex-tui/0.147.0-alpha.4 (Mac OS X 14.0; arm64) iTerm"))
	require.Empty(t, CodexUserAgentVersion("curl 8.7.1"))
}

func TestSetCodexUserAgentVersion(t *testing.T) {
	require.Equal(t,
		"codex_cli_rs/0.146.0 (Ubuntu 22.4.0; x86_64) xterm-256color",
		SetCodexUserAgentVersion("codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color", "0.146.0"),
	)
	require.Equal(t,
		"cccc/0.146.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.146.0)",
		SetCodexUserAgentVersion("cccc/0.142.0 (Ubuntu 22.4.0; x86_64) screen (codex-tui; 0.142.0)", "0.146.0"),
	)
	require.Empty(t, SetCodexUserAgentVersion("not-a-codex-client", "0.146.0"))
}
