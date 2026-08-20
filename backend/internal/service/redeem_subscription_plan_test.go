package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestApplySubscriptionPlanToRedeemCodeCapturesImmutableTerms(t *testing.T) {
	dailyLimit := 18.0
	weeklyLimit := 75.0
	monthlyLimit := 260.0
	plan := &dbent.SubscriptionPlan{
		ID:              15,
		Name:            "Professional",
		ValidityDays:    2,
		ValidityUnit:    "weeks",
		DailyLimitUsd:   &dailyLimit,
		WeeklyLimitUsd:  &weeklyLimit,
		MonthlyLimitUsd: &monthlyLimit,
		RateMultiplier:  1.25,
	}
	code := &RedeemCode{Type: RedeemTypeSubscription, Value: 0, Status: StatusUnused}

	require.NoError(t, applySubscriptionPlanToRedeemCode(code, plan))
	require.Equal(t, int64(15), *code.SubscriptionPlanID)
	require.Equal(t, 14, code.ValidityDays)
	require.Equal(t, "Professional", code.PlanNameSnapshot)
	require.Equal(t, 18.0, *code.DailyLimitUSDSnapshot)
	require.Equal(t, 75.0, *code.WeeklyLimitUSDSnapshot)
	require.Equal(t, 260.0, *code.MonthlyLimitUSDSnapshot)
	require.Equal(t, 1.25, code.RateMultiplierSnapshot)

	plan.Name = "Changed later"
	*plan.DailyLimitUsd = 999
	plan.RateMultiplier = 9.99
	require.Equal(t, "Professional", code.PlanNameSnapshot)
	require.Equal(t, 18.0, *code.DailyLimitUSDSnapshot)
	require.Equal(t, 1.25, code.RateMultiplierSnapshot)
}

type planRedeemCodeRepository struct {
	*redeemCodeRepoStub
	created []RedeemCode
}

func (r *planRedeemCodeRepository) Create(_ context.Context, code *RedeemCode) error {
	copy := *code
	r.created = append(r.created, copy)
	return nil
}

type snapshotRedeemCodeRepository struct {
	*redeemCodeRepoStub
}

func (r *snapshotRedeemCodeRepository) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range r.codesByCode {
		if code.ID == id {
			copy := *code
			return &copy, nil
		}
	}
	return nil, ErrRedeemCodeNotFound
}

func TestGenerateRedeemCodesFromPlanCapturesPlanSnapshot(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	dailyLimit := 20.0
	plan, err := client.SubscriptionPlan.Create().
		SetName("Professional").
		SetPrice(29.9).
		SetValidityDays(30).
		SetDailyLimitUsd(dailyLimit).
		SetRateMultiplier(1.25).
		Save(ctx)
	require.NoError(t, err)

	repo := &planRedeemCodeRepository{redeemCodeRepoStub: &redeemCodeRepoStub{}}
	svc := &adminServiceImpl{entClient: client, redeemCodeRepo: repo}

	codes, err := svc.GenerateRedeemCodes(ctx, &GenerateRedeemCodesInput{
		Count:              1,
		Type:               RedeemTypeSubscription,
		SubscriptionPlanID: &plan.ID,
	})

	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.Len(t, repo.created, 1)
	require.Equal(t, plan.ID, *codes[0].SubscriptionPlanID)
	require.Equal(t, "Professional", codes[0].PlanNameSnapshot)
	require.Equal(t, dailyLimit, *codes[0].DailyLimitUSDSnapshot)
	require.Equal(t, 1.25, codes[0].RateMultiplierSnapshot)
}

func TestRedeemSubscriptionCodeCreatesNewSnapshotInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	planID := int64(15)
	dailyLimit := 20.0
	redeemRepo := &snapshotRedeemCodeRepository{redeemCodeRepoStub: &redeemCodeRepoStub{
		codesByCode: map[string]*RedeemCode{
			"PLAN-CODE": {
				ID:                     1,
				Code:                   "PLAN-CODE",
				Type:                   RedeemTypeSubscription,
				Status:                 StatusUnused,
				SubscriptionPlanID:     &planID,
				PlanNameSnapshot:       "Professional",
				DailyLimitUSDSnapshot:  &dailyLimit,
				RateMultiplierSnapshot: 1.25,
				ValidityDays:           30,
			},
		},
	}}
	subscriptionRepo := newSubscriptionUserSubRepoStub()
	subscriptionService := NewSubscriptionService(subscriptionRepo, nil, nil, nil)
	redeemService := NewRedeemService(
		redeemRepo,
		&userRepoStub{user: &User{ID: 42}},
		subscriptionService,
		nil,
		nil,
		client,
		nil,
		nil,
	)

	_, err := redeemService.Redeem(ctx, 42, "PLAN-CODE")

	require.NoError(t, err)
	require.Equal(t, 1, subscriptionRepo.createCalls)
	created, err := subscriptionRepo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(15), *created.SubscriptionPlanID)
	require.Equal(t, "Professional", created.PlanNameSnapshot)
	require.Equal(t, dailyLimit, *created.DailyLimitUSDSnapshot)
	require.Equal(t, 1.25, created.RateMultiplierSnapshot)
}

func TestSubscriptionFromRedeemCodeCreatesIndependentSnapshot(t *testing.T) {
	planID := int64(15)
	dailyLimit := 18.0
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	code := &RedeemCode{
		Type:                   RedeemTypeSubscription,
		SubscriptionPlanID:     &planID,
		PlanNameSnapshot:       "Professional",
		DailyLimitUSDSnapshot:  &dailyLimit,
		RateMultiplierSnapshot: 1.25,
		ValidityDays:           30,
	}

	subscription, err := subscriptionFromRedeemCode(code, 42, "redeem code ABC", now)

	require.NoError(t, err)
	require.Equal(t, int64(42), subscription.UserID)
	require.Equal(t, int64(15), *subscription.SubscriptionPlanID)
	require.Equal(t, "Professional", subscription.PlanNameSnapshot)
	require.Equal(t, 18.0, *subscription.DailyLimitUSDSnapshot)
	require.Equal(t, 1.25, subscription.RateMultiplierSnapshot)
	require.Equal(t, now, subscription.StartsAt)
	require.Equal(t, now.AddDate(0, 0, 30), subscription.ExpiresAt)
}
