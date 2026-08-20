package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type subscriptionPlanCandidatesRepoStub struct {
	userSubRepoNoop
	candidates       []UserSubscription
	requestedPlanIDs []int64
	calls            int
}

func (r *subscriptionPlanCandidatesRepoStub) ListActiveByUserIDAndPlanIDs(
	_ context.Context,
	_ int64,
	planIDs []int64,
) ([]UserSubscription, error) {
	r.calls++
	r.requestedPlanIDs = append([]int64(nil), planIDs...)
	return append([]UserSubscription(nil), r.candidates...), nil
}

func TestListActiveSubscriptionsByPlanIDsUsesDistinctSortedPlanIDs(t *testing.T) {
	repo := &subscriptionPlanCandidatesRepoStub{
		candidates: []UserSubscription{{ID: 11}, {ID: 12}},
	}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptionsByPlanIDs(
		context.Background(),
		10,
		[]int64{30, 10, 30, 20},
	)

	require.NoError(t, err)
	require.Equal(t, []int64{10, 20, 30}, repo.requestedPlanIDs)
	require.Equal(t, []int64{11, 12}, []int64{subscriptions[0].ID, subscriptions[1].ID})
	require.Equal(t, 1, repo.calls)
}

func TestListActiveSubscriptionsByPlanIDsSkipsRepositoryForNoPlans(t *testing.T) {
	repo := &subscriptionPlanCandidatesRepoStub{}
	svc := &SubscriptionService{userSubRepo: repo}

	subscriptions, err := svc.ListActiveSubscriptionsByPlanIDs(context.Background(), 10, nil)

	require.NoError(t, err)
	require.Empty(t, subscriptions)
	require.Zero(t, repo.calls)
}
