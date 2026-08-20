//go:build unit

package service

import (
	"context"
	"errors"
)

// mockAccountRepoForGemini is the shared account repository test double used by
// provider refresh and error-policy tests. Embedding the production interface
// keeps unrelated methods out of individual fixtures while explicit methods
// below provide deterministic account lookups.
type mockAccountRepoForGemini struct {
	AccountRepository
	accounts         []Account
	accountsByID     map[int64]*Account
	listPlatformFunc func(context.Context, string) ([]Account, error)
	getByIDCalls     int
}

func (m *mockAccountRepoForGemini) GetByID(_ context.Context, id int64) (*Account, error) {
	m.getByIDCalls++
	if account, ok := m.accountsByID[id]; ok {
		return account, nil
	}
	return nil, errors.New("account not found")
}

func (m *mockAccountRepoForGemini) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if m.listPlatformFunc != nil {
		return m.listPlatformFunc(ctx, platform)
	}
	accounts := make([]Account, 0, len(m.accounts))
	for _, account := range m.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (m *mockAccountRepoForGemini) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

type mockAccountRepoForPlatform = mockAccountRepoForGemini

type stubOpenAIAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r stubOpenAIAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, errors.New("account not found")
}

func (r stubOpenAIAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r stubOpenAIAccountRepo) ListSchedulableByPlatformPool(_ context.Context, platformID int64, platform string) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.PlatformID != nil && *account.PlatformID == platformID && account.Platform == platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type snapshotUpdateAccountRepo struct {
	stubOpenAIAccountRepo
	updateExtraCalls chan map[string]any
}

func (r *snapshotUpdateAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.updateExtraCalls != nil {
		copied := make(map[string]any, len(updates))
		for key, value := range updates {
			copied[key] = value
		}
		r.updateExtraCalls <- copied
	}
	return nil
}

func (m *mockAccountRepoForGemini) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	accounts := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := m.accountsByID[id]; ok {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (m *mockAccountRepoForGemini) ExistsByID(_ context.Context, id int64) (bool, error) {
	_, ok := m.accountsByID[id]
	return ok, nil
}
