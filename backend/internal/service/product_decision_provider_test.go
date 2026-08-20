//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/productcore"
	"github.com/stretchr/testify/require"
)

func TestProductDecisionProviderResolvesFromImmutableGrant(t *testing.T) {
	planID := int64(17)
	provider := NewProductDecisionProvider(newPlatformAssetProductCoreAdapterForTest(&UserSubscription{
		ID: 21, UserID: 7, SubscriptionPlanID: &planID, RateMultiplierSnapshot: 0.5,
	}))
	grant := productcore.AccessGrant{
		KeyID:               10,
		UserID:              7,
		Balance:             10,
		PlatformIDs:         []int64{3},
		SubscriptionPlanIDs: []int64{17},
		AllowBalance:        true,
	}

	decision, err := provider.Resolve(context.Background(), grant, productcore.Request{
		Model:              "gpt-4o",
		EndpointCapability: "chat_completions",
	})

	require.NoError(t, err)
	require.Equal(t, int64(3), decision.Platform.ID)
	require.Equal(t, int64(21), *decision.BillingAsset.SubscriptionID)
	require.Equal(t, 0.5, decision.BillingAsset.RateMultiplier)
	require.Equal(t, []int64{3}, grant.PlatformIDs)
}
