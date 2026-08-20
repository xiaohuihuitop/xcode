package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// sub2APIAuxiliaryExecutor keeps the remaining Sub2API protocol handlers
// behind the same runtime boundary. Billing-capable handlers publish their
// existing usage facts through the injected sink; capability-only endpoints
// receive the ApplicationGateway non-billing sink.
type sub2APIAuxiliaryExecutor struct {
	gatewayHandler *GatewayHandler
	openAIHandler  *OpenAIGatewayHandler
	endpoint       gatewayruntime.Endpoint
}

func (e sub2APIAuxiliaryExecutor) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	route, _ := service.GatewayPlatformAssetContextFromContext(ctx)
	if route == nil || route.Platform == nil {
		if compatibilityRoute := runtimeCompatibilityRoute(request); compatibilityRoute != nil {
			ctx = service.WithGatewayPlatformAssetContext(ctx, compatibilityRoute)
			route = compatibilityRoute
		}
	}
	if route == nil || route.Platform == nil {
		return gatewayruntime.Result{}, service.ErrAPIKeyPlatformForbidden
	}
	handler := e.handlerFor(request, route.Platform.AccountPlatform)
	if handler == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	return (legacyEndpointExecutor{handler: handler}).Execute(ctx, request, sink)
}

func (e sub2APIAuxiliaryExecutor) handlerFor(request gatewayruntime.Request, adapter string) legacyGinHandler {
	switch e.endpoint {
	case gatewayruntime.EndpointLive:
		if e.openAIHandler != nil {
			return e.openAIHandler.legacyLive
		}
	}
	return nil
}

var _ Sub2APIEndpointExecutor = (*sub2APIAuxiliaryExecutor)(nil)
