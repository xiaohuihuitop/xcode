//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateAndCheckLimitsRejectsUsageAtLimit(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	limit := 10.0
	tests := []struct {
		name     string
		setLimit func(*UserSubscription)
		setUsage func(*UserSubscription)
		expected error
	}{
		{
			name:     "daily",
			setLimit: func(sub *UserSubscription) { sub.DailyLimitUSDSnapshot = &limit },
			setUsage: func(sub *UserSubscription) { sub.DailyUsageUSD = limit },
			expected: ErrDailyLimitExceeded,
		},
		{
			name:     "weekly",
			setLimit: func(sub *UserSubscription) { sub.WeeklyLimitUSDSnapshot = &limit },
			setUsage: func(sub *UserSubscription) { sub.WeeklyUsageUSD = limit },
			expected: ErrWeeklyLimitExceeded,
		},
		{
			name:     "monthly",
			setLimit: func(sub *UserSubscription) { sub.MonthlyLimitUSDSnapshot = &limit },
			setUsage: func(sub *UserSubscription) { sub.MonthlyUsageUSD = limit },
			expected: ErrMonthlyLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windowStart := now.Add(-time.Hour)
			sub := &UserSubscription{
				Status:             SubscriptionStatusActive,
				StartsAt:           now.Add(-time.Hour),
				ExpiresAt:          now.Add(time.Hour),
				DailyWindowStart:   &windowStart,
				WeeklyWindowStart:  &windowStart,
				MonthlyWindowStart: &windowStart,
			}
			tt.setUsage(sub)
			tt.setLimit(sub)
			svc := NewSubscriptionService(userSubRepoNoop{}, nil, nil, nil)
			svc.now = func() time.Time { return now }

			needsMaintenance, err := svc.ValidateAndCheckLimits(sub)

			require.ErrorIs(t, err, tt.expected)
			require.False(t, needsMaintenance)
		})
	}
}
