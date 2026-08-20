//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionSummaryItemUsesInstanceSnapshotLimits(t *testing.T) {
	dailyLimit := 10.0
	weeklyLimit := 20.0
	monthlyLimit := 30.0
	subscription := service.UserSubscription{
		ID:                      1,
		PlanNameSnapshot:        "Premium",
		DailyLimitUSDSnapshot:   &dailyLimit,
		WeeklyLimitUSDSnapshot:  &weeklyLimit,
		MonthlyLimitUSDSnapshot: &monthlyLimit,
	}

	item := subscriptionSummaryItemFromService(subscription)

	require.Equal(t, "Premium", item.PlanName)
	require.Equal(t, dailyLimit, item.DailyLimitUSD)
	require.Equal(t, weeklyLimit, item.WeeklyLimitUSD)
	require.Equal(t, monthlyLimit, item.MonthlyLimitUSD)
}
