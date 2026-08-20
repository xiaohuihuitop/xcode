package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAIResponsesToolParameterTypes(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","parameters":{"type":null}},{"type":"function","parameters":{"properties":{}}}],"input":[{"tools":[{"function":{"parameters":{"type":null}}}]}]}`)

	out, changed, err := sanitizeOpenAIResponsesToolParameterTypes(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "object", gjson.GetBytes(out, "tools.0.parameters.type").String())
	require.False(t, gjson.GetBytes(out, "tools.1.parameters.type").Exists(), "missing type remains unconstrained")
	require.Equal(t, "object", gjson.GetBytes(out, "input.0.tools.0.function.parameters.type").String())
}
