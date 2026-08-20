package service

import (
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func applySubscriptionPlanToRedeemCode(code *RedeemCode, plan *dbent.SubscriptionPlan) error {
	if code == nil || plan == nil || plan.ID <= 0 || plan.ValidityDays <= 0 {
		return ErrSubscriptionPlanInvalid
	}

	validityDays := normalizeAssignValidityDays(psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit))
	planID := plan.ID
	code.Type = RedeemTypeSubscription
	code.SubscriptionPlanID = &planID
	code.ValidityDays = validityDays
	code.PlanNameSnapshot = plan.Name
	code.DailyLimitUSDSnapshot = copyFloat64(plan.DailyLimitUsd)
	code.WeeklyLimitUSDSnapshot = copyFloat64(plan.WeeklyLimitUsd)
	code.MonthlyLimitUSDSnapshot = copyFloat64(plan.MonthlyLimitUsd)
	code.RateMultiplierSnapshot = plan.RateMultiplier
	return nil
}

func (r *RedeemCode) hasSubscriptionPlanSnapshot() bool {
	return r != nil && r.SubscriptionPlanID != nil && *r.SubscriptionPlanID > 0 && r.PlanNameSnapshot != "" && r.ValidityDays > 0
}

func subscriptionFromRedeemCode(
	code *RedeemCode,
	userID int64,
	notes string,
	now time.Time,
) (*UserSubscription, error) {
	if code == nil || userID <= 0 || !code.hasSubscriptionPlanSnapshot() {
		return nil, fmt.Errorf("%w: subscription redeem code snapshot is invalid", ErrSubscriptionPlanInvalid)
	}

	expiresAt := now.AddDate(0, 0, normalizeAssignValidityDays(code.ValidityDays))
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}
	return &UserSubscription{
		UserID:                  userID,
		SubscriptionPlanID:      copyRedeemCodeInt64(code.SubscriptionPlanID),
		PlanNameSnapshot:        code.PlanNameSnapshot,
		DailyLimitUSDSnapshot:   copyFloat64(code.DailyLimitUSDSnapshot),
		WeeklyLimitUSDSnapshot:  copyFloat64(code.WeeklyLimitUSDSnapshot),
		MonthlyLimitUSDSnapshot: copyFloat64(code.MonthlyLimitUSDSnapshot),
		RateMultiplierSnapshot:  code.RateMultiplierSnapshot,
		StartsAt:                now,
		ExpiresAt:               expiresAt,
		Status:                  SubscriptionStatusActive,
		AssignedAt:              now,
		Notes:                   notes,
		CreatedAt:               now,
		UpdatedAt:               now,
	}, nil
}

func copyRedeemCodeInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
