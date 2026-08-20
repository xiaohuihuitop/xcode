package service

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionPlanNotFound = infraerrors.NotFound("SUBSCRIPTION_PLAN_NOT_FOUND", "subscription plan not found")
	ErrSubscriptionPlanInvalid  = infraerrors.BadRequest("SUBSCRIPTION_PLAN_INVALID", "subscription plan terms are invalid")
)

// AssignSubscriptionFromPlanInput identifies the user and immutable plan source
// for one newly issued subscription instance.
type AssignSubscriptionFromPlanInput struct {
	UserID     int64
	PlanID     int64
	AssignedBy int64
	Notes      string
}

// BulkAssignSubscriptionFromPlanInput creates one independent instance for
// each user entry. Repeated user IDs intentionally create repeated instances.
type BulkAssignSubscriptionFromPlanInput struct {
	UserIDs    []int64
	PlanID     int64
	AssignedBy int64
	Notes      string
}

// AssignSubscriptionFromPlan creates a new instance for every issuance. It
// never extends an existing subscription, so repeated purchases stay separate.
func (s *SubscriptionService) AssignSubscriptionFromPlan(
	ctx context.Context,
	input *AssignSubscriptionFromPlanInput,
) (*UserSubscription, error) {
	return s.assignSubscriptionFromPlan(ctx, input, false)
}

// BulkAssignSubscriptionFromPlan issues independent instances from a plan.
// It deliberately never reuses or extends a prior subscription instance.
func (s *SubscriptionService) BulkAssignSubscriptionFromPlan(
	ctx context.Context,
	input *BulkAssignSubscriptionFromPlanInput,
) (*BulkAssignResult, error) {
	if input == nil || input.PlanID <= 0 {
		return nil, ErrSubscriptionPlanInvalid
	}
	result := &BulkAssignResult{
		Subscriptions: make([]UserSubscription, 0, len(input.UserIDs)),
		Errors:        make([]string, 0),
		Statuses:      make(map[int64]string),
	}
	for _, userID := range input.UserIDs {
		sub, err := s.AssignSubscriptionFromPlan(ctx, &AssignSubscriptionFromPlanInput{
			UserID:     userID,
			PlanID:     input.PlanID,
			AssignedBy: input.AssignedBy,
			Notes:      input.Notes,
		})
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			result.Statuses[userID] = "failed"
			continue
		}
		result.SuccessCount++
		result.CreatedCount++
		result.Subscriptions = append(result.Subscriptions, *sub)
		result.Statuses[userID] = "created"
	}
	return result, nil
}

func (s *SubscriptionService) assignSubscriptionFromPlan(
	ctx context.Context,
	input *AssignSubscriptionFromPlanInput,
	deferCacheInvalidation bool,
) (*UserSubscription, error) {
	if input == nil || input.UserID <= 0 || input.PlanID <= 0 {
		return nil, ErrSubscriptionPlanInvalid
	}
	client := s.subscriptionPlanClient(ctx)
	if client == nil {
		return nil, fmt.Errorf("subscription plan client is unavailable")
	}

	plan, err := client.SubscriptionPlan.Get(ctx, input.PlanID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrSubscriptionPlanNotFound
		}
		return nil, fmt.Errorf("get subscription plan: %w", err)
	}

	sub, err := subscriptionFromPlan(plan, *input, s.subscriptionNow())
	if err != nil {
		return nil, err
	}
	if err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		return s.userSubRepo.Create(txCtx, sub)
	}); err != nil {
		return nil, fmt.Errorf("create subscription from plan: %w", err)
	}

	s.maybeInvalidateAssignmentCache(sub.UserID, sub.ID, deferCacheInvalidation)
	return s.userSubRepo.GetByID(ctx, sub.ID)
}

func (s *SubscriptionService) subscriptionPlanClient(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return s.entClient
}

func (s *SubscriptionService) subscriptionNow() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func subscriptionFromPlan(
	plan *dbent.SubscriptionPlan,
	input AssignSubscriptionFromPlanInput,
	now time.Time,
) (*UserSubscription, error) {
	if plan == nil || plan.ID <= 0 || plan.ValidityDays <= 0 {
		return nil, ErrSubscriptionPlanInvalid
	}

	validityDays := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	expiresAt := now.AddDate(0, 0, normalizeAssignValidityDays(validityDays))
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}

	planID := plan.ID
	sub := &UserSubscription{
		UserID:                  input.UserID,
		SubscriptionPlanID:      &planID,
		PlanNameSnapshot:        plan.Name,
		DailyLimitUSDSnapshot:   copyFloat64(plan.DailyLimitUsd),
		WeeklyLimitUSDSnapshot:  copyFloat64(plan.WeeklyLimitUsd),
		MonthlyLimitUSDSnapshot: copyFloat64(plan.MonthlyLimitUsd),
		RateMultiplierSnapshot:  plan.RateMultiplier,
		StartsAt:                now,
		ExpiresAt:               expiresAt,
		Status:                  SubscriptionStatusActive,
		AssignedAt:              now,
		Notes:                   input.Notes,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if input.AssignedBy > 0 {
		assignedBy := input.AssignedBy
		sub.AssignedBy = &assignedBy
	}
	return sub, nil
}

func copyFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
