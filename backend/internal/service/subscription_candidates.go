package service

import (
	"context"
	"sort"
	"time"
)

// ListActiveSubscriptions exposes all active user subscriptions to billing
// candidate selectors such as API keys. Keep this name aligned with the
// narrow resolver interface so new subscription purchases are picked up
// without changing API-key wiring.
func (s *SubscriptionService) ListActiveSubscriptions(
	ctx context.Context,
	userID int64,
) ([]UserSubscription, error) {
	return s.ListActiveUserSubscriptions(ctx, userID)
}

// ListActiveSubscriptionsByPlanIDs returns all active candidates for the
// explicitly authorized plans.
func (s *SubscriptionService) ListActiveSubscriptionsByPlanIDs(
	ctx context.Context,
	userID int64,
	planIDs []int64,
) ([]UserSubscription, error) {
	normalizedPlanIDs := normalizeSubscriptionPlanIDs(planIDs)
	if len(normalizedPlanIDs) == 0 {
		return []UserSubscription{}, nil
	}

	lister, ok := s.userSubRepo.(ActiveUserSubscriptionPlanLister)
	if !ok {
		return nil, ErrSubscriptionNotFound
	}
	subscriptions, err := lister.ListActiveByUserIDAndPlanIDs(ctx, userID, normalizedPlanIDs)
	if err != nil {
		return nil, err
	}
	return cloneUserSubscriptions(subscriptions), nil
}

func normalizeSubscriptionPlanIDs(planIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(planIDs))
	for _, planID := range planIDs {
		if planID > 0 {
			seen[planID] = struct{}{}
		}
	}

	normalized := make([]int64, 0, len(seen))
	for planID := range seen {
		normalized = append(normalized, planID)
	}
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left] < normalized[right]
	})
	return normalized
}

func cloneUserSubscriptions(subscriptions []UserSubscription) []UserSubscription {
	cloned := make([]UserSubscription, len(subscriptions))
	for index := range subscriptions {
		cloned[index] = *cloneUserSubscription(&subscriptions[index])
	}
	return cloned
}

func cloneUserSubscription(subscription *UserSubscription) *UserSubscription {
	if subscription == nil {
		return nil
	}

	cloned := *subscription
	cloned.SubscriptionPlanID = copyInt64(subscription.SubscriptionPlanID)
	cloned.DailyLimitUSDSnapshot = copyFloat64(subscription.DailyLimitUSDSnapshot)
	cloned.WeeklyLimitUSDSnapshot = copyFloat64(subscription.WeeklyLimitUSDSnapshot)
	cloned.MonthlyLimitUSDSnapshot = copyFloat64(subscription.MonthlyLimitUSDSnapshot)
	cloned.DailyWindowStart = copyTime(subscription.DailyWindowStart)
	cloned.WeeklyWindowStart = copyTime(subscription.WeeklyWindowStart)
	cloned.MonthlyWindowStart = copyTime(subscription.MonthlyWindowStart)
	cloned.AssignedBy = copyInt64(subscription.AssignedBy)
	cloned.DeletedAt = copyTime(subscription.DeletedAt)
	return &cloned
}

func copyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
