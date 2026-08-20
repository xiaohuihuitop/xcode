//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	"github.com/stretchr/testify/require"
)

type productUsageFinalizerStub struct {
	records []ProductUsageRecord
}

func (s *productUsageFinalizerStub) Finalize(_ context.Context, record ProductUsageRecord) error {
	s.records = append(s.records, record)
	return nil
}

func TestProductUsageSinkCarriesDecisionSnapshotToFinalizer(t *testing.T) {
	subscriptionID := int64(8)
	snapshot := applicationgateway.DecisionSnapshot{
		Grant: productcore.AccessGrant{KeyID: 4, UserID: 5},
		Decision: productcore.Decision{BillingAsset: &productcore.BillingAsset{
			Source:         "subscription",
			SubscriptionID: &subscriptionID,
			RateMultiplier: 1.25,
		}},
	}
	finalizer := &productUsageFinalizerStub{}
	factory := NewProductUsageSinkFactory(finalizer)
	sink := factory.ForDecision(snapshot)

	err := sink.RecordFinal(context.Background(), gatewayruntime.UsageEvent{
		RequestID: "usage-event-1",
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			InputTokens:  12,
			OutputTokens: 3,
			AccountID:    77,
		},
	})

	require.NoError(t, err)
	require.Len(t, finalizer.records, 1)
	require.Equal(t, "usage-event-1", finalizer.records[0].Event.RequestID)
	require.Equal(t, int64(8), *finalizer.records[0].Snapshot.Decision.BillingAsset.SubscriptionID)
	require.Equal(t, int64(77), finalizer.records[0].Event.Facts.AccountID)
}
