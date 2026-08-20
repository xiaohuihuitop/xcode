//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRequestUpstreamModelPrefersPlatformRoute(t *testing.T) {
	ctx := WithGatewayPlatformAssetContext(context.Background(), &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID: 7, RequestedModel: "public-gpt", UpstreamModel: "gpt-5.6",
		},
		SchedulingScope: PlatformSchedulingScope{
			PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
		},
	})
	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"public-gpt": "gpt-old"},
	}}

	model, source := resolveRequestUpstreamModel(ctx, account, "public-gpt")

	require.Equal(t, "gpt-5.6", model)
	require.Equal(t, "platform", source)
}

func TestResolveRequestUpstreamModelKeepsUnscopedAccountMapping(t *testing.T) {
	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"public-gpt": "gpt-old"},
	}}

	model, source := resolveRequestUpstreamModel(context.Background(), account, "public-gpt")

	require.Equal(t, "gpt-old", model)
	require.Equal(t, "account", source)
}

func TestResolveRequestUpstreamModelLeavesCompactMappingToCompactStage(t *testing.T) {
	ctx := WithGatewayPlatformAssetContext(context.Background(), &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID: 7, RequestedModel: "public-gpt", UpstreamModel: "gpt-5.6",
		},
		SchedulingScope: PlatformSchedulingScope{
			PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
		},
	})
	account := &Account{Credentials: map[string]any{
		"model_mapping":         map[string]any{"public-gpt": "gpt-old"},
		"compact_model_mapping": map[string]any{"gpt-5.6": "gpt-5.6-compact"},
	}}

	model, source := resolveRequestUpstreamModel(ctx, account, "public-gpt")

	require.Equal(t, "gpt-5.6", model)
	require.Equal(t, "platform", source)
	require.Equal(t, "gpt-5.6-compact", resolveOpenAICompactForwardModel(account, model))
}
