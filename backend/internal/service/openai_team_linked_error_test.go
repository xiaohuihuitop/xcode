//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAITeamLinkedAccountRepoStub struct {
	mockAccountRepoForGemini
	accounts    []Account
	setErrorIDs []int64
}

func (r *openAITeamLinkedAccountRepoStub) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	result := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.Status == StatusActive {
			result = append(result, account)
		}
	}
	return result, nil
}

func (r *openAITeamLinkedAccountRepoStub) SetError(_ context.Context, id int64, _ string) error {
	r.setErrorIDs = append(r.setErrorIDs, id)
	return nil
}

func TestOpenAITeamLinkedDeactivationBlocksSiblingAccounts(t *testing.T) {
	repo := &openAITeamLinkedAccountRepoStub{accounts: []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "team-a"}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "team-a"}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"chatgpt_account_id": "team-b"}},
	}}
	rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	trigger := repo.accounts[0]
	disabled := rateLimits.HandleUpstreamError(
		context.Background(),
		&trigger,
		http.StatusPaymentRequired,
		http.Header{},
		[]byte(`{"detail":{"code":"deactivated_workspace"}}`),
	)

	require.True(t, disabled)
	require.Equal(t, []int64{2, 1}, repo.setErrorIDs)
}
