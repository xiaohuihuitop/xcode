package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type assetPermissionsAPIKeyRepoStub struct {
	*apiKeyRepoStub
	created  *APIKey
	replaced []APIKeyAssetPermissions
}

func (s *assetPermissionsAPIKeyRepoStub) Create(_ context.Context, key *APIKey) error {
	key.ID = 101
	copy := *key
	s.created = &copy
	return nil
}

func (s *assetPermissionsAPIKeyRepoStub) ExistsByKey(context.Context, string) (bool, error) {
	return false, nil
}

func (s *assetPermissionsAPIKeyRepoStub) ReplaceAssetPermissions(
	_ context.Context,
	_ int64,
	permissions APIKeyAssetPermissions,
) error {
	s.replaced = append(s.replaced, NormalizeAPIKeyAssetPermissions(permissions))
	return nil
}

func TestValidateAPIKeyAssetPermissions(t *testing.T) {
	require.ErrorIs(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{}), ErrAPIKeyPlatformRequired)
	require.ErrorIs(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{PlatformIDs: []int64{9}}), ErrAPIKeyBillingSourceRequired)
	require.NoError(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{
		PlatformIDs:  []int64{9},
		AllowBalance: true,
	}))
	require.NoError(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{
		PlatformIDs:         []int64{9},
		SubscriptionPlanIDs: []int64{18},
	}))
}

func TestNewAPIKeyFromCreateRequestDefaultsToBalanceAllowed(t *testing.T) {
	key := newAPIKeyFromCreateRequest(CreateAPIKeyRequest{Name: "default-balance"})

	require.True(t, key.AllowBalance)
}

func TestNewAPIKeyFromCreateRequestHonorsExplicitBalanceDisable(t *testing.T) {
	disabled := false
	key := newAPIKeyFromCreateRequest(CreateAPIKeyRequest{
		Name:         "plans-only",
		AllowBalance: &disabled,
	})

	require.False(t, key.AllowBalance)
}

func TestAPIKeyServiceCreateCarriesExplicitAssetPermissions(t *testing.T) {
	disabled := false
	repo := &assetPermissionsAPIKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{}}
	svc := &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &userRepoStub{user: &User{ID: 7}},
	}

	key, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:                "plans-only",
		CustomKey:           stringPtr("sk-plans-only-credential"),
		PlatformIDs:         []int64{30, 10, 30},
		SubscriptionPlanIDs: []int64{80, 20, 80},
		AllowBalance:        &disabled,
	})

	require.NoError(t, err)
	require.Equal(t, []int64{10, 30}, key.AllowedPlatformIDs)
	require.Equal(t, []int64{20, 80}, key.AllowedSubscriptionPlanIDs)
	require.False(t, key.AllowBalance)
	require.NotNil(t, repo.created)
	require.Equal(t, []int64{10, 30}, repo.created.AllowedPlatformIDs)
	require.Equal(t, []int64{20, 80}, repo.created.AllowedSubscriptionPlanIDs)
}

func TestAPIKeyServiceUpdateReplacesExplicitAssetPermissions(t *testing.T) {
	oldPlatformIDs := []int64{1}
	oldPlanIDs := []int64{2}
	repo := &assetPermissionsAPIKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{apiKey: &APIKey{
		ID:                         101,
		UserID:                     7,
		Key:                        "sk-existing-key-credential",
		Status:                     StatusActive,
		AllowedPlatformIDs:         oldPlatformIDs,
		AllowedSubscriptionPlanIDs: oldPlanIDs,
		AllowBalance:               true,
	}}}
	platformIDs := []int64{30, 10, 30}
	planIDs := []int64{80, 20, 80}
	disabled := false
	svc := &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &userRepoStub{user: &User{ID: 7}},
	}

	updated, err := svc.Update(context.Background(), 101, 7, UpdateAPIKeyRequest{
		PlatformIDs:         &platformIDs,
		SubscriptionPlanIDs: &planIDs,
		AllowBalance:        &disabled,
	})

	require.NoError(t, err)
	require.Equal(t, []int64{10, 30}, updated.AllowedPlatformIDs)
	require.Equal(t, []int64{20, 80}, updated.AllowedSubscriptionPlanIDs)
	require.False(t, updated.AllowBalance)
	require.Equal(t, []APIKeyAssetPermissions{{
		PlatformIDs:         []int64{10, 30},
		SubscriptionPlanIDs: []int64{20, 80},
		AllowBalance:        false,
	}}, repo.replaced)
}

func TestAPIKeyAuthSnapshotRoundTripPreservesAssetPermissions(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{
		ID:                         101,
		UserID:                     7,
		Key:                        "sk-cached-key-credential",
		Status:                     StatusActive,
		AllowedPlatformIDs:         []int64{10, 30},
		AllowedSubscriptionPlanIDs: []int64{20, 80},
		AllowBalance:               false,
		User:                       &User{ID: 7, Status: StatusActive},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	key.AllowedPlatformIDs[0] = 99
	key.AllowedSubscriptionPlanIDs[0] = 88
	restored := svc.snapshotToAPIKey(key.Key, snapshot)

	require.Equal(t, []int64{10, 30}, restored.AllowedPlatformIDs)
	require.Equal(t, []int64{20, 80}, restored.AllowedSubscriptionPlanIDs)
	require.False(t, restored.AllowBalance)
}
