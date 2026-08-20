//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type dailyMidnightResetRepo struct {
	userSubRepoNoop
	resetCalled    bool
	newWindowStart time.Time
}

func (r *dailyMidnightResetRepo) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, newWindowStart time.Time) error {
	r.resetCalled = true
	r.newWindowStart = newWindowStart
	return nil
}

func TestSubscriptionDailyWindowUsesConfiguredTimezoneMidnight(t *testing.T) {
	base := timezone.StartOfDay(time.Date(2026, 8, 6, 12, 0, 0, 0, timezone.Location()))
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute)
	now := base.AddDate(0, 0, 1).Add(5 * time.Minute)
	repo := &dailyMidnightResetRepo{}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := &UserSubscription{
		ID: 1, UserID: 10,
		StartsAt: base.AddDate(0, 0, -3), ExpiresAt: base.AddDate(0, 0, 30),
		DailyUsageUSD: 43.34, DailyWindowStart: &manualResetAt,
	}

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))
	require.True(t, repo.resetCalled)
	require.Equal(t, base.AddDate(0, 0, 1), repo.newWindowStart)
	require.Zero(t, sub.DailyUsageUSD)
	require.Equal(t, base.AddDate(0, 0, 1), *sub.DailyWindowStart)
}

func TestSubscriptionDailyWindowDoesNotResetWithinSameCalendarDay(t *testing.T) {
	base := timezone.StartOfDay(time.Date(2026, 8, 6, 12, 0, 0, 0, timezone.Location()))
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute)
	sub := &UserSubscription{
		StartsAt: base.AddDate(0, 0, -3), ExpiresAt: base.AddDate(0, 0, 30),
		DailyWindowStart: &manualResetAt,
	}

	require.False(t, sub.NeedsDailyResetAt(base.Add(23*time.Hour+59*time.Minute)))
	require.True(t, sub.NeedsDailyResetAt(base.AddDate(0, 0, 1).Add(time.Minute)))
	require.Equal(t, base.AddDate(0, 0, 1), *sub.DailyResetTime())
}

func TestSubscriptionOneDayCardKeepsOneTimeDailyQuota(t *testing.T) {
	base := timezone.StartOfDay(time.Date(2026, 8, 6, 12, 0, 0, 0, timezone.Location()))
	start := base.Add(17 * time.Hour)
	sub := &UserSubscription{StartsAt: start, ExpiresAt: start.AddDate(0, 0, 1), DailyWindowStart: &base}
	require.False(t, sub.NeedsDailyResetAt(base.AddDate(0, 0, 1).Add(2*time.Hour)))
}
