package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/productcore"
)

type PlatformAssetResolution struct {
	Decision     *productcore.Decision
	Subscription *UserSubscription
}

type PlatformAssetProductCoreAdapter struct {
	apiKeyService       *APIKeyService
	subscriptionService apiKeySubscriptionResolver
	platformResolver    PlatformModelResolver
}

func NewPlatformAssetProductCoreAdapter(
	apiKeyService *APIKeyService,
	subscriptionService apiKeySubscriptionResolver,
	platformResolver PlatformModelResolver,
) *PlatformAssetProductCoreAdapter {
	return &PlatformAssetProductCoreAdapter{
		apiKeyService:       apiKeyService,
		subscriptionService: subscriptionService,
		platformResolver:    platformResolver,
	}
}

type platformCatalogAdapter struct {
	resolver PlatformModelResolver
}

func (a platformCatalogAdapter) ListModelCandidates(ctx context.Context, model string) ([]*productcore.Platform, error) {
	resolved, err := a.resolver.ResolveModelCandidates(ctx, model)
	if err != nil {
		if errors.Is(err, ErrPlatformModelNotFound) {
			return nil, productcore.ErrModelUnavailable
		}
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, productcore.ErrModelUnavailable
	}
	platforms := make([]*productcore.Platform, 0, len(resolved))
	for _, candidate := range resolved {
		if candidate == nil {
			continue
		}
		platforms = append(platforms, productPlatformFromResolved(candidate))
	}
	if len(platforms) == 0 {
		return nil, productcore.ErrModelUnavailable
	}
	return platforms, nil
}

type requestAssetSelector struct {
	service       *APIKeyService
	apiKey        *APIKey
	subscriptions apiKeySubscriptionResolver
	subscription  *UserSubscription
}

func (s *requestAssetSelector) Select(
	ctx context.Context,
	_ productcore.AccessGrant,
	skipBilling bool,
) (*productcore.BillingAsset, error) {
	asset, err := s.service.ResolveBillingAssetForRequest(ctx, s.apiKey, s.subscriptions, skipBilling)
	if err != nil {
		return nil, mapServiceAssetError(err)
	}
	if asset == nil {
		return nil, nil
	}
	s.subscription = cloneUserSubscription(asset.Subscription)
	return &productcore.BillingAsset{
		Source:         asset.Source,
		SubscriptionID: clonePlatformInt64Pointer(asset.SubscriptionID),
		PlanID:         clonePlatformInt64Pointer(asset.PlanID),
		RateMultiplier: asset.RateMultiplier,
	}, nil
}

func (a *PlatformAssetProductCoreAdapter) Resolve(
	ctx context.Context,
	apiKey *APIKey,
	model string,
	endpoint string,
	skipBilling bool,
) (*PlatformAssetResolution, error) {
	if a == nil || a.apiKeyService == nil || a.platformResolver == nil {
		return nil, fmt.Errorf("%w: product core adapter dependencies are required", ErrPlatformInvalid)
	}
	if apiKey == nil || !UsesPlatformAssetPermissions(apiKey) {
		return nil, ErrAPIKeyPlatformForbidden
	}

	selector := &requestAssetSelector{
		service:       a.apiKeyService,
		apiKey:        apiKey,
		subscriptions: a.subscriptionService,
	}
	decision, err := productcore.NewAuthorizer(
		platformCatalogAdapter{resolver: a.platformResolver}, selector,
	).Resolve(ctx, productCoreAccessGrant(apiKey), productCoreRequest(model, endpoint, skipBilling))
	if err != nil {
		return nil, mapProductCoreError(err)
	}
	if err := validateProductCorePlatform(decision); err != nil {
		return nil, err
	}
	return &PlatformAssetResolution{Decision: decision, Subscription: selector.subscription}, nil
}

