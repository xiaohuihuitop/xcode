package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesToAnthropicDropsUnsupportedAndEmptyContent(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","content":[{"type":"reasoning_text","text":"private"}]},
		{"role":"user","content":[{"type":"unknown_part","value":"x"}]},
		{"role":"assistant","content":[{"type":"output_text","text":"   "}]},
		{"type":"custom_history","content":[{"type":"input_text","text":"keep me"},{"type":"reasoning_text","text":"drop me"}]}
	]`)

	_, messages, err := convertResponsesInputToAnthropic("", input)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].Role)
	require.Contains(t, string(messages[0].Content), "keep me")
	require.NotContains(t, string(messages[0].Content), "reasoning_text")
}
