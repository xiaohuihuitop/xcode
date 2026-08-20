package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type contextDecisionProvider struct{}

func (contextDecisionProvider) Resolve(ctx context.Context, _ productcore.AccessGrant, _ productcore.Request) (*productcore.Decision, error) {
	route, ok := service.GatewayPlatformAssetContextFromContext(ctx)
	if !ok || route == nil || route.Platform == nil {
		return nil, service.ErrAPIKeyPlatformForbidden
	}
	platform := route.Platform
	decision := &productcore.Decision{
		Platform: productcore.Platform{
			ID:                   platform.PlatformID,
			Code:                 platform.PlatformCode,
			AccountPlatform:      platform.AccountPlatform,
			RequestedModel:       platform.RequestedModel,
			UpstreamModel:        platform.UpstreamModel,
			EndpointCapabilities: append([]string(nil), platform.EndpointCapabilities...),
			MatchPriority:        platform.MatchPriority,
		},
	}
	if route.BillingAsset != nil {
		asset := route.BillingAsset
		decision.BillingAsset = &productcore.BillingAsset{
			Source:         asset.Source,
			SubscriptionID: cloneInt64Pointer(asset.SubscriptionID),
			PlanID:         cloneInt64Pointer(asset.PlanID),
			RateMultiplier: asset.RateMultiplier,
		}
	}
	return decision, nil
}

type noOpRuntimeUsageSink struct{}

func (noOpRuntimeUsageSink) RecordFinal(context.Context, gatewayruntime.UsageEvent) error { return nil }

type noOpRuntimeUsageSinkFactory struct{}

func (noOpRuntimeUsageSinkFactory) ForDecision(applicationgateway.DecisionSnapshot) gatewayruntime.UsageSink {
	return noOpRuntimeUsageSink{}
}

// NewSub2APIProductionApplicationGateway composes the existing protocol
// handlers behind one runtime adapter. The handlers remain protocol-specific;
// the application boundary is shared and is the only production dispatch path.
func NewSub2APIProductionApplicationGateway(gatewayHandler *GatewayHandler, openaiHandler *OpenAIGatewayHandler, apiKeys service.ProductUsageAPIKeyLoader) *applicationgateway.Gateway {
	executors := make(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor, 12)
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointMessages,
		gatewayruntime.EndpointChatCompletions,
		gatewayruntime.EndpointResponses,
	} {
		executors[endpoint] = sub2APIMessagesExecutor{
			gatewayHandler: gatewayHandler,
			openaiHandler:  openaiHandler,
			endpoint:       endpoint,
		}
	}
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointCountTokens,
		gatewayruntime.EndpointEmbeddings,
		gatewayruntime.EndpointAlphaSearch,
	} {
		executors[endpoint] = sub2APISyncExecutor{
			gatewayHandler: gatewayHandler,
			openAIHandler:  openaiHandler,
			endpoint:       endpoint,
		}
	}
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointGeminiNative,
		gatewayruntime.EndpointImages,
		gatewayruntime.EndpointVideos,
	} {
		executors[endpoint] = sub2APIGeminiMediaExecutor{
			gatewayHandler: gatewayHandler,
			openAIHandler:  openaiHandler,
			endpoint:       endpoint,
		}
	}
	executors[gatewayruntime.EndpointLive] = sub2APIAuxiliaryExecutor{
		gatewayHandler: gatewayHandler,
		openAIHandler:  openaiHandler,
		endpoint:       gatewayruntime.EndpointLive,
	}
	adapter := NewSub2APIRuntimeAdapter(executors)
	usageFactory := service.NewSub2APIProductUsageSinkFactory(
		gatewayServiceFromHandler(gatewayHandler),
		openAIGatewayServiceFromHandler(openaiHandler),
		apiKeys,
	)
	return applicationgateway.New(contextDecisionProvider{}, adapter, usageFactory)
}

func gatewayServiceFromHandler(h *GatewayHandler) *service.GatewayService {
	if h == nil {
		return nil
	}
	return h.gatewayService
}

func openAIGatewayServiceFromHandler(h *OpenAIGatewayHandler) *service.OpenAIGatewayService {
	if h == nil {
		return nil
	}
	return h.gatewayService
}

func endpointCapabilityForRuntime(endpoint gatewayruntime.Endpoint) string {
	switch endpoint {
	case gatewayruntime.EndpointChatCompletions:
		return "chat_completions"
	case gatewayruntime.EndpointResponses:
		return "responses"
	case gatewayruntime.EndpointMessages:
		return "messages"
	default:
		return string(endpoint)
	}
}

func requestLikelyStreams(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if strings.EqualFold(c.GetHeader("Accept"), "text/event-stream") {
		return true
	}
	if c.Request.URL != nil {
		if strings.EqualFold(c.Request.URL.Query().Get("alt"), "sse") {
			return true
		}
		return strings.Contains(c.Request.URL.Path, "streamGenerateContent")
	}
	return false
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
