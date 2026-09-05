package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptResponsesClientTools_PromotesCompletedToolSearchDiscoveries(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{"type": "tool_search"}},
		"input": []any{map[string]any{
			"type": "tool_search_output", "status": "completed", "call_id": "search_1",
			"tools": []any{
				map[string]any{"type": "function", "name": "inspect", "parameters": map[string]any{"type": "object"}},
				map[string]any{"type": "namespace", "name": "ops", "tools": []any{map[string]any{
					"type": "function", "name": "run", "parameters": map[string]any{"type": "object"},
				}}},
			},
		}},
	}

	mapping, changed, err := AdaptResponsesClientTools(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, ResponsesNamespaceName{Namespace: "ops", Name: "run"}, mapping.NamespaceTools["ops__run"])
	tools := requireResponsesClientToolValue[[]any](t, req["tools"])
	require.Equal(t, []string{"tool_search", "inspect", "ops__run"}, responsesClientToolNames(t, tools))
	input := requireResponsesClientToolValue[[]any](t, req["input"])
	output := requireResponsesClientToolValue[map[string]any](t, input[0])
	require.Equal(t, "function_call_output", output["type"])
	require.NotContains(t, output, "tools")
}

func TestAdaptResponsesClientToolsWithInheritedMappingRestoresClientDeclarations(t *testing.T) {
	first := map[string]any{"tools": []any{
		map[string]any{"type": "custom", "name": "exec"},
		map[string]any{"type": "tool_search"},
		map[string]any{"type": "namespace", "name": "ops", "tools": []any{map[string]any{"type": "function", "name": "run"}}},
	}}
	mapping, changed, err := AdaptResponsesClientTools(first)
	require.NoError(t, err)
	require.True(t, changed)
	lowered := requireResponsesClientToolValue[[]any](t, first["tools"])

	followUp := map[string]any{"input": []any{map[string]any{
		"type": "custom_tool_call", "id": "ctc_1", "call_id": "call_1", "name": "exec", "input": "pwd",
	}}}
	got, changed, err := AdaptResponsesClientToolsWithInheritedMapping(followUp, mapping, lowered)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, mapping, got)
	replayed := requireResponsesClientToolValue[[]any](t, followUp["tools"])
	require.Equal(t, "function", requireResponsesClientToolValue[map[string]any](t, replayed[0])["type"])
	require.Equal(t, "function", requireResponsesClientToolValue[map[string]any](t, replayed[1])["type"])
	require.Equal(t, "ops__run", requireResponsesClientToolValue[map[string]any](t, replayed[2])["name"])
	replayedInput := requireResponsesClientToolValue[[]any](t, followUp["input"])
	require.Equal(t, "function_call", requireResponsesClientToolValue[map[string]any](t, replayedInput[0])["type"])
}

func TestAdaptResponsesClientToolsWithInheritedMapping_ReplaysHistoryAndRecoversClientItemID(t *testing.T) {
	first := map[string]any{
		"tools": []any{map[string]any{"type": "custom", "name": "exec"}},
	}
	mapping, changed, err := AdaptResponsesClientTools(first)
	require.NoError(t, err)
	require.True(t, changed)
	loweredTools := requireResponsesClientToolValue[[]any](t, first["tools"])

	followUp := map[string]any{
		"input": []any{map[string]any{
			"type": "custom_tool_call", "id": "ctc_123", "call_id": "call_1", "name": "exec", "input": "pwd",
		}},
	}
	inherited, changed, err := AdaptResponsesClientToolsWithInheritedMapping(followUp, mapping, loweredTools)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, inherited.CustomTools["exec"])
	replayedTools := requireResponsesClientToolValue[[]any](t, followUp["tools"])
	replayedTool := requireResponsesClientToolValue[map[string]any](t, replayedTools[0])
	require.Equal(t, "function", replayedTool["type"])
	replayedInput := requireResponsesClientToolValue[[]any](t, followUp["input"])
	replayedCall := requireResponsesClientToolValue[map[string]any](t, replayedInput[0])
	require.Equal(t, "function_call", replayedCall["type"])
	require.Equal(t, "fc_123", replayedCall["id"])
}
