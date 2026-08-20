package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func recordGatewayUsage(ctx context.Context, svc *service.GatewayService, input *service.RecordUsageInput) error {
	if sink, ok := gatewayruntime.UsageSinkFromContext(ctx); ok {
		return sink.RecordFinal(ctx, gatewayUsageEvent(input))
	}
	if svc == nil {
		return errors.New("gateway service unavailable")
	}
	return svc.RecordUsage(ctx, input)
}

func recordGatewayLongContextUsage(ctx context.Context, svc *service.GatewayService, input *service.RecordUsageLongContextInput) error {
	if sink, ok := gatewayruntime.UsageSinkFromContext(ctx); ok {
		if input == nil || input.Result == nil {
			return gatewayruntime.ErrUsageSinkUnavailable
		}
		facts := gatewayFacts(&service.RecordUsageInput{
			Result:             input.Result,
			InboundEndpoint:    input.InboundEndpoint,
			UpstreamEndpoint:   input.UpstreamEndpoint,
			UserAgent:          input.UserAgent,
			IPAddress:          input.IPAddress,
			SessionID:          input.SessionID,
			RequestPayloadHash: input.RequestPayloadHash,
			ForceCacheBilling:  input.ForceCacheBilling,
			ModelRoutingUsageFields: service.ModelRoutingUsageFields{
				OriginalModel:      input.OriginalModel,
				MappedModel:        input.MappedModel,
				BillingModelSource: input.BillingModelSource,
				ModelMappingChain:  input.ModelMappingChain,
			},
		})
		facts.LongContextThreshold = input.LongContextThreshold
		facts.LongContextMultiplier = input.LongContextMultiplier
		return sink.RecordFinal(ctx, gatewayruntime.UsageEvent{
			RequestID: input.Result.RequestID,
			Success:   true,
			Facts:     facts,
		})
	}
	if svc == nil {
		return errors.New("gateway service unavailable")
	}
	return svc.RecordUsageWithLongContext(ctx, input)
}

func recordOpenAIUsage(ctx context.Context, svc *service.OpenAIGatewayService, input *service.OpenAIRecordUsageInput) error {
	if sink, ok := gatewayruntime.UsageSinkFromContext(ctx); ok {
		return sink.RecordFinal(ctx, openAIUsageEvent(input))
	}
	if svc == nil {
		return errors.New("openai gateway service unavailable")
	}
	return svc.RecordUsage(ctx, input)
}

func gatewayUsageEvent(input *service.RecordUsageInput) gatewayruntime.UsageEvent {
	facts := gatewayruntime.UsageFacts{}
	if input != nil {
		facts = gatewayFacts(input)
	}
	requestID := ""
	if input != nil && input.Result != nil {
		requestID = input.Result.RequestID
	}
	return gatewayruntime.UsageEvent{RequestID: requestID, Success: input != nil && input.Result != nil, Facts: facts}
}

func openAIUsageEvent(input *service.OpenAIRecordUsageInput) gatewayruntime.UsageEvent {
	facts := gatewayruntime.UsageFacts{}
	if input != nil {
		facts = openAIFacts(input)
	}
	requestID := ""
	if input != nil && input.Result != nil {
		requestID = input.Result.RequestID
	}
	return gatewayruntime.UsageEvent{RequestID: requestID, Success: input != nil && input.Result != nil, Facts: facts}
}

func gatewayFacts(input *service.RecordUsageInput) gatewayruntime.UsageFacts {
	result := input.Result
	facts := gatewayruntime.UsageFacts{
		Model:                  result.Model,
		RequestedModel:         input.OriginalModel,
		InputTokens:            result.Usage.InputTokens,
		OutputTokens:           result.Usage.OutputTokens,
		CacheCreationTokens:    result.Usage.CacheCreationInputTokens,
		CacheReadTokens:        result.Usage.CacheReadInputTokens,
		ImageOutputTokens:      result.Usage.ImageOutputTokens,
		ImageCount:             result.ImageCount,
		FirstTokenMilliseconds: firstTokenMilliseconds(result.FirstTokenMs),
		DurationMilliseconds:   result.Duration.Milliseconds(),
		AccountID:              accountID(input.Account),
		UpstreamEndpoint:       strings.TrimSpace(input.UpstreamEndpoint),
		UpstreamModel:          result.UpstreamModel,
		InboundEndpoint:        input.InboundEndpoint,
		UserAgent:              input.UserAgent,
		ClientIP:               input.IPAddress,
		SessionID:              input.SessionID,
		RequestPayloadHash:     input.RequestPayloadHash,
		OriginalModel:          input.OriginalModel,
		MappedModel:            input.MappedModel,
		BillingModelSource:     input.BillingModelSource,
		ModelMappingChain:      input.ModelMappingChain,
		ForceCacheBilling:      input.ForceCacheBilling,
	}
	if result.ReasoningEffort != nil {
		facts.ReasoningEffort = strings.TrimSpace(*result.ReasoningEffort)
	}
	return facts
}

func openAIFacts(input *service.OpenAIRecordUsageInput) gatewayruntime.UsageFacts {
	result := input.Result
	facts := gatewayruntime.UsageFacts{
		Model:                  result.Model,
		RequestedModel:         input.OriginalModel,
		InputTokens:            result.Usage.InputTokens,
		OutputTokens:           result.Usage.OutputTokens,
		CacheCreationTokens:    result.Usage.CacheCreationInputTokens,
		CacheReadTokens:        result.Usage.CacheReadInputTokens,
		ImageInputTokens:       result.Usage.ImageInputTokens,
		ImageOutputTokens:      result.Usage.ImageOutputTokens,
		ImageCount:             result.ImageCount,
		VideoCount:             result.VideoCount,
		FirstTokenMilliseconds: firstTokenMilliseconds(result.FirstTokenMs),
		DurationMilliseconds:   result.Duration.Milliseconds(),
		AccountID:              accountID(input.Account),
		UpstreamEndpoint:       strings.TrimSpace(input.UpstreamEndpoint),
		UpstreamModel:          result.UpstreamModel,
		InboundEndpoint:        input.InboundEndpoint,
		UserAgent:              input.UserAgent,
		ClientIP:               input.IPAddress,
		SessionID:              input.SessionID,
		RequestPayloadHash:     input.RequestPayloadHash,
		OriginalModel:          input.OriginalModel,
		MappedModel:            input.MappedModel,
		BillingModelSource:     input.BillingModelSource,
		ModelMappingChain:      input.ModelMappingChain,
		CyberBlocked:           input.CyberBlocked,
		BillingModel:           result.BillingModel,
	}
	if result.ServiceTier != nil {
		facts.ServiceTier = strings.TrimSpace(*result.ServiceTier)
	}
	if result.ReasoningEffort != nil {
		facts.ReasoningEffort = strings.TrimSpace(*result.ReasoningEffort)
	}
	return facts
}

func firstTokenMilliseconds(value *int) int64 {
	if value == nil || *value < 0 {
		return 0
	}
	return int64(*value)
}

func accountID(account *service.Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}
