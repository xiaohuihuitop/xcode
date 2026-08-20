//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type restoreUserSubRepoStub struct {
	userSubRepoNoop
	sub            *UserSubscription
	restoreCalls   int
	restoredStatus string
}

func (r *restoreUserSubRepoStub) GetByIDIncludeDeleted(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	copy := *r.sub
	return &copy, nil
}

func (r *restoreUserSubRepoStub) Restore(_ context.Context, id int64, status string) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	r.restoreCalls++
	r.restoredStatus = status
	copy := *r.sub
	copy.Status = status
	copy.DeletedAt = nil
	r.sub = &copy
	return &copy, nil
}

func TestRestoreSubscription_ExpiredActiveRestoresAsExpired(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &restoreUserSubRepoStub{sub: &UserSubscription{
		ID: 1, UserID: 10, Status: SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(-time.Minute), DeletedAt: &deletedAt,
	}}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	t.Cleanup(svc.Stop)

	restored, err := svc.RestoreSubscription(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, repo.restoreCalls)
	require.Equal(t, SubscriptionStatusExpired, repo.restoredStatus)
	require.Equal(t, SubscriptionStatusExpired, restored.Status)
	require.Nil(t, restored.DeletedAt)
}

func TestRestoreSubscription_NotRevokedReturnsConflict(t *testing.T) {
	repo := &restoreUserSubRepoStub{sub: &UserSubscription{
		ID: 1, UserID: 10, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour),
	}}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	t.Cleanup(svc.Stop)

	_, err := svc.RestoreSubscription(context.Background(), 1)
	require.ErrorIs(t, err, ErrSubscriptionNotRevoked)
	require.Zero(t, repo.restoreCalls)
}

func TestRestoreSubscription_AllowsParallelActiveSubscription(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &restoreUserSubRepoStub{sub: &UserSubscription{
		ID: 1, UserID: 10, Status: SubscriptionStatusExpired,
		ExpiresAt: time.Now().Add(-time.Hour), DeletedAt: &deletedAt,
	}}
	svc := NewSubscriptionService(repo, nil, nil, nil)
	t.Cleanup(svc.Stop)

	restored, err := svc.RestoreSubscription(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, repo.restoreCalls)
	require.NotNil(t, restored)
}
