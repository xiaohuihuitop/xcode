package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dailyResetTrackingUserSubRepo struct {
	userSubRepoNoop
	resetDailyCalled bool
}

func (r *dailyResetTrackingUserSubRepo) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	r.resetDailyCalled = true
	return nil
}

func TestUserSubscriptionNeedsDailyReset_DailyCardKeepsOneTimeQuota(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	windowStart := startOfDay(start)
	sub := &UserSubscription{StartsAt: start, ExpiresAt: start.Add(24 * time.Hour), DailyWindowStart: &windowStart}

	require.True(t, sub.HasOneTimeDailyQuota())
	require.False(t, sub.NeedsDailyResetAt(windowStart.Add(25*time.Hour)))
}

func TestUserSubscriptionNeedsDailyReset_MultiDaySubscriptionStillRefreshes(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	windowStart := startOfDay(start)
	sub := &UserSubscription{StartsAt: start, ExpiresAt: start.AddDate(0, 0, 2), DailyWindowStart: &windowStart}

	require.False(t, sub.HasOneTimeDailyQuota())
	require.True(t, sub.NeedsDailyResetAt(windowStart.Add(24*time.Hour)))
}

func TestUserSubscriptionDailyResetTime_DailyCardReturnsExpiry(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	windowStart := startOfDay(start)
	expiresAt := start.Add(24 * time.Hour)
	sub := &UserSubscription{StartsAt: start, ExpiresAt: expiresAt, DailyWindowStart: &windowStart}

	resetAt := sub.DailyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, expiresAt, *resetAt)
}

func TestCheckAndResetWindows_DailyCardDoesNotResetDailyUsage(t *testing.T) {
	now := time.Now()
	startsAt := now.Add(-23 * time.Hour)
	windowStart := now.Add(-25 * time.Hour)
	repo := &dailyResetTrackingUserSubRepo{}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	sub := &UserSubscription{ID: 1, UserID: 10, StartsAt: startsAt, ExpiresAt: startsAt.Add(24 * time.Hour), DailyUsageUSD: 10, DailyWindowStart: &windowStart}

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.False(t, repo.resetDailyCalled)
	require.Equal(t, 10.0, sub.DailyUsageUSD)
}

func TestCheckAndResetWindows_MultiDaySubscriptionResetsDailyUsage(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(-48 * time.Hour)
	windowStart := now.Add(-25 * time.Hour)
	repo := &dailyResetTrackingUserSubRepo{}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := &UserSubscription{ID: 1, UserID: 10, StartsAt: startsAt, ExpiresAt: startsAt.AddDate(0, 0, 4), DailyUsageUSD: 10, DailyWindowStart: &windowStart}

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.True(t, repo.resetDailyCalled)
	require.Zero(t, sub.DailyUsageUSD)
}

func TestValidateAndCheckLimits_DailyCardDoesNotAllowSecondQuota(t *testing.T) {
	start := time.Now().Add(-23 * time.Hour)
	windowStart := time.Now().Add(-25 * time.Hour)
	dailyLimit := 10.0
	sub := &UserSubscription{
		Status: SubscriptionStatusActive, StartsAt: start, ExpiresAt: start.Add(24 * time.Hour),
		DailyWindowStart: &windowStart, DailyUsageUSD: dailyLimit + 0.01, DailyLimitUSDSnapshot: &dailyLimit,
	}
	svc := NewSubscriptionService(userSubRepoNoop{}, nil, nil, nil)

	needsMaintenance, err := svc.ValidateAndCheckLimits(sub)
	require.False(t, needsMaintenance)
	require.True(t, errors.Is(err, ErrDailyLimitExceeded))
	require.Equal(t, dailyLimit+0.01, sub.DailyUsageUSD)
}
