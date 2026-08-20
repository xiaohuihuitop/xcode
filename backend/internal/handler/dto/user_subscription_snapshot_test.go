//go:build unit

package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionFromServiceIncludesPlanSnapshot(t *testing.T) {
	planID := int64(17)
	dailyLimit := 25.0
	weeklyLimit := 80.0
	monthlyLimit := 260.0

	got := UserSubscriptionFromService(&service.UserSubscription{
		ID:                      9,
		SubscriptionPlanID:      &planID,
		PlanNameSnapshot:        "Professional",
		DailyLimitUSDSnapshot:   &dailyLimit,
		WeeklyLimitUSDSnapshot:  &weeklyLimit,
		MonthlyLimitUSDSnapshot: &monthlyLimit,
		RateMultiplierSnapshot:  1.25,
	})

	require.Equal(t, &planID, got.SubscriptionPlanID)
	require.Equal(t, "Professional", got.PlanNameSnapshot)
	require.Equal(t, &dailyLimit, got.DailyLimitUSDSnapshot)
	require.Equal(t, &weeklyLimit, got.WeeklyLimitUSDSnapshot)
	require.Equal(t, &monthlyLimit, got.MonthlyLimitUSDSnapshot)
	require.Equal(t, 1.25, got.RateMultiplierSnapshot)
}
