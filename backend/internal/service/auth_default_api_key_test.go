//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type defaultAPIKeyProvisionerStub struct {
	calls int
	err   error
}

type defaultAPIKeyPlatformListerStub struct {
	platforms []Platform
}

func (s defaultAPIKeyPlatformListerStub) List(context.Context) ([]Platform, error) {
	return s.platforms, nil
}

func (s *defaultAPIKeyProvisionerStub) EnsureDefaultAPIKey(context.Context, int64) error {
	s.calls++
	return s.err
}

func TestPostAuthUserBootstrapEnsuresDefaultAPIKeyWithoutBlockingAuth(t *testing.T) {
	provisioner := &defaultAPIKeyProvisionerStub{err: errors.New("key store unavailable")}
	svc := &AuthService{defaultAPIKeyProvisioner: provisioner}

	svc.postAuthUserBootstrap(context.Background(), &User{ID: 7}, "email", false)

	require.Equal(t, 1, provisioner.calls)
}

func TestSuccessfulLoginRetriesMissingDefaultAPIKey(t *testing.T) {
	provisioner := &defaultAPIKeyProvisionerStub{}
	svc := &AuthService{
		defaultAPIKeyProvisioner: provisioner,
		userRepo:                 &userRepoStub{user: &User{ID: 7, Email: "user@example.com"}},
	}

	svc.RecordSuccessfulLogin(context.Background(), 7)

	require.Equal(t, 1, provisioner.calls)
}

func TestAPIKeyServiceEnsureDefaultAPIKeyUsesAllActivePlatformsAndBilling(t *testing.T) {
	repo := &assetPermissionsAPIKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{
		allowListAllByUserID: true,
	}}
	svc := &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &userRepoStub{user: &User{ID: 7}},
		cfg:        &config.Config{Default: config.DefaultConfig{APIKeyPrefix: "sk-"}},
		platformLister: defaultAPIKeyPlatformListerStub{platforms: []Platform{
			{ID: 2, Status: PlatformStatusActive},
			{ID: 1, Status: StatusDisabled},
		}},
	}

	err := svc.EnsureDefaultAPIKey(context.Background(), 7)

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.Equal(t, []int64{2}, repo.created.AllowedPlatformIDs)
	require.True(t, repo.created.AllowAllSubscriptions)
	require.True(t, repo.created.AllowBalance)
}
