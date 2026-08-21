package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

// sub2APILegacyDriver is a migration-only wrapper around the existing
// handler-owned adapter. It is the only place where the old executor surface
// is translated into the public RuntimeBridge contract.
type sub2APILegacyDriver struct {
	adapter *Sub2APIRuntimeAdapter
}

func newSub2APILegacyDriver(adapter *Sub2APIRuntimeAdapter) *sub2APILegacyDriver {
	return &sub2APILegacyDriver{adapter: adapter}
}

func (d *sub2APILegacyDriver) Dispatch(
	ctx context.Context,
	request v1.Request,
	sink runtimebridge.EventSink,
) (v1.Result, error) {
	if d == nil || d.adapter == nil {
		return v1.Result{}, ErrSub2APIRuntimeUnavailable
	}
	if sink == nil {
		return v1.Result{}, runtimebridge.ErrLocalRuntimeUnavailable
	}
	exchange, ok := runtimebridge.LocalExchangeFromContext(ctx)
	if !ok {
		return v1.Result{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	legacySink := &legacyRuntimeUsageSink{sink: sink, request: request}
	result, err := d.adapter.Dispatch(ctx, gatewayRequestFromContract(request, exchange), legacySink)
	return contractResultFromGatewayResult(request, result), err
}

func gatewayRequestFromContract(request v1.Request, exchange gatewayruntime.HTTPExchange) gatewayruntime.Request {
	return gatewayruntime.Request{
		RequestID:       request.RequestID,
		PlatformID:      request.Platform.ID,
		PlatformCode:    request.Platform.Code,
		Adapter:         request.Platform.RuntimeAdapter,
		Endpoint:        gatewayruntime.Endpoint(request.Endpoint),
		InboundEndpoint: request.InboundEndpoint,
		RequestedModel:  request.Platform.RequestedModel,
		UpstreamModel:   request.Platform.UpstreamModel,
		Stream:          request.Stream,
		Payload:         append([]byte(nil), request.Payload...),
		Exchange:        exchange,
		Metadata: gatewayruntime.RequestMetadata{
			APIKeyID:           request.Owner.APIKeyID,
			UserID:             request.Owner.UserID,
			Headers:            cloneStringHeaders(request.Headers),
			UserAgent:          request.Session.UserAgent,
			ClientIP:           request.Session.ClientIP,
			SessionID:          request.Session.SessionID,
			RequestPayloadHash: request.Session.RequestPayloadHash,
		},
	}
}

func contractResultFromGatewayResult(request v1.Request, result gatewayruntime.Result) v1.Result {
	headers := make(map[string][]string, len(result.Response.Header))
	for key, values := range result.Response.Header {
		headers[key] = append([]string(nil), values...)
	}
	usage := usageFactsFromGateway(request, result.Usage)
	return v1.Result{
		StatusCode:       result.StatusCode,
		ResponseHeaders:  headers,
		Body:             append([]byte(nil), result.Response.Body...),
		Streamed:         result.Response.Streamed,
		AccountID:        result.AccountID,
		UpstreamEndpoint: result.UpstreamEndpoint,
		UpstreamModel:    result.UpstreamModel,
		Usage:            usage,
	}
}

type legacyRuntimeUsageSink struct {
	sink    runtimebridge.EventSink
	request v1.Request
}

func (s *legacyRuntimeUsageSink) RecordFinal(ctx context.Context, event gatewayruntime.UsageEvent) error {
	if s == nil || s.sink == nil {
		return runtimebridge.ErrLocalRuntimeUnavailable
	}
	converted := contractEventFromGatewayUsage(event)
	if converted.Usage != nil {
		if converted.Usage.PlatformID == 0 {
			converted.Usage.PlatformID = s.request.Platform.ID
		}
		if converted.Usage.Endpoint == "" {
			converted.Usage.Endpoint = string(s.request.Endpoint)
		}
		if converted.Usage.RequestedModel == "" {
			converted.Usage.RequestedModel = s.request.Platform.RequestedModel
		}
		if converted.Usage.UpstreamModel == "" {
			converted.Usage.UpstreamModel = s.request.Platform.UpstreamModel
		}
	}
	return s.sink.Publish(ctx, converted)
}

func contractEventFromGatewayUsage(event gatewayruntime.UsageEvent) v1.Event {
	kind := v1.EventRuntimeFailed
	if event.Success {
		kind = v1.EventUsageFinal
	}
	converted := v1.Event{
		Sequence: 1,
		Kind:     kind,
		Usage:    pointerToUsageFacts(event.Facts),
	}
	if event.Error != nil {
		runtimeErr := &v1.RuntimeError{
			Category:       string(event.Error.Category),
			Message:        event.Error.Error(),
			Retryable:      event.Error.Retryable,
			UpstreamStatus: 0,
		}
		if event.Error.AccountID > 0 {
			runtimeErr.AttemptedAccountIDs = []int64{event.Error.AccountID}
		}
		converted.Error = runtimeErr
	}
	converted.Usage.AccountID = event.Facts.AccountID
	return converted
}

func pointerToUsageFacts(value gatewayruntime.UsageFacts) *v1.UsageFacts {
	converted := usageFactsFromGateway(v1.Request{}, value)
	return &converted
}

func usageFactsFromGateway(request v1.Request, facts gatewayruntime.UsageFacts) v1.UsageFacts {
	endpoint := string(request.Endpoint)
	if endpoint == "" {
		endpoint = facts.InboundEndpoint
	}
	return v1.UsageFacts{
		Adapter:                  firstNonEmptyHandler(facts.Adapter, request.Platform.RuntimeAdapter),
		AccountID:                facts.AccountID,
		Endpoint:                 endpoint,
		RequestedModel:           firstNonEmptyHandler(facts.RequestedModel, request.Platform.RequestedModel),
		UpstreamModel:            firstNonEmptyHandler(facts.UpstreamModel, request.Platform.UpstreamModel),
		UpstreamEndpoint:         facts.UpstreamEndpoint,
		Model:                    facts.Model,
		ServiceTier:              facts.ServiceTier,
		ReasoningEffort:          facts.ReasoningEffort,
		BillingModel:             facts.BillingModel,
		OriginalModel:            facts.OriginalModel,
		MappedModel:              facts.MappedModel,
		BillingModelSource:       facts.BillingModelSource,
		ModelMappingChain:        facts.ModelMappingChain,
		InputTokens:              facts.InputTokens,
		OutputTokens:             facts.OutputTokens,
		CacheCreationTokens:      facts.CacheCreationTokens,
		CacheReadTokens:          facts.CacheReadTokens,
		ImageInputTokens:         facts.ImageInputTokens,
		ImageOutputTokens:        facts.ImageOutputTokens,
		ImageCount:               facts.ImageCount,
		VideoCount:               facts.VideoCount,
		ForceCacheBilling:        facts.ForceCacheBilling,
		CyberBlocked:             facts.CyberBlocked,
		LongContextThreshold:     facts.LongContextThreshold,
		LongContextMultiplier:    facts.LongContextMultiplier,
		FirstTokenMilliseconds:   facts.FirstTokenMilliseconds,
		DurationMilliseconds:     facts.DurationMilliseconds,
		TerminalStatus:           facts.TerminalStatus,
		RequestWasClientStream:   facts.RequestWasClientStream || request.Stream,
		InboundEndpoint:          facts.InboundEndpoint,
		UserAgent:                facts.UserAgent,
		ClientIP:                 facts.ClientIP,
		SessionID:                facts.SessionID,
		RequestPayloadHash:       facts.RequestPayloadHash,
		ResponseWasPartiallySent: facts.ResponseWasPartiallySent,
	}
}

func cloneStringHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func firstNonEmptyHandler(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ runtimebridge.Driver = (*sub2APILegacyDriver)(nil)
var _ gatewayruntime.UsageSink = (*legacyRuntimeUsageSink)(nil)
