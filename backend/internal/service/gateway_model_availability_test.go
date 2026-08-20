//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelAvailabilityRepoStub struct {
	AccountRepository
	accounts        []Account
	platformID      int64
	accountPlatform string
}

func (s *modelAvailabilityRepoStub) ListModelAvailabilityCandidates(
	_ context.Context,
	platformID int64,
	accountPlatform string,
) ([]Account, error) {
	s.platformID = platformID
	s.accountPlatform = accountPlatform
	return append([]Account(nil), s.accounts...), nil
}

func TestDiagnoseModelAvailabilityUsesPlatformPool(t *testing.T) {
	repo := &modelAvailabilityRepoStub{accounts: []Account{{ID: 1}}}
	svc := &GatewayService{accountRepo: repo}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 42, AccountPlatform: PlatformOpenAI,
	})

	diagnosis := svc.DiagnoseModelAvailabilityForPlatform(ctx, "gpt-5.6", PlatformOpenAI)

	require.True(t, diagnosis.HasAccountsInPool)
	require.True(t, diagnosis.HasModelSupport)
	require.Equal(t, int64(42), repo.platformID)
	require.Equal(t, PlatformOpenAI, repo.accountPlatform)
}

func TestDiagnoseModelAvailabilityRejectsMismatchedScope(t *testing.T) {
	repo := &modelAvailabilityRepoStub{}
	svc := &GatewayService{accountRepo: repo}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 42, AccountPlatform: PlatformOpenAI,
	})

	diagnosis := svc.DiagnoseModelAvailabilityForPlatform(ctx, "glm-5", PlatformAnthropic)

	require.True(t, diagnosis.HasAccountsInPool)
	require.True(t, diagnosis.HasModelSupport)
	require.Zero(t, repo.platformID)
}

func TestOpenAIDiagnoseModelAvailabilityUsesPlatformPool(t *testing.T) {
	repo := &modelAvailabilityRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 9, AccountPlatform: PlatformGrok,
	})

	diagnosis := svc.DiagnoseModelAvailabilityForPlatform(ctx, "grok-4.5", PlatformGrok)

	require.False(t, diagnosis.HasAccountsInPool)
	require.False(t, diagnosis.HasModelSupport)
	require.Equal(t, int64(9), repo.platformID)
	require.Equal(t, PlatformGrok, repo.accountPlatform)
}
