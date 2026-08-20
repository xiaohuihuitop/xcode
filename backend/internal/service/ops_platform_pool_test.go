package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type opsPlatformPoolAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (s *opsPlatformPoolAccountRepoStub) ListOpsAccountsForStats(_ context.Context, _ string, _ *int64) ([]Account, error) {
	return append([]Account(nil), s.accounts...), nil
}

func TestOpsPlatformPoolStatsAggregateByPlatformID(t *testing.T) {
	t.Parallel()

	poolA := int64(11)
	poolB := int64(12)
	repo := &opsPlatformPoolAccountRepoStub{accounts: []Account{
		{ID: 1, Name: "a1", Platform: PlatformOpenAI, PlatformID: &poolA, PlatformName: "GPT 主池", Status: StatusActive, Schedulable: true, Concurrency: 3},
		{ID: 2, Name: "a2", Platform: PlatformOpenAI, PlatformID: &poolA, PlatformName: "GPT 主池", Status: StatusActive, Schedulable: false, Concurrency: 2},
		{ID: 3, Name: "b1", Platform: PlatformOpenAI, PlatformID: &poolB, PlatformName: "GPT 备用池", Status: StatusActive, Schedulable: true, Concurrency: 4},
	}}
	svc := &OpsService{
		accountRepo: repo,
		settingRepo: newRuntimeSettingRepoStub(),
	}

	_, availabilityPools, accounts, _, err := svc.GetAccountAvailabilityStats(context.Background(), PlatformOpenAI, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), availabilityPools[poolA].TotalAccounts)
	require.Equal(t, int64(1), availabilityPools[poolA].AvailableCount)
	require.Equal(t, "GPT 主池", availabilityPools[poolA].PlatformName)
	require.Equal(t, poolA, accounts[1].PlatformID)
	require.Equal(t, "GPT 主池", accounts[1].PlatformName)
	require.Equal(t, int64(1), availabilityPools[poolB].AvailableCount)

	_, concurrencyPools, concurrencyAccounts, _, err := svc.GetConcurrencyStats(context.Background(), PlatformOpenAI, nil)
	require.NoError(t, err)
	require.Equal(t, int64(5), concurrencyPools[poolA].MaxCapacity)
	require.Equal(t, int64(4), concurrencyPools[poolB].MaxCapacity)
	require.Equal(t, "GPT 备用池", concurrencyPools[poolB].PlatformName)
	require.Equal(t, poolB, concurrencyAccounts[3].PlatformID)
}
