//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformAssetProductCoreAdapterKeepsSelectedSubscription(t *testing.T) {
	planID := int64(17)
	subscriptionID := int64(21)
	subscription := &UserSubscription{ID: subscriptionID, SubscriptionPlanID: &planID, RateMultiplierSnapshot: 0.5}
	adapter := newPlatformAssetProductCoreAdapterForTest(subscription)

	resolution, err := adapter.Resolve(context.Background(), apiKeyWithPlatformAndPlan(3, planID),
		"gpt-4o", "/v1/chat/completions", false)

	require.NoError(t, err)
	require.Equal(t, subscriptionID, *resolution.Decision.BillingAsset.SubscriptionID)
	require.Equal(t, subscription, resolution.Subscription)
	require.Equal(t, 0.5, resolution.Decision.BillingAsset.RateMultiplier)
}

func TestPlatformAssetProductCoreAdapterMapsBalanceErrorBackToService(t *testing.T) {
	adapter := newPlatformAssetProductCoreAdapterForInsufficientBalance()
	_, err := adapter.Resolve(context.Background(), apiKeyWithBalanceOnly(3),
		"gpt-4o", "/v1/chat/completions", false)

	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestPlatformAssetProductCoreAdapterFallsBackToBalance(t *testing.T) {
	fallback := NewPlatformAssetProductCoreAdapter(
		&APIKeyService{globalBalanceRateProvider: globalBalanceRateProviderStub{rate: 1.75}},
		&assetSubscriptionResolverStub{
			candidates:   []UserSubscription{{ID: 21, UserID: 7, SubscriptionPlanID: int64Pointer(17)}},
			validateErrs: map[int64]error{21: ErrDailyLimitExceeded},
		},
		platformModelResolverStub{resolved: &ResolvedPlatformModel{
			PlatformID:           3,
			AccountPlatform:      PlatformOpenAI,
			EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
		}},
	)
	resolution, err := fallback.Resolve(context.Background(), apiKeyWithPlatformAndPlan(3, 17),
		"gpt-4o", "/v1/chat/completions", false)

	require.NoError(t, err)
	require.Equal(t, BillingSourceBalance, resolution.Decision.BillingAsset.Source)
	require.Equal(t, 1.75, resolution.Decision.BillingAsset.RateMultiplier)
}

func TestPlatformAssetProductCoreAdapterSkipsBillingInSimpleMode(t *testing.T) {
	simple, err := newPlatformAssetProductCoreAdapterForInsufficientBalance().Resolve(
		context.Background(), apiKeyWithBalanceOnly(3), "gpt-4o", "/v1/chat/completions", true,
	)

	require.NoError(t, err)
	require.Nil(t, simple.Decision.BillingAsset)
}

func apiKeyWithPlatformAndPlan(platformID, planID int64) *APIKey {
	return &APIKey{
		ID: 10, UserID: 7, User: &User{ID: 7, Balance: 10},
		AllowedPlatformIDs: []int64{platformID}, AllowedSubscriptionPlanIDs: []int64{planID}, AllowBalance: true,
	}
}

func apiKeyWithBalanceOnly(platformID int64) *APIKey {
	return &APIKey{
		ID: 10, UserID: 7, User: &User{ID: 7, Balance: 0},
		AllowedPlatformIDs: []int64{platformID}, AllowBalance: true,
	}
}

func newPlatformAssetProductCoreAdapterForTest(subscription *UserSubscription) *PlatformAssetProductCoreAdapter {
	return NewPlatformAssetProductCoreAdapter(
		&APIKeyService{},
		&assetSubscriptionResolverStub{candidates: []UserSubscription{*subscription}},
		platformModelResolverStub{resolved: &ResolvedPlatformModel{
			PlatformID: 3, PlatformCode: "gpt", AccountPlatform: PlatformOpenAI,
			RequestedModel:       "gpt-4o",
			EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
		}},
	)
}

func newPlatformAssetProductCoreAdapterForInsufficientBalance() *PlatformAssetProductCoreAdapter {
	return NewPlatformAssetProductCoreAdapter(
		&APIKeyService{},
		nil,
		platformModelResolverStub{resolved: &ResolvedPlatformModel{
			PlatformID: 3, PlatformCode: "gpt", AccountPlatform: PlatformOpenAI,
			RequestedModel:       "gpt-4o",
			EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
		}},
	)
}

func int64Pointer(value int64) *int64 {
	return &value
}
