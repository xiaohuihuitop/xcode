package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
)

func gatewayPlatformAssetContextFromDecision(
	decision *productcore.Decision,
	subscription *UserSubscription,
) *GatewayPlatformAssetContext {
	if decision == nil {
		return nil
	}
	platform := decision.Platform
	return &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID:           platform.ID,
			PlatformCode:         platform.Code,
			AccountPlatform:      platform.AccountPlatform,
			RequestedModel:       platform.RequestedModel,
			UpstreamModel:        platform.UpstreamModel,
			EndpointCapabilities: append([]string(nil), platform.EndpointCapabilities...),
		},
		BillingAsset: resolvedBillingAssetFromProduct(decision.BillingAsset, subscription),
		SchedulingScope: PlatformSchedulingScope{
			PlatformID:      platform.ID,
			PlatformCode:    platform.Code,
			AccountPlatform: platform.AccountPlatform,
		},
	}
}

func resolvedBillingAssetFromProduct(
	asset *productcore.BillingAsset,
	subscription *UserSubscription,
) *ResolvedBillingAsset {
	if asset == nil {
		return nil
	}
	return &ResolvedBillingAsset{
		Source:         asset.Source,
		SubscriptionID: clonePlatformInt64Pointer(asset.SubscriptionID),
		PlanID:         clonePlatformInt64Pointer(asset.PlanID),
		RateMultiplier: asset.RateMultiplier,
		Subscription:   cloneUserSubscription(subscription),
	}
}

func cloneGatewayPlatformAssetContext(route *GatewayPlatformAssetContext) *GatewayPlatformAssetContext {
	if route == nil {
		return nil
	}
	return &GatewayPlatformAssetContext{
		Platform:        cloneResolvedPlatformModel(route.Platform),
		BillingAsset:    cloneResolvedBillingAsset(route.BillingAsset),
		SchedulingScope: route.SchedulingScope,
	}
}

func attachGatewayPlatformAssetRoute(ctx context.Context, route *GatewayPlatformAssetContext) context.Context {
	if ctx == nil || route == nil || route.Platform == nil {
		return ctx
	}
	scope, ok := normalizePlatformSchedulingScope(route.SchedulingScope)
	if !ok {
		return ctx
	}
	cloned := cloneGatewayPlatformAssetContext(route)
	ctx = context.WithValue(ctx, ctxkey.GatewayPlatformAsset, cloned)
	ctx = WithPlatformSchedulingScope(ctx, scope)
	ctx = WithResolvedTargetPlatform(ctx, scope.AccountPlatform)
	if model := strings.TrimSpace(cloned.Platform.UpstreamModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, model)
	}
	if model := strings.TrimSpace(cloned.Platform.RequestedModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
	}
	return ctx
}

func attachProductDecision(
	ctx context.Context,
	decision *productcore.Decision,
	subscription *UserSubscription,
) context.Context {
	return attachGatewayPlatformAssetRoute(ctx, gatewayPlatformAssetContextFromDecision(decision, subscription))
}

func AttachPlatformAssetResolution(ctx context.Context, resolution *PlatformAssetResolution) context.Context {
	if resolution == nil {
		return ctx
	}
	return attachProductDecision(ctx, resolution.Decision, resolution.Subscription)
}

// WithGatewayPlatformAssetContext installs the immutable platform and billing
// route used by ProductCore and the Sub2API runtime adapter.
func WithGatewayPlatformAssetContext(ctx context.Context, route *GatewayPlatformAssetContext) context.Context {
	return attachGatewayPlatformAssetRoute(ctx, route)
}

// GatewayPlatformAssetContextFromContext returns an isolated copy.
func GatewayPlatformAssetContextFromContext(ctx context.Context) (*GatewayPlatformAssetContext, bool) {
	if ctx == nil {
		return nil, false
	}
	if route, ok := ctx.Value(ctxkey.GatewayPlatformAsset).(*GatewayPlatformAssetContext); ok && route != nil && route.Platform != nil {
		return cloneGatewayPlatformAssetContext(route), true
	}
	return nil, false
}
