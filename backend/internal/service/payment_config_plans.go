package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return validatePlanBillingTerms(req.DailyLimitUSD, req.WeeklyLimitUSD, req.MonthlyLimitUSD, req.RateMultiplier)
}

func validatePlanBillingTerms(daily, weekly, monthly, rateMultiplier *float64) error {
	for _, term := range []struct {
		name  string
		value *float64
	}{
		{"daily limit", daily},
		{"weekly limit", weekly},
		{"monthly limit", monthly},
	} {
		if err := validateNonNegativePlanValue(term.name, term.value); err != nil {
			return err
		}
	}
	return validateNonNegativePlanValue("rate multiplier", rateMultiplier)
}

func validateNonNegativePlanValue(name string, value *float64) error {
	if value == nil {
		return nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return infraerrors.BadRequest("PLAN_BILLING_TERM_INVALID", name+" must be a finite value >= 0")
	}
	return nil
}

func normalizePlanLimit(value *float64) *float64 {
	if value == nil || *value == 0 {
		return nil
	}
	return value
}

func planRateMultiplier(value *float64) float64 {
	if value == nil {
		return 1
	}
	return *value
}

// --- Plan CRUD ---

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanRequired(req.Name, req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice); err != nil {
		return nil, err
	}
	if err := validatePlanBillingTerms(req.DailyLimitUSD, req.WeeklyLimitUSD, req.MonthlyLimitUSD, req.RateMultiplier); err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	b := s.entClient.SubscriptionPlan.Create().
		SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder).
		SetRateMultiplier(planRateMultiplier(req.RateMultiplier))
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	if limit := normalizePlanLimit(req.DailyLimitUSD); limit != nil {
		b.SetDailyLimitUsd(*limit)
	}
	if limit := normalizePlanLimit(req.WeeklyLimitUSD); limit != nil {
		b.SetWeeklyLimitUsd(*limit)
	}
	if limit := normalizePlanLimit(req.MonthlyLimitUSD); limit != nil {
		b.SetMonthlyLimitUsd(*limit)
	}
	return b.Save(ctx)
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	if req.RateMultiplier != nil {
		u.SetRateMultiplier(*req.RateMultiplier)
	}
	if req.DailyLimitUSD != nil {
		setPlanDailyLimit(u, req.DailyLimitUSD)
	}
	if req.WeeklyLimitUSD != nil {
		setPlanWeeklyLimit(u, req.WeeklyLimitUSD)
	}
	if req.MonthlyLimitUSD != nil {
		setPlanMonthlyLimit(u, req.MonthlyLimitUSD)
	}
	return u.Save(ctx)
}

func setPlanDailyLimit(update *dbent.SubscriptionPlanUpdateOne, limit *float64) {
	if value := normalizePlanLimit(limit); value != nil {
		update.SetDailyLimitUsd(*value)
		return
	}
	update.ClearDailyLimitUsd()
}

func setPlanWeeklyLimit(update *dbent.SubscriptionPlanUpdateOne, limit *float64) {
	if value := normalizePlanLimit(limit); value != nil {
		update.SetWeeklyLimitUsd(*value)
		return
	}
	update.ClearWeeklyLimitUsd()
}

func setPlanMonthlyLimit(update *dbent.SubscriptionPlanUpdateOne, limit *float64) {
	if value := normalizePlanLimit(limit); value != nil {
		update.SetMonthlyLimitUsd(*value)
		return
	}
	update.ClearMonthlyLimitUsd()
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}
