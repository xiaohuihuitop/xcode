package runtimebridge

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type localEventSink struct {
	request   gatewayruntime.Request
	recorder  *gatewayruntime.TerminalRecorder
	collector *v1.TerminalCollector
}

func (s *localEventSink) Publish(ctx context.Context, event v1.Event) error {
	if s == nil || s.recorder == nil || s.collector == nil {
		return ErrLocalRuntimeUnavailable
	}
	if !event.IsTerminal() {
		return nil
	}
	if err := s.collector.RecordTerminal(event); err != nil {
		return err
	}
	usageEvent := usageEventFromV1(s.request, event)
	return s.recorder.RecordFinal(ctx, usageEvent)
}

func (s *localEventSink) recorded() bool {
	return s != nil && s.collector != nil && s.collector.Recorded()
}

func usageEventFromV1(request gatewayruntime.Request, event v1.Event) gatewayruntime.UsageEvent {
	facts := gatewayruntime.UsageFacts{
		Adapter:                  request.Adapter,
		RequestedModel:           request.RequestedModel,
		UpstreamModel:            request.UpstreamModel,
		InboundEndpoint:          request.InboundEndpoint,
		UserAgent:                request.Metadata.UserAgent,
		ClientIP:                 request.Metadata.ClientIP,
		SessionID:                request.Metadata.SessionID,
		RequestPayloadHash:       request.Metadata.RequestPayloadHash,
		RequestWasClientStream:   request.Stream,
		ResponseWasPartiallySent: request.Exchange != nil && request.Exchange.Size() > 0,
	}
	if event.Usage != nil {
		facts = usageFactsFromV1(request, *event.Usage)
	}
	usageEvent := gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   event.Kind == v1.EventUsageFinal,
		Facts:     facts,
	}
	if event.Error != nil {
		usageEvent.Error = gatewayruntime.NewRuntimeError(
			gatewayruntime.ErrorCategory(event.Error.Category),
			event.Error.Retryable,
			event.Error.Message,
		)
	}
	if !usageEvent.Success && usageEvent.Error == nil {
		usageEvent.Error = gatewayruntime.NewRuntimeError(
			gatewayruntime.ErrorInvalidUpstreamResponse,
			false,
			"runtime request did not finish successfully",
		)
	}
	return usageEvent
}

func usageFactsFromV1(request gatewayruntime.Request, usage v1.UsageFacts) gatewayruntime.UsageFacts {
	return gatewayruntime.UsageFacts{
		Adapter:                  firstNonEmpty(request.Adapter, usage.Adapter),
		Model:                    firstNonEmpty(usage.Model, usage.RequestedModel),
		RequestedModel:           firstNonEmpty(usage.RequestedModel, request.RequestedModel),
		UpstreamModel:            firstNonEmpty(usage.UpstreamModel, request.UpstreamModel),
		UpstreamEndpoint:         usage.UpstreamEndpoint,
		ServiceTier:              usage.ServiceTier,
		ReasoningEffort:          usage.ReasoningEffort,
		BillingModel:             usage.BillingModel,
		OriginalModel:            usage.OriginalModel,
		MappedModel:              usage.MappedModel,
		BillingModelSource:       usage.BillingModelSource,
		ModelMappingChain:        usage.ModelMappingChain,
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationTokens:      usage.CacheCreationTokens,
		CacheReadTokens:          usage.CacheReadTokens,
		ImageInputTokens:         usage.ImageInputTokens,
		ImageOutputTokens:        usage.ImageOutputTokens,
		ImageCount:               usage.ImageCount,
		VideoCount:               usage.VideoCount,
		ForceCacheBilling:        usage.ForceCacheBilling,
		CyberBlocked:             usage.CyberBlocked,
		LongContextThreshold:     usage.LongContextThreshold,
		LongContextMultiplier:    usage.LongContextMultiplier,
		AccountID:                usage.AccountID,
		DurationMilliseconds:     usage.DurationMilliseconds,
		FirstTokenMilliseconds:   usage.FirstTokenMilliseconds,
		InboundEndpoint:          firstNonEmpty(usage.InboundEndpoint, request.InboundEndpoint),
		UserAgent:                firstNonEmpty(usage.UserAgent, request.Metadata.UserAgent),
		ClientIP:                 firstNonEmpty(usage.ClientIP, request.Metadata.ClientIP),
		SessionID:                firstNonEmpty(usage.SessionID, request.Metadata.SessionID),
		RequestPayloadHash:       firstNonEmpty(usage.RequestPayloadHash, request.Metadata.RequestPayloadHash),
		TerminalStatus:           usage.TerminalStatus,
		RequestWasClientStream:   usage.RequestWasClientStream || request.Stream,
		ResponseWasPartiallySent: usage.ResponseWasPartiallySent || (request.Exchange != nil && request.Exchange.Size() > 0),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var _ EventSink = (*localEventSink)(nil)
