//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBulkAssignSubscriptionFromPlanCreatesSeparateInstances(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	plan, err := client.SubscriptionPlan.Create().
		SetName("Parallel package").
		SetPrice(9.9).
		SetValidityDays(30).
		SetRateMultiplier(1.5).
		Save(ctx)
	require.NoError(t, err)

	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(subRepo, nil, client, nil)
	t.Cleanup(svc.Stop)

	result, err := svc.BulkAssignSubscriptionFromPlan(ctx, &BulkAssignSubscriptionFromPlanInput{
		UserIDs: []int64{42, 42},
		PlanID:  plan.ID,
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 2, result.CreatedCount)
	require.Zero(t, result.ReusedCount)
	require.Len(t, result.Subscriptions, 2)
	require.Len(t, subRepo.byID, 2)
	for _, subscription := range result.Subscriptions {
		require.Equal(t, plan.ID, *subscription.SubscriptionPlanID)
		require.Equal(t, "created", result.Statuses[subscription.UserID])
	}
}
