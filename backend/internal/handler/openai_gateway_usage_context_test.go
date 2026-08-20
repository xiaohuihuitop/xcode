package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &GatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "client-request-123", gotClientRequestID)
	require.Equal(t, "request-456", gotRequestID)
}

func TestOpenAISubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "openai-client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "openai-request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "openai-client-request-123", gotClientRequestID)
	require.Equal(t, "openai-request-456", gotRequestID)
}

func TestOpenAISubmitUsageRecordTaskPreservesRuntimeUsageSink(t *testing.T) {
	parent := gatewayruntime.WithUsageSink(context.Background(), recordingUsageSink{})
	var got gatewayruntime.UsageSink
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		got, _ = gatewayruntime.UsageSinkFromContext(ctx)
	})

	require.NotNil(t, got)
}

type recordingUsageSink struct{}

func (recordingUsageSink) RecordFinal(context.Context, gatewayruntime.UsageEvent) error { return nil }

func TestOpenAISubmitUsageRecordTaskPreservesPlatformAssetContext(t *testing.T) {
	platformID := int64(42)
	parent := service.WithGatewayPlatformAssetContext(context.Background(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:      platformID,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
		BillingAsset: &service.ResolvedBillingAsset{
			Source:         service.BillingSourceBalance,
			RateMultiplier: 1.25,
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      platformID,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
	})

	var got *service.GatewayPlatformAssetContext
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		got, _ = service.GatewayPlatformAssetContextFromContext(ctx)
	})

	require.NotNil(t, got)
	require.Equal(t, platformID, got.Platform.PlatformID)
	require.Equal(t, service.BillingSourceBalance, got.BillingAsset.Source)
	require.Equal(t, 1.25, got.BillingAsset.RateMultiplier)
}

func TestUsageRecordContextPreservesRouteAfterParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	parent = service.WithGatewayPlatformAssetContext(parent, &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:      42,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
		BillingAsset: &service.ResolvedBillingAsset{
			Source:         service.BillingSourceSubscription,
			SubscriptionID: int64Pointer(7),
			RateMultiplier: 1.5,
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      42,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
	})
	cancel()

	workerContext := usageRecordContext(parent, context.Background())
	route, ok := service.GatewayPlatformAssetContextFromContext(workerContext)

	require.NoError(t, workerContext.Err())
	require.True(t, ok)
	require.Equal(t, service.BillingSourceSubscription, route.BillingAsset.Source)
	require.Equal(t, int64(7), *route.BillingAsset.SubscriptionID)
}

func TestCyberPolicyRecordContextPreservesRouteAfterParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	parent = service.WithGatewayPlatformAssetContext(parent, &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:      42,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
		BillingAsset: &service.ResolvedBillingAsset{
			Source:         service.BillingSourceSubscription,
			SubscriptionID: int64Pointer(7),
			RateMultiplier: 1.5,
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      42,
			PlatformCode:    "openai",
			AccountPlatform: service.PlatformOpenAI,
		},
	})
	cancelParent()

	workerContext, cancelWorker := newCyberPolicyRecordContext(parent)
	defer cancelWorker()
	route, ok := service.GatewayPlatformAssetContextFromContext(workerContext)

	require.NoError(t, workerContext.Err())
	require.True(t, ok)
	require.Equal(t, int64(42), route.Platform.PlatformID)
	require.Equal(t, service.BillingSourceSubscription, route.BillingAsset.Source)
	require.Equal(t, int64(7), *route.BillingAsset.SubscriptionID)
	require.Equal(t, 1.5, route.BillingAsset.RateMultiplier)
}

func TestLiveCallIdentityUsesBusinessPlatformID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest("POST", "/v1/live", nil)
	request = request.WithContext(service.WithPlatformSchedulingScope(request.Context(), service.PlatformSchedulingScope{
		PlatformID:      42,
		PlatformCode:    "openai",
		AccountPlatform: service.PlatformOpenAI,
	}))
	c.Request = request

	identity := liveCallIdentity(c, &service.APIKey{ID: 7}, 9, nil)

	require.NotNil(t, identity.PlatformID)
	require.Equal(t, int64(42), *identity.PlatformID)
}

func int64Pointer(value int64) *int64 {
	return &value
}
