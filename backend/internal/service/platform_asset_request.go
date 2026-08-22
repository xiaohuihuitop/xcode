package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAPIKeyPlatformForbidden     = infraerrors.Forbidden("API_KEY_PLATFORM_FORBIDDEN", "api key is not authorized for the resolved platform")
	ErrPlatformEndpointUnsupported = infraerrors.Forbidden("PLATFORM_ENDPOINT_UNSUPPORTED", "the resolved platform does not support this endpoint")
)

// GatewayPlatformAssetContext carries the explicit route for one request.
type GatewayPlatformAssetContext struct {
	Platform        *ResolvedPlatformModel
	BillingAsset    *ResolvedBillingAsset
	SchedulingScope PlatformSchedulingScope
}

// UsesPlatformAssetPermissions reports whether an API Key has at least one
// explicit platform grant. New model requests require this grant; the result
// is not a signal to fall back to legacy group routing.
func UsesPlatformAssetPermissions(apiKey *APIKey) bool {
	return apiKey != nil && len(apiKey.AllowedPlatformIDs) > 0
}

// ResolvePlatformAssetRequest resolves a V2 request in the required order:
// model -> authorized platform -> endpoint capability -> billing asset.
func (s *APIKeyService) ResolvePlatformAssetRequest(
	ctx context.Context,
	apiKey *APIKey,
	resolver PlatformModelResolver,
	subscriptions apiKeySubscriptionResolver,
	requestedModel, endpoint string,
	skipBilling bool,
) (*GatewayPlatformAssetContext, error) {
	if apiKey == nil || !UsesPlatformAssetPermissions(apiKey) {
		return nil, ErrAPIKeyPlatformForbidden
	}
	resolution, err := NewPlatformAssetProductCoreAdapter(s, subscriptions, resolver).
		Resolve(ctx, apiKey, requestedModel, endpoint, skipBilling)
	if err != nil {
		return nil, err
	}
	return gatewayPlatformAssetContextFromDecision(resolution.Decision, resolution.Subscription), nil
}

func cloneResolvedPlatformModel(value *ResolvedPlatformModel) *ResolvedPlatformModel {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.EndpointCapabilities = append([]string(nil), value.EndpointCapabilities...)
	return &cloned
}

func cloneResolvedBillingAsset(value *ResolvedBillingAsset) *ResolvedBillingAsset {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.SubscriptionID != nil {
		subscriptionID := *value.SubscriptionID
		cloned.SubscriptionID = &subscriptionID
	}
	if value.PlanID != nil {
		planID := *value.PlanID
		cloned.PlanID = &planID
	}
	cloned.Subscription = cloneUserSubscription(value.Subscription)
	return &cloned
}

func effectivePricingAdapter(ctx context.Context, apiKey *APIKey) string {
	if route, ok := GatewayPlatformAssetContextFromContext(ctx); ok && route.Platform != nil {
		if adapter := strings.TrimSpace(route.Platform.AccountPlatform); adapter != "" {
			return adapter
		}
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		return strings.TrimSpace(platform)
	}
	return ""
}

func pricingInputForRequest(ctx context.Context, apiKey *APIKey, model string) PricingInput {
	input := PricingInput{
		Model:   model,
		Adapter: effectivePricingAdapter(ctx, apiKey),
	}
	if route, ok := GatewayPlatformAssetContextFromContext(ctx); ok && route.Platform != nil {
		input.PlatformCode = strings.TrimSpace(route.Platform.PlatformCode)
		input.PublicModel = strings.TrimSpace(route.Platform.RequestedModel)
	}
	return input
}

func overridePlatformAssetBillingMultipliers(ctx context.Context, token, image, video float64) (float64, float64, float64) {
	route, ok := GatewayPlatformAssetContextFromContext(ctx)
	if !ok || route.BillingAsset == nil {
		return token, image, video
	}
	multiplier := nonNegativeMultiplier(route.BillingAsset.RateMultiplier)
	return multiplier, multiplier, multiplier
}

func applyPlatformAssetUsageAttribution(ctx context.Context, usageLog *UsageLog) {
	if usageLog == nil {
		return
	}
	route, ok := GatewayPlatformAssetContextFromContext(ctx)
	if !ok || route.Platform == nil {
		return
	}
	platformID := route.Platform.PlatformID
	usageLog.PlatformID = &platformID
	if route.BillingAsset == nil || route.BillingAsset.Source == "" {
		return
	}
	source := route.BillingAsset.Source
	usageLog.BillingSourceType = &source
}
