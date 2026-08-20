package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionFromPlanCopiesImmutableTerms(t *testing.T) {
	dailyLimit := 25.0
	weeklyLimit := 80.0
	monthlyLimit := 260.0
	plan := &dbent.SubscriptionPlan{
		ID:              17,
		Name:            "Professional",
		ValidityDays:    30,
		DailyLimitUsd:   &dailyLimit,
		WeeklyLimitUsd:  &weeklyLimit,
		MonthlyLimitUsd: &monthlyLimit,
		RateMultiplier:  1.25,
	}
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)

	sub, err := subscriptionFromPlan(plan, AssignSubscriptionFromPlanInput{
		UserID:     42,
		AssignedBy: 7,
		Notes:      "payment order 101",
	}, now)

	require.NoError(t, err)
	require.Equal(t, int64(42), sub.UserID)
	require.Equal(t, int64(17), *sub.SubscriptionPlanID)
	require.Equal(t, "Professional", sub.PlanNameSnapshot)
	require.Equal(t, 25.0, *sub.DailyLimitUSDSnapshot)
	require.Equal(t, 80.0, *sub.WeeklyLimitUSDSnapshot)
	require.Equal(t, 260.0, *sub.MonthlyLimitUSDSnapshot)
	require.Equal(t, 1.25, sub.RateMultiplierSnapshot)
	require.Equal(t, now, sub.StartsAt)
	require.Equal(t, now.AddDate(0, 0, 30), sub.ExpiresAt)
	require.Equal(t, SubscriptionStatusActive, sub.Status)
	require.Equal(t, int64(7), *sub.AssignedBy)

	plan.Name = "Changed later"
	*plan.DailyLimitUsd = 999
	plan.RateMultiplier = 9.99
	require.Equal(t, "Professional", sub.PlanNameSnapshot)
	require.Equal(t, 25.0, *sub.DailyLimitUSDSnapshot)
	require.Equal(t, 1.25, sub.RateMultiplierSnapshot)
}

func TestSubscriptionFromPlanRejectsMissingValidity(t *testing.T) {
	_, err := subscriptionFromPlan(&dbent.SubscriptionPlan{ID: 1}, AssignSubscriptionFromPlanInput{UserID: 3}, time.Now())
	require.Error(t, err)
}

func TestSubscriptionFromPlanConvertsWeeklyValidityToDays(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	plan := &dbent.SubscriptionPlan{
		ID:           1,
		Name:         "Two weeks",
		ValidityDays: 2,
		ValidityUnit: "weeks",
	}

	subscription, err := subscriptionFromPlan(plan, AssignSubscriptionFromPlanInput{UserID: 3}, now)

	require.NoError(t, err)
	require.Equal(t, now.AddDate(0, 0, 14), subscription.ExpiresAt)
}

func TestValidateAndCheckLimitsPrefersSubscriptionSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	snapshotLimit := 10.0
	subscription := &UserSubscription{
		Status:                SubscriptionStatusActive,
		ExpiresAt:             now.Add(time.Hour),
		DailyUsageUSD:         snapshotLimit,
		DailyLimitUSDSnapshot: &snapshotLimit,
	}
	svc := NewSubscriptionService(userSubRepoNoop{}, nil, nil, nil)
	svc.now = func() time.Time { return now }

	_, err := svc.ValidateAndCheckLimits(subscription)

	require.ErrorIs(t, err, ErrDailyLimitExceeded)
}

func TestCheckUsageLimitsPrefersSubscriptionSnapshot(t *testing.T) {
	snapshotLimit := 10.0
	subscription := &UserSubscription{
		DailyUsageUSD:         9,
		DailyLimitUSDSnapshot: &snapshotLimit,
	}
	svc := NewSubscriptionService(userSubRepoNoop{}, nil, nil, nil)

	err := svc.CheckUsageLimits(context.Background(), subscription, 2)

	require.ErrorIs(t, err, ErrDailyLimitExceeded)
}
