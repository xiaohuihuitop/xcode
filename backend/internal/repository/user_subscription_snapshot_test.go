package repository

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionEntityToServiceKeepsPlanSnapshot(t *testing.T) {
	planID := int64(15)
	dailyLimit := 12.5
	weeklyLimit := 50.0
	monthlyLimit := 180.0
	entity := &dbent.UserSubscription{
		ID:                      100,
		UserID:                  9,
		SubscriptionPlanID:      &planID,
		PlanNameSnapshot:        "Growth",
		DailyLimitUsdSnapshot:   &dailyLimit,
		WeeklyLimitUsdSnapshot:  &weeklyLimit,
		MonthlyLimitUsdSnapshot: &monthlyLimit,
		RateMultiplierSnapshot:  1.4,
	}

	subscription := userSubscriptionEntityToService(entity)

	require.Equal(t, int64(15), *subscription.SubscriptionPlanID)
	require.Equal(t, "Growth", subscription.PlanNameSnapshot)
	require.Equal(t, 12.5, *subscription.DailyLimitUSDSnapshot)
	require.Equal(t, 50.0, *subscription.WeeklyLimitUSDSnapshot)
	require.Equal(t, 180.0, *subscription.MonthlyLimitUSDSnapshot)
	require.Equal(t, 1.4, subscription.RateMultiplierSnapshot)
}
