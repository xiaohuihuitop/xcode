package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type subscriptionPlanCandidatesRepoStub struct {
	userSubRepoNoop
	candidates       []UserSubscription
	requestedPlanIDs []int64
	calls            int
}

type subscriptionWindowMaintenanceRepoStub struct {
	userSubRepoNoop
	candidates     []UserSubscription
	resetDailyIDs  []int64
	resetDailyTime time.Time
}

func (r *subscriptionWindowMaintenanceRepoStub) ListActiveByUserID(
	_ context.Context,
	_ int64,
) ([]UserSubscription, error) {
	return cloneUserSubscriptions(r.candidates), nil
}

func (r *subscriptionWindowMaintenanceRepoStub) ResetDailyUsage(
	_ context.Context,
	subscriptionID int64,
	_ *time.Time,
	resetAt time.Time,
) error {
	r.resetDailyIDs = append(r.resetDailyIDs, subscriptionID)
	r.resetDailyTime = resetAt
	for index := range r.candidates {
		if r.candidates[index].ID == subscriptionID {
			r.candidates[index].DailyUsageUSD = 0
			r.candidates[index].DailyWindowStart = copyTime(&resetAt)
		}
	}
	return nil
}

func (r *subscriptionWindowMaintenanceRepoStub) GetByID(
	_ context.Context,
	subscriptionID int64,
) (*UserSubscription, error) {
	for index := range r.candidates {
		if r.candidates[index].ID == subscriptionID {
			return cloneUserSubscription(&r.candidates[index]), nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func (r *subscriptionPlanCandidatesRepoStub) ListActiveByUserID(
	_ context.Context,
	_ int64,
) ([]UserSubscription, error) {
	r.calls++
	return append([]UserSubscription(nil), r.candidates...), nil
}

func (r *subscriptionPlanCandidatesRepoStub) ListActiveByUserIDAndPlanIDs(
	_ context.Context,
	_ int64,
	planIDs []int64,
) ([]UserSubscription, error) {
	r.calls++
	r.requestedPlanIDs = append([]int64(nil), planIDs...)
	return append([]UserSubscription(nil), r.candidates...), nil
}

func TestListActiveSubscriptionsByPlanIDsUsesDistinctSortedPlanIDs(t *testing.T) {
	repo := &subscriptionPlanCandidatesRepoStub{
		candidates: []UserSubscription{{ID: 11}, {ID: 12}},
	}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptionsByPlanIDs(
		context.Background(),
		10,
		[]int64{30, 10, 30, 20},
	)

	require.NoError(t, err)
	require.Equal(t, []int64{10, 20, 30}, repo.requestedPlanIDs)
	require.Equal(t, []int64{11, 12}, []int64{subscriptions[0].ID, subscriptions[1].ID})
	require.Equal(t, 1, repo.calls)
}

func TestListActiveSubscriptionsByPlanIDsSkipsRepositoryForNoPlans(t *testing.T) {
	repo := &subscriptionPlanCandidatesRepoStub{}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptionsByPlanIDs(context.Background(), 10, nil)

	require.NoError(t, err)
	require.Empty(t, subscriptions)
	require.Zero(t, repo.calls)
}

func TestListActiveSubscriptionsUsesAllActiveUserSubscriptions(t *testing.T) {
	repo := &subscriptionPlanCandidatesRepoStub{
		candidates: []UserSubscription{{ID: 11}, {ID: 12}},
	}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptions(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, []int64{11, 12}, []int64{subscriptions[0].ID, subscriptions[1].ID})
	require.Equal(t, 1, repo.calls)
}

func TestListActiveSubscriptionsPreservesExpiredWindowForMaintenance(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.AddDate(0, 0, -2)
	repo := &subscriptionPlanCandidatesRepoStub{
		candidates: []UserSubscription{{
			ID:               11,
			StartsAt:         now.AddDate(0, 0, -5),
			ExpiresAt:        now.AddDate(0, 0, 5),
			DailyUsageUSD:    101.11,
			DailyWindowStart: &windowStart,
		}},
	}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptions(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, subscriptions, 1)
	require.Equal(t, 101.11, subscriptions[0].DailyUsageUSD)
	require.NotNil(t, subscriptions[0].DailyWindowStart)
	require.Equal(t, windowStart, *subscriptions[0].DailyWindowStart)
}

func TestResolveBillingAssetMaintainsExpiredDailyWindowBeforeSelection(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	currentDayStart := timezone.StartOfDay(now)
	staleDayStart := currentDayStart.AddDate(0, 0, -2)
	weeklyWindowStart := now.AddDate(0, 0, -1)
	monthlyWindowStart := now.AddDate(0, 0, -1)
	dailyLimit := 100.0
	firstPlanID := int64(10)
	secondPlanID := int64(20)
	repo := &subscriptionWindowMaintenanceRepoStub{candidates: []UserSubscription{
		{
			ID: 18, UserID: 2, SubscriptionPlanID: &firstPlanID,
			Status: SubscriptionStatusActive, StartsAt: now.AddDate(0, 0, -10), ExpiresAt: now.AddDate(0, 0, 13),
			DailyLimitUSDSnapshot: &dailyLimit, DailyUsageUSD: 101.11, DailyWindowStart: &staleDayStart,
			WeeklyWindowStart: &weeklyWindowStart, MonthlyWindowStart: &monthlyWindowStart,
		},
		{
			ID: 20, UserID: 2, SubscriptionPlanID: &secondPlanID,
			Status: SubscriptionStatusActive, StartsAt: now.AddDate(0, 0, -10), ExpiresAt: now.AddDate(0, 0, 21),
			DailyLimitUSDSnapshot: &dailyLimit, DailyUsageUSD: 90.65, DailyWindowStart: &currentDayStart,
			WeeklyWindowStart: &weeklyWindowStart, MonthlyWindowStart: &monthlyWindowStart,
		},
	}}
	subscriptionService := NewSubscriptionService(repo, nil, nil, nil)
	subscriptionService.now = func() time.Time { return now }
	apiKey := &APIKey{UserID: 2, AllowAllSubscriptions: true, AllowBalance: false}

	asset, err := (&APIKeyService{}).ResolveBillingAssetForRequest(
		context.Background(),
		apiKey,
		subscriptionService,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, asset)
	require.Equal(t, int64(18), *asset.SubscriptionID)
	require.Equal(t, []int64{18}, repo.resetDailyIDs)
	require.Equal(t, currentDayStart, repo.resetDailyTime)
	require.Zero(t, asset.Subscription.DailyUsageUSD)
	require.Equal(t, currentDayStart, *asset.Subscription.DailyWindowStart)
}
