package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type platformPoolAccountListerStub struct {
	platformID      int64
	accountPlatform string
	accounts        []Account
}

func (s *platformPoolAccountListerStub) ListSchedulableByPlatformPool(
	_ context.Context,
	platformID int64,
	accountPlatform string,
) ([]Account, error) {
	s.platformID = platformID
	s.accountPlatform = accountPlatform
	return append([]Account(nil), s.accounts...), nil
}

func TestListPlatformPoolSchedulableAccountsUsesExplicitPool(t *testing.T) {
	repo := &platformPoolAccountListerStub{accounts: []Account{{ID: 9, Platform: PlatformOpenAI}}}
	scope := PlatformSchedulingScope{PlatformID: 42, AccountPlatform: PlatformOpenAI}

	accounts, err := listPlatformPoolSchedulableAccounts(context.Background(), repo, scope)

	require.NoError(t, err)
	require.Equal(t, int64(42), repo.platformID)
	require.Equal(t, PlatformOpenAI, repo.accountPlatform)
	require.Equal(t, []Account{{ID: 9, Platform: PlatformOpenAI}}, accounts)
}

func TestPlatformAssetIDUsesBusinessPlatformID(t *testing.T) {
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID:      42,
		PlatformCode:    "openai",
		AccountPlatform: PlatformOpenAI,
	})

	require.Equal(t, int64(-43), *PlatformSchedulingID(ctx))
	require.Equal(t, int64(42), *PlatformAssetID(ctx))
}
