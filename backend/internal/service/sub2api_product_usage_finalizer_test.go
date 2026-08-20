//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
)

func TestProductUsageTimingPreservesRuntimeLatency(t *testing.T) {
	event := gatewayruntime.UsageEvent{
		RequestID: "usage-latency",
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			DurationMilliseconds:   4321,
			FirstTokenMilliseconds: 876,
		},
	}

	openAIResult := openAIForwardResultFromProductUsage(event)
	require.Equal(t, 4321*time.Millisecond, openAIResult.Duration)
	require.NotNil(t, openAIResult.FirstTokenMs)
	require.Equal(t, 876, *openAIResult.FirstTokenMs)

	gatewayResult := gatewayForwardResultFromProductUsage(event)
	require.Equal(t, 4321*time.Millisecond, gatewayResult.Duration)
	require.NotNil(t, gatewayResult.FirstTokenMs)
	require.Equal(t, 876, *gatewayResult.FirstTokenMs)
}

func TestProductUsageTimingOmitsUnavailableFirstToken(t *testing.T) {
	event := gatewayruntime.UsageEvent{
		RequestID: "usage-no-first-token",
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			DurationMilliseconds: 250,
		},
	}

	require.Nil(t, openAIForwardResultFromProductUsage(event).FirstTokenMs)
	require.Nil(t, gatewayForwardResultFromProductUsage(event).FirstTokenMs)
}

func TestSub2APIProductUsageFinalizerSkipsFailedTerminalEvents(t *testing.T) {
	finalizer := NewSub2APIProductUsageFinalizer(nil, nil, nil)
	err := finalizer.Finalize(context.Background(), ProductUsageRecord{
		Snapshot: applicationgateway.DecisionSnapshot{},
		Event: gatewayruntime.UsageEvent{
			Success: false,
			Error:   gatewayruntime.NewRuntimeError(gatewayruntime.ErrorUpstream5xx, true, "upstream failed"),
		},
	})
	require.NoError(t, err)
}

func TestSub2APIProductUsageFinalizerRequiresCompleteDependenciesForSuccessfulEvent(t *testing.T) {
	finalizer := NewSub2APIProductUsageFinalizer(nil, nil, nil)
	err := finalizer.Finalize(context.Background(), ProductUsageRecord{
		Snapshot: applicationgateway.DecisionSnapshot{},
		Event: gatewayruntime.UsageEvent{
			Success: true,
			Facts:   gatewayruntime.UsageFacts{AccountID: 1},
		},
	})
	require.ErrorIs(t, err, ErrProductUsageFinalizerUnavailable)
}
