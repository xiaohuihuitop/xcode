//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type subscriptionInstanceCacheStub struct {
	billingCacheWorkerStub
	data               *SubscriptionCacheData
	lastSubscriptionID atomic.Int64
	updateCalls        atomic.Int64
}

func (s *subscriptionInstanceCacheStub) GetSubscriptionCache(
	_ context.Context,
	_ int64,
	subscriptionID int64,
) (*SubscriptionCacheData, error) {
	s.lastSubscriptionID.Store(subscriptionID)
	return s.data, nil
}

func (s *subscriptionInstanceCacheStub) UpdateSubscriptionUsage(
	_ context.Context,
	_ int64,
	subscriptionID int64,
	_ float64,
) error {
	s.lastSubscriptionID.Store(subscriptionID)
	s.updateCalls.Add(1)
	return nil
}

func TestCheckBillingEligibilityUsesSubscriptionInstance(t *testing.T) {
	limit := 10.0
	expiresAt := time.Now().Add(time.Hour)
	cache := &subscriptionInstanceCacheStub{data: &SubscriptionCacheData{
		SubscriptionID: 101,
		Status:         SubscriptionStatusActive,
		ExpiresAt:      expiresAt,
		DailyUsage:     1,
		DailyLimitUSD:  &limit,
	}}
	svc := &BillingCacheService{cache: cache, cfg: &config.Config{}}
	subscription := &UserSubscription{
		ID:                    101,
		UserID:                7,
		Status:                SubscriptionStatusActive,
		ExpiresAt:             expiresAt,
		DailyLimitUSDSnapshot: &limit,
	}
	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 7},
		nil,
		subscription,
		PlatformOpenAI,
	)

	require.NoError(t, err)
	require.Equal(t, subscription.ID, cache.lastSubscriptionID.Load())
}

func TestQueueUpdateSubscriptionUsageUpdatesConcreteSubscriptionInstance(t *testing.T) {
	cache := &subscriptionInstanceCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	svc.QueueUpdateSubscriptionUsage(7, 101, 0.25)

	require.Eventually(t, func() bool {
		return cache.updateCalls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(101), cache.lastSubscriptionID.Load())
}