func productCoreAccessGrant(apiKey *APIKey) productcore.AccessGrant {
	grant := productcore.AccessGrant{
		KeyID:               apiKey.ID,
		UserID:              apiKey.UserID,
		PlatformIDs:         append([]int64(nil), apiKey.AllowedPlatformIDs...),
		SubscriptionPlanIDs: append([]int64(nil), apiKey.AllowedSubscriptionPlanIDs...),
		AllowBalance:        apiKey.AllowBalance,
	}
	if apiKey.User != nil {
		grant.Balance = apiKey.User.Balance
	}
	return grant
}

func productCoreRequest(model, endpoint string, skipBilling bool) productcore.Request {
	return productcore.Request{
		Model:              model,
		EndpointCapability: string(billingEndpointCapability(endpoint)),
		SkipBilling:        skipBilling,
	}
}

func billingEndpointCapability(endpoint string) OpenAIEndpointCapability {
	normalized := strings.ToLower(endpoint)
	switch {
	case strings.Contains(normalized, "/chat/completions"):
		return OpenAIEndpointCapabilityChatCompletions
	case strings.Contains(normalized, "/responses"):
		return OpenAIEndpointCapabilityResponses
	default:
		return ""
	}
}

func validateProductCorePlatform(decision *productcore.Decision) error {
	if decision == nil {
		return fmt.Errorf("%w: product core returned no decision", ErrPlatformInvalid)
	}
	scope := PlatformSchedulingScope{
		PlatformID:      decision.Platform.ID,
		PlatformCode:    decision.Platform.Code,
		AccountPlatform: decision.Platform.AccountPlatform,
	}
	if _, ok := normalizePlatformSchedulingScope(scope); !ok {
		return fmt.Errorf("%w: resolved platform has no account adapter", ErrPlatformInvalid)
	}
	return nil
}

func productPlatformFromResolved(resolved *ResolvedPlatformModel) *productcore.Platform {
	if resolved == nil {
		return nil
	}
	return &productcore.Platform{
		ID:                   resolved.PlatformID,
		Code:                 resolved.PlatformCode,
		AccountPlatform:      resolved.AccountPlatform,
		RequestedModel:       resolved.RequestedModel,
		UpstreamModel:        resolved.UpstreamModel,
		EndpointCapabilities: append([]string(nil), resolved.EndpointCapabilities...),
		MatchPriority:        resolved.MatchPriority,
	}
}

func mapServiceAssetError(err error) error {
	switch {
	case errors.Is(err, ErrNoUsableBillingSource):
		return productcore.ErrNoBillingAsset
	case errors.Is(err, ErrInsufficientBalance):
		return productcore.ErrInsufficientBalance
	case errors.Is(err, ErrDailyLimitExceeded):
		return productcore.ErrDailyLimitExceeded
	case errors.Is(err, ErrWeeklyLimitExceeded):
		return productcore.ErrWeeklyLimitExceeded
	case errors.Is(err, ErrMonthlyLimitExceeded):
		return productcore.ErrMonthlyLimitExceeded
	default:
		return err
	}
}

func mapProductCoreError(err error) error {
	switch {
	case errors.Is(err, productcore.ErrModelUnavailable):
		return ErrPlatformModelNotFound
	case errors.Is(err, productcore.ErrPlatformForbidden):
		return ErrAPIKeyPlatformForbidden
	case errors.Is(err, productcore.ErrPlatformAmbiguous):
		return ErrPlatformModelAmbiguous
	case errors.Is(err, productcore.ErrEndpointUnsupported):
		return ErrPlatformEndpointUnsupported
	case errors.Is(err, productcore.ErrNoBillingAsset):
		return ErrNoUsableBillingSource
	case errors.Is(err, productcore.ErrInsufficientBalance):
		return ErrInsufficientBalance
	case errors.Is(err, productcore.ErrDailyLimitExceeded):
		return ErrDailyLimitExceeded
	case errors.Is(err, productcore.ErrWeeklyLimitExceeded):
		return ErrWeeklyLimitExceeded
	case errors.Is(err, productcore.ErrMonthlyLimitExceeded):
		return ErrMonthlyLimitExceeded
	default:
		return err
	}
}
