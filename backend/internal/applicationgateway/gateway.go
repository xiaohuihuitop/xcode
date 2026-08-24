package applicationgateway

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
)

var (
	ErrGatewayUnavailable     = errors.New("application gateway dependencies are unavailable")
	ErrDecisionUnavailable    = errors.New("application gateway decision is unavailable")
	ErrGatewayTerminalMissing = errors.New("application gateway terminal usage is missing")
)

type DecisionProvider interface {
	Resolve(context.Context, productcore.AccessGrant, productcore.Request) (*productcore.Decision, error)
}

type DecisionSnapshot struct {
	Decision productcore.Decision
	Grant    productcore.AccessGrant
}

type UsageSinkFactory interface {
	ForDecision(DecisionSnapshot) gatewayruntime.UsageSink
}

type DispatchRequest struct {
	Grant   productcore.AccessGrant
	Product productcore.Request
	Runtime gatewayruntime.Request
}

type Gateway struct {
	decisions DecisionProvider
	runtime   gatewayruntime.GatewayRuntime
	usage     UsageSinkFactory
}

func New(decisions DecisionProvider, runtime gatewayruntime.GatewayRuntime, usage UsageSinkFactory) *Gateway {
	return &Gateway{decisions: decisions, runtime: runtime, usage: usage}
}

func (g *Gateway) Dispatch(ctx context.Context, request DispatchRequest) (gatewayruntime.Result, error) {
	if g == nil || g.decisions == nil || g.runtime == nil || g.usage == nil {
		return gatewayruntime.Result{}, ErrGatewayUnavailable
	}
	decision, err := g.decisions.Resolve(ctx, request.Grant, request.Product)
	if err != nil {
		return gatewayruntime.Result{}, err
	}
	if decision == nil {
		return gatewayruntime.Result{}, ErrDecisionUnavailable
	}
	snapshot := DecisionSnapshot{
		Decision: cloneDecision(decision),
		Grant:    cloneGrant(request.Grant),
	}
	sink := g.usage.ForDecision(snapshot)
	if sink == nil {
		return gatewayruntime.Result{}, gatewayruntime.ErrUsageSinkUnavailable
	}
	if isNonBillingEndpoint(request.Runtime.Endpoint) {
		sink = noBillingUsageSink{}
	}
	runtimeRequest := runtimeRequestFromDecision(request.Runtime, snapshot.Decision)
	recorder := gatewayruntime.NewTerminalRecorder(sink)
	result, err := g.runtime.Dispatch(ctx, runtimeRequest, recorder)
	if !recorder.Recorded() {
		if err != nil {
			return result, errors.Join(err, ErrGatewayTerminalMissing)
		}
		return result, ErrGatewayTerminalMissing
	}
	return result, err
}

func isNonBillingEndpoint(endpoint gatewayruntime.Endpoint) bool {
	switch endpoint {
	case gatewayruntime.EndpointCountTokens,
		gatewayruntime.EndpointResponsesInputTokens,
		gatewayruntime.EndpointLive,
		gatewayruntime.EndpointWebSocket:
		return true
	default:
		return false
	}
}

type noBillingUsageSink struct{}

func (noBillingUsageSink) RecordFinal(context.Context, gatewayruntime.UsageEvent) error { return nil }

func runtimeRequestFromDecision(request gatewayruntime.Request, decision productcore.Decision) gatewayruntime.Request {
	request.PlatformID = decision.Platform.ID
	request.PlatformCode = decision.Platform.Code
	request.Adapter = decision.Platform.AccountPlatform
	request.RequestedModel = decision.Platform.RequestedModel
	request.UpstreamModel = decision.Platform.UpstreamModel
	if request.Endpoint == "" {
		request.Endpoint = endpointFromCapability(decision.Platform.EndpointCapabilities)
	}
	request.Payload = append([]byte(nil), request.Payload...)
	request.Metadata.Headers = cloneHeaders(request.Metadata.Headers)
	return request
}

func endpointFromCapability(capabilities []string) gatewayruntime.Endpoint {
	for _, capability := range capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case "responses":
			return gatewayruntime.EndpointResponses
		case "chat_completions", "chat-completions":
			return gatewayruntime.EndpointChatCompletions
		case "messages":
			return gatewayruntime.EndpointMessages
		}
	}
	return ""
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func cloneGrant(grant productcore.AccessGrant) productcore.AccessGrant {
	grant.PlatformIDs = append([]int64(nil), grant.PlatformIDs...)
	grant.SubscriptionPlanIDs = append([]int64(nil), grant.SubscriptionPlanIDs...)
	return grant
}

func cloneDecision(decision *productcore.Decision) productcore.Decision {
	cloned := productcore.Decision{}
	if decision == nil {
		return cloned
	}
	cloned.Platform = decision.Platform
	cloned.Platform.EndpointCapabilities = append([]string(nil), decision.Platform.EndpointCapabilities...)
	if decision.BillingAsset != nil {
		asset := *decision.BillingAsset
		if decision.BillingAsset.SubscriptionID != nil {
			value := *decision.BillingAsset.SubscriptionID
			asset.SubscriptionID = &value
		}
		if decision.BillingAsset.PlanID != nil {
			value := *decision.BillingAsset.PlanID
			asset.PlanID = &value
		}
		cloned.BillingAsset = &asset
	}
	return cloned
}
