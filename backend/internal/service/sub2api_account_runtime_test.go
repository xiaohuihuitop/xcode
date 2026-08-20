//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
)

type accountProbeStub struct {
	snapshot  *UpstreamBillingProbeSnapshot
	err       error
	accountID int64
}

func (s *accountProbeStub) ProbeAccount(_ context.Context, accountID int64) (*UpstreamBillingProbeSnapshot, error) {
	s.accountID = accountID
	return s.snapshot, s.err
}

func TestSub2APIAccountRuntimeReturnsSanitizedHealth(t *testing.T) {
	probe := &accountProbeStub{snapshot: &UpstreamBillingProbeSnapshot{Status: UpstreamBillingProbeStatusOK}}
	runtime := NewSub2APIAccountRuntime(probe)

	result, err := runtime.ProbeAccount(context.Background(), gatewayruntime.AccountProbeRequest{AccountID: 42})

	require.NoError(t, err)
	require.True(t, result.Healthy)
	require.Equal(t, int64(42), probe.accountID)
	require.Empty(t, result.EndpointCapabilities)
}

func TestSub2APIAccountRuntimeMapsProbeFailureWithoutCredentials(t *testing.T) {
	wantErr := errors.New("upstream failed")
	runtime := NewSub2APIAccountRuntime(&accountProbeStub{err: wantErr})

	result, err := runtime.ProbeAccount(context.Background(), gatewayruntime.AccountProbeRequest{AccountID: 7})

	require.ErrorIs(t, err, wantErr)
	require.False(t, result.Healthy)
	require.NotEmpty(t, result.ErrorCategory)
}
