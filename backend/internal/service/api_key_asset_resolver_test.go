//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type assetSubscriptionResolverStub struct {
	candidates   []UserSubscription
	validateErrs map[int64]error
	checked      []int64
}

func (s *assetSubscriptionResolverStub) GetActiveSubscription(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func (s *assetSubscriptionResolverStub) ListActiveSubscriptionsByPlanIDs(
	_ context.Context,
	_ int64,
	_ []int64,
) ([]UserSubscription, error) {
	return cloneUserSubscriptions(s.candidates), nil
}

func (s *assetSubscriptionResolverStub) ValidateAndCheckLimits(sub *UserSubscription) (bool, error) {
	s.checked = append(s.checked, sub.ID)
	return false, s.validateErrs[sub.ID]
}

func (s *assetSubscriptionResolverStub) EnsureWindowMaintenance(_ context.Context, sub *UserSubscription) (*UserSubscription, error) {
	return sub, nil
}

type globalBalanceRateProviderStub struct {
	rate float64
}

func (s globalBalanceRateProviderStub) GetGlobalBalanceRateMultiplier(context.Context) float64 {
	return s.rate
}

func TestResolveBillingAssetUsesEarliestUsableSubscriptionThenBalance(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	firstPlanID := int64(10)
	secondPlanID := int64(20)
	resolver := &assetSubscriptionResolverStub{candidates: []UserSubscription{
		{ID: 2, UserID: 7, SubscriptionPlanID: &secondPlanID, ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now.Add(-time.Hour), RateMultiplierSnapshot: 3},
		{ID: 1, UserID: 7, SubscriptionPlanID: &firstPlanID, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now.Add(-2 * time.Hour), RateMultiplierSnapshot: 0.5},
	}}
	apiKey := &APIKey{
		UserID:                     7,
		AllowedSubscriptionPlanIDs: []int64{10, 20},
		AllowBalance:               true,
		User:                       &User{ID: 7, Balance: 100},
	}

	asset, err := (&APIKeyService{}).ResolveBillingAssetForRequest(context.Background(), apiKey, resolver, false)

	require.NoError(t, err)
	require.Equal(t, BillingSourceSubscription, asset.Source)
	require.Equal(t, int64(1), *asset.SubscriptionID)
	require.Equal(t, int64(10), *asset.PlanID)
	require.Equal(t, 0.5, asset.RateMultiplier)
	require.Equal(t, []int64{1}, resolver.checked)
}

func TestResolveBillingAssetSkipsExhaustedSubscriptionAndHonorsBalanceFlag(t *testing.T) {
	planID := int64(10)
	resolver := &assetSubscriptionResolverStub{
		candidates:   []UserSubscription{{ID: 1, UserID: 7, SubscriptionPlanID: &planID, RateMultiplierSnapshot: 2}},
		validateErrs: map[int64]error{1: ErrDailyLimitExceeded},
	}
	apiKey := &APIKey{
		UserID:                     7,
		AllowedSubscriptionPlanIDs: []int64{10},
		AllowBalance:               false,
		User:                       &User{ID: 7, Balance: 100},
	}

	_, err := (&APIKeyService{}).ResolveBillingAssetForRequest(context.Background(), apiKey, resolver, false)

	require.ErrorIs(t, err, ErrNoUsableBillingSource)
	require.Equal(t, []int64{1}, resolver.checked)
}

func TestResolveBillingAssetUsesGlobalBalanceRateWithoutPlanMultiplier(t *testing.T) {
	apiKey := &APIKey{
		UserID:       7,
		AllowBalance: true,
		User:         &User{ID: 7, Balance: 10},
	}
	svc := &APIKeyService{globalBalanceRateProvider: globalBalanceRateProviderStub{rate: 1.75}}

	asset, err := svc.ResolveBillingAssetForRequest(context.Background(), apiKey, &assetSubscriptionResolverStub{}, false)

	require.NoError(t, err)
	require.Equal(t, BillingSourceBalance, asset.Source)
	require.Nil(t, asset.SubscriptionID)
	require.Equal(t, 1.75, asset.RateMultiplier)
}

func TestProvideAPIKeyServiceUsesConfiguredGlobalBalanceRate(t *testing.T) {
	configService := &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingKeyGlobalBalanceRateMultiplier: "1.25",
	}}}
	svc := ProvideAPIKeyService(nil, nil, nil, nil, &config.Config{}, nil, nil, configService)
	apiKey := &APIKey{
		UserID:       7,
		AllowBalance: true,
		User:         &User{ID: 7, Balance: 10},
	}

	asset, err := svc.ResolveBillingAssetForRequest(context.Background(), apiKey, &assetSubscriptionResolverStub{}, false)

	require.NoError(t, err)
	require.Equal(t, 1.25, asset.RateMultiplier)
}
