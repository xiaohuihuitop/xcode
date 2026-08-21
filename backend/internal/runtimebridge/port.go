package runtimebridge

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

var (
	ErrLocalRuntimeUnavailable     = errors.New("local runtime bridge is unavailable")
	ErrLocalRuntimeTerminalMissing = errors.New("local runtime bridge terminal event is missing")
)

// Driver is the implementation-facing RuntimeBridge contract. A Driver owns
// upstream execution and emits transport-neutral events; it never receives a
// ProductCore billing asset.
type Driver interface {
	Dispatch(context.Context, v1.Request, EventSink) (v1.Result, error)
}

// EventSink receives events from a Driver. The local bridge converts only the
// terminal event into the existing ProductCore UsageSink.
type EventSink interface {
	Publish(context.Context, v1.Event) error
}

// LocalRuntime adapts a v1 Driver to the current in-process GatewayRuntime
// interface. This keeps the production deployment unchanged while the
// Driver implementation is moved out of HTTP handlers.
type LocalRuntime struct {
	driver Driver
}

func NewLocalRuntime(driver Driver) *LocalRuntime {
	return &LocalRuntime{driver: driver}
}

// RequestFromDecision copies only the immutable platform route into the public
// runtime contract. BillingAsset is intentionally not represented in v1.
func RequestFromDecision(request gatewayruntime.Request, decision productcore.Decision) v1.Request {
	return v1.Request{
		ContractVersion: v1.CurrentContractVersion,
		RequestID:       request.RequestID,
		Platform: v1.PlatformRoute{
			ID:                   decision.Platform.ID,
			Code:                 decision.Platform.Code,
			RuntimeAdapter:       decision.Platform.AccountPlatform,
			RequestedModel:       decision.Platform.RequestedModel,
			UpstreamModel:        decision.Platform.UpstreamModel,
			EndpointCapabilities: append([]string(nil), decision.Platform.EndpointCapabilities...),
		},
		Endpoint:        v1.Endpoint(request.Endpoint),
		InboundEndpoint: request.InboundEndpoint,
		Stream:          request.Stream,
		Payload:         append([]byte(nil), request.Payload...),
		Headers:         cloneHeaders(request.Metadata.Headers),
		Owner: v1.OwnerRef{
			UserID:   request.Metadata.UserID,
			APIKeyID: request.Metadata.APIKeyID,
		},
		Session: v1.SessionMetadata{
			SessionID:          request.Metadata.SessionID,
			UserAgent:          request.Metadata.UserAgent,
			ClientIP:           request.Metadata.ClientIP,
			RequestPayloadHash: request.Metadata.RequestPayloadHash,
		},
	}
}

func requestFromRuntime(request gatewayruntime.Request) v1.Request {
	return RequestFromDecision(request, productcore.Decision{
		Platform: productcore.Platform{
			ID:                   request.PlatformID,
			Code:                 request.PlatformCode,
			AccountPlatform:      request.Adapter,
			RequestedModel:       request.RequestedModel,
			UpstreamModel:        request.UpstreamModel,
			EndpointCapabilities: []string{string(request.Endpoint)},
		},
	})
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

var _ gatewayruntime.GatewayRuntime = (*LocalRuntime)(nil)
