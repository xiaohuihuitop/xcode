//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type platformModelResolverStub struct {
	resolved *ResolvedPlatformModel
	err      error
}

func (s platformModelResolverStub) ResolveModel(context.Context, string) (*ResolvedPlatformModel, error) {
	return s.resolved, s.err
}

func (s platformModelResolverStub) ResolveModelCandidates(context.Context, string) ([]*ResolvedPlatformModel, error) {
	if s.resolved == nil {
		return nil, s.err
	}
	return []*ResolvedPlatformModel{s.resolved}, s.err
}

func TestResolvePlatformAssetRequestRejectsUnapprovedPlatform(t *testing.T) {
	apiKey := &APIKey{
		UserID:             7,
		AllowedPlatformIDs: []int64{2},
		AllowBalance:       true,
		User:               &User{ID: 7, Balance: 10},
	}
	resolver := platformModelResolverStub{resolved: &ResolvedPlatformModel{
		PlatformID:      1,
		PlatformCode:    "gpt",
		AccountPlatform: PlatformOpenAI,
	}}

	_, err := (&APIKeyService{}).ResolvePlatformAssetRequest(
		context.Background(), apiKey, resolver, nil, "gpt-4o", "/v1/chat/completions", false,
	)

	require.ErrorIs(t, err, ErrAPIKeyPlatformForbidden)
}

func TestResolvePlatformAssetRequestHonorsEndpointAndSelectsBalance(t *testing.T) {
	apiKey := &APIKey{
		UserID:             7,
		AllowedPlatformIDs: []int64{1},
		AllowBalance:       true,
		User:               &User{ID: 7, Balance: 10},
	}
	resolver := platformModelResolverStub{resolved: &ResolvedPlatformModel{
		PlatformID:           1,
		PlatformCode:         "gpt",
		AccountPlatform:      PlatformOpenAI,
		EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
	}}

	route, err := (&APIKeyService{}).ResolvePlatformAssetRequest(
		context.Background(), apiKey, resolver, nil, "gpt-4o", "/v1/chat/completions", false,
	)

	require.NoError(t, err)
	require.Equal(t, int64(1), route.Platform.PlatformID)
	require.Equal(t, BillingSourceBalance, route.BillingAsset.Source)
	require.Equal(t, PlatformOpenAI, route.SchedulingScope.AccountPlatform)

	_, err = (&APIKeyService{}).ResolvePlatformAssetRequest(
		context.Background(), apiKey, resolver, nil, "gpt-4o", "/v1/responses", false,
	)
	require.ErrorIs(t, err, ErrPlatformEndpointUnsupported)
}

func TestGatewayPlatformAssetContextSetsSchedulingAndModelRouting(t *testing.T) {
	route := &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID:      1,
			PlatformCode:    "gpt",
			AccountPlatform: PlatformOpenAI,
			RequestedModel:  "public-gpt",
			UpstreamModel:   "gpt-4o-2024-08-06",
		},
		SchedulingScope: PlatformSchedulingScope{
			PlatformID:      1,
			PlatformCode:    "gpt",
			AccountPlatform: PlatformOpenAI,
		},
	}

	ctx := WithGatewayPlatformAssetContext(context.Background(), route)

	got, ok := GatewayPlatformAssetContextFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(1), got.Platform.PlatformID)
	scope, ok := PlatformSchedulingScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(1), scope.PlatformID)
	platform, ok := ResolvedTargetPlatformFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, PlatformOpenAI, platform)
	upstreamModel, ok := ResolvedUpstreamModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "gpt-4o-2024-08-06", upstreamModel)
	publicModel, ok := RequestedPublicModelFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "public-gpt", publicModel)
}

func TestPlatformAssetBillingFactsOverrideDefaultValues(t *testing.T) {
	platformID := int64(3)
	subscriptionID := int64(22)
	ctx := WithGatewayPlatformAssetContext(context.Background(), &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{PlatformID: platformID, AccountPlatform: PlatformOpenAI},
		BillingAsset: &ResolvedBillingAsset{
			Source:         BillingSourceSubscription,
			SubscriptionID: &subscriptionID,
			RateMultiplier: 0.5,
		},
		SchedulingScope: PlatformSchedulingScope{PlatformID: platformID, AccountPlatform: PlatformOpenAI},
	})

	token, image, video := overridePlatformAssetBillingMultipliers(ctx, 3, 4, 5)
	require.Equal(t, 0.5, token)
	require.Equal(t, 0.5, image)
	require.Equal(t, 0.5, video)

	usageLog := &UsageLog{}
	applyPlatformAssetUsageAttribution(ctx, usageLog)
	require.Equal(t, platformID, *usageLog.PlatformID)
	require.Equal(t, BillingSourceSubscription, *usageLog.BillingSourceType)
}

func TestPricingInputForRequestPreservesPlatformAndPublicModel(t *testing.T) {
	ctx := WithGatewayPlatformAssetContext(context.Background(), &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID:      1,
			PlatformCode:    "codex",
			AccountPlatform: PlatformOpenAI,
			RequestedModel:  "gpt-5.6-sol",
			UpstreamModel:   "gpt-5.6-upstream",
		},
		SchedulingScope: PlatformSchedulingScope{PlatformID: 1, PlatformCode: "codex", AccountPlatform: PlatformOpenAI},
	})

	input := pricingInputForRequest(ctx, nil, "gpt-5.6-upstream")
	require.Equal(t, PlatformOpenAI, input.Adapter)
	require.Equal(t, "codex", input.PlatformCode)
	require.Equal(t, "gpt-5.6-sol", input.PublicModel)
	require.Equal(t, "gpt-5.6-upstream", input.Model)
}
