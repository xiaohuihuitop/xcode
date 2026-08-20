//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type usageBridgeRecordingSink struct {
	events []gatewayruntime.UsageEvent
}

func (s *usageBridgeRecordingSink) RecordFinal(_ context.Context, event gatewayruntime.UsageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestRecordOpenAIUsageUsesRuntimeSinkWhenPresent(t *testing.T) {
	sink := &usageBridgeRecordingSink{}
	ctx := gatewayruntime.WithUsageSink(context.Background(), sink)
	firstTokenMs := 123

	err := recordOpenAIUsage(ctx, nil, &service.OpenAIRecordUsageInput{
		Result: &service.OpenAIForwardResult{
			RequestID:     "usage-bridge-openai",
			Model:         "gpt-5.6",
			BillingModel:  "gpt-5.6",
			UpstreamModel: "gpt-5.6-2026",
			Usage: service.OpenAIUsage{
				InputTokens:              7,
				OutputTokens:             3,
				CacheReadInputTokens:     2,
				CacheCreationInputTokens: 1,
			},
			Duration:     2 * time.Second,
			FirstTokenMs: &firstTokenMs,
		},
		InboundEndpoint:  "/v1/responses",
		UpstreamEndpoint: "/v1/responses",
		UserAgent:        "runtime-test",
		ModelRoutingUsageFields: service.ModelRoutingUsageFields{
			OriginalModel: "gpt-5.6",
			MappedModel:   "gpt-5.6-2026",
		},
	})

	require.NoError(t, err)
	require.Len(t, sink.events, 1)
	event := sink.events[0]
	require.Equal(t, "usage-bridge-openai", event.RequestID)
	require.True(t, event.Success)
	require.Equal(t, 7, event.Facts.InputTokens)
	require.Equal(t, 3, event.Facts.OutputTokens)
	require.Equal(t, 2, event.Facts.CacheReadTokens)
	require.Equal(t, int64(123), event.Facts.FirstTokenMilliseconds)
	require.Equal(t, "/v1/responses", event.Facts.UpstreamEndpoint)
}

func TestRecordGatewayUsageUsesRuntimeSinkWhenPresent(t *testing.T) {
	sink := &usageBridgeRecordingSink{}
	ctx := gatewayruntime.WithUsageSink(context.Background(), sink)

	err := recordGatewayUsage(ctx, nil, &service.RecordUsageInput{
		Result: &service.ForwardResult{
			RequestID:     "usage-bridge-gateway",
			Model:         "claude-sonnet",
			UpstreamModel: "claude-sonnet-2026",
			Usage:         service.ClaudeUsage{InputTokens: 4, OutputTokens: 6},
			Duration:      time.Second,
		},
		InboundEndpoint:  "/v1/messages",
		UpstreamEndpoint: "/v1/messages",
		ModelRoutingUsageFields: service.ModelRoutingUsageFields{
			OriginalModel: "claude-sonnet",
			MappedModel:   "claude-sonnet-2026",
		},
	})

	require.NoError(t, err)
	require.Len(t, sink.events, 1)
	require.Equal(t, "usage-bridge-gateway", sink.events[0].RequestID)
	require.Equal(t, 4, sink.events[0].Facts.InputTokens)
	require.Equal(t, 6, sink.events[0].Facts.OutputTokens)
}
