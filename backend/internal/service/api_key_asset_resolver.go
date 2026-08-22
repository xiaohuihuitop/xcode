package service

import (
	"context"
	"errors"
	"reflect"
	"sort"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	BillingSourceSubscription = "subscription"
	BillingSourceBalance      = "balance"
)

var ErrNoUsableBillingSource = infraerrors.Forbidden("NO_USABLE_BILLING_SOURCE", "no usable subscription or balance source is available")

// ResolvedBillingAsset identifies the asset that will pay for one request.
// Subscription is kept for the gateway's existing usage and limit accounting.
type ResolvedBillingAsset struct {
	Source         string
	SubscriptionID *int64
	PlanID         *int64
	RateMultiplier float64
	Subscription   *UserSubscription
}

type apiKeySubscriptionPlanCandidateLister interface {
	ListActiveSubscriptionsByPlanIDs(ctx context.Context, userID int64, planIDs []int64) ([]UserSubscription, error)
}

type apiKeyAllSubscriptionLister interface {
	ListActiveSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error)
}

type apiKeySubscriptionResolver interface {
	ValidateAndCheckLimits(sub *UserSubscription) (bool, error)
	EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription) (*UserSubscription, error)
}

func hasSubscriptionResolver(resolver apiKeySubscriptionResolver) bool {
	if resolver == nil {
		return false
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func isSubscriptionCandidateUnavailableError(err error) bool {
	return errors.Is(err, ErrDailyLimitExceeded) ||
		errors.Is(err, ErrWeeklyLimitExceeded) ||
		errors.Is(err, ErrMonthlyLimitExceeded) ||
		errors.Is(err, ErrSubscriptionNotFound) ||
		errors.Is(err, ErrSubscriptionInvalid) ||
		errors.Is(err, ErrSubscriptionExpired) ||
		errors.Is(err, ErrSubscriptionSuspended)
}

// ResolveBillingAssetForRequest selects a user asset after platform routing has
// already succeeded. It only inspects the selected platform and billing assets.
func (s *APIKeyService) ResolveBillingAssetForRequest(
	ctx context.Context,
	apiKey *APIKey,
	subscriptions apiKeySubscriptionResolver,
	skipBilling bool,
) (*ResolvedBillingAsset, error) {
	if apiKey == nil {
		return nil, ErrNoUsableBillingSource
	}
	if skipBilling {
		return nil, nil
	}
	asset, err := s.resolveSubscriptionBillingAsset(ctx, apiKey, subscriptions)
	if err != nil || asset != nil {
		return asset, err
	}
	return s.resolveBalanceBillingAsset(ctx, apiKey)
}

func (s *APIKeyService) resolveSubscriptionBillingAsset(
	ctx context.Context,
	apiKey *APIKey,
	subscriptions apiKeySubscriptionResolver,
) (*ResolvedBillingAsset, error) {
	if (!apiKey.AllowAllSubscriptions && len(apiKey.AllowedSubscriptionPlanIDs) == 0) || !hasSubscriptionResolver(subscriptions) {
		return nil, nil
	}
	if apiKey.AllowAllSubscriptions {
		lister, ok := subscriptions.(apiKeyAllSubscriptionLister)
		if !ok {
			return nil, nil
		}
		candidates, err := lister.ListActiveSubscriptions(ctx, apiKey.UserID)
		if err != nil {
			if isSubscriptionCandidateUnavailableError(err) {
				return nil, nil
			}
			return nil, err
		}
		return s.firstUsableSubscriptionAsset(ctx, nil, candidates, subscriptions)
	}
	lister, ok := subscriptions.(apiKeySubscriptionPlanCandidateLister)
	if !ok {
		return nil, nil
	}
	candidates, err := lister.ListActiveSubscriptionsByPlanIDs(ctx, apiKey.UserID, apiKey.AllowedSubscriptionPlanIDs)
	if err != nil {
		if isSubscriptionCandidateUnavailableError(err) {
			return nil, nil
		}
		return nil, err
	}
	return s.firstUsableSubscriptionAsset(ctx, apiKey.AllowedSubscriptionPlanIDs, candidates, subscriptions)
}

func (s *APIKeyService) firstUsableSubscriptionAsset(
	ctx context.Context,
	allowedPlanIDs []int64,
	candidates []UserSubscription,
	subscriptions apiKeySubscriptionResolver,
) (*ResolvedBillingAsset, error) {
	var allowed map[int64]struct{}
	if allowedPlanIDs != nil {
		allowed = makeAllowedPlanIDSet(allowedPlanIDs)
	}
	sortSubscriptionCandidates(candidates)
	for index := range candidates {
		subscription := &candidates[index]
		if !subscriptionUsesAllowedPlan(subscription, allowed) {
			continue
		}
		needsMaintenance, err := subscriptions.ValidateAndCheckLimits(subscription)
		if needsMaintenance {
			subscription, err = subscriptions.EnsureWindowMaintenance(ctx, subscription)
			if err == nil && subscription != nil {
				_, err = subscriptions.ValidateAndCheckLimits(subscription)
			}
		}
		if err != nil || subscription == nil {
			if isSubscriptionCandidateUnavailableError(err) || subscription == nil {
				continue
			}
			return nil, err
		}
		return resolvedSubscriptionBillingAsset(subscription), nil
	}
	return nil, nil
}

func (s *APIKeyService) resolveBalanceBillingAsset(ctx context.Context, apiKey *APIKey) (*ResolvedBillingAsset, error) {
	if !apiKey.AllowBalance {
		return nil, ErrNoUsableBillingSource
	}
	if apiKey.User == nil || apiKey.User.Balance <= 0 {
		return nil, ErrInsufficientBalance
	}
	return &ResolvedBillingAsset{
		Source:         BillingSourceBalance,
		RateMultiplier: globalBalanceRateMultiplier(ctx, s.globalBalanceRateProvider),
	}, nil
}

func makeAllowedPlanIDSet(planIDs []int64) map[int64]struct{} {
	allowed := make(map[int64]struct{}, len(planIDs))
	for _, planID := range planIDs {
		if planID > 0 {
			allowed[planID] = struct{}{}
		}
	}
	return allowed
}

func subscriptionUsesAllowedPlan(subscription *UserSubscription, allowed map[int64]struct{}) bool {
	if subscription == nil || subscription.SubscriptionPlanID == nil {
		return false
	}
	if allowed == nil {
		return true
	}
	_, ok := allowed[*subscription.SubscriptionPlanID]
	return ok
}

func sortSubscriptionCandidates(candidates []UserSubscription) {
	sort.SliceStable(candidates, func(left, right int) bool {
		if !candidates[left].ExpiresAt.Equal(candidates[right].ExpiresAt) {
			return candidates[left].ExpiresAt.Before(candidates[right].ExpiresAt)
		}
		if !candidates[left].CreatedAt.Equal(candidates[right].CreatedAt) {
			return candidates[left].CreatedAt.Before(candidates[right].CreatedAt)
		}
		return candidates[left].ID < candidates[right].ID
	})
}

func resolvedSubscriptionBillingAsset(subscription *UserSubscription) *ResolvedBillingAsset {
	if subscription == nil || subscription.SubscriptionPlanID == nil {
		return nil
	}
	subscriptionID := subscription.ID
	planID := *subscription.SubscriptionPlanID
	return &ResolvedBillingAsset{
		Source:         BillingSourceSubscription,
		SubscriptionID: &subscriptionID,
		PlanID:         &planID,
		RateMultiplier: nonNegativeMultiplier(subscription.RateMultiplierSnapshot),
		Subscription:   cloneUserSubscription(subscription),
	}
}
