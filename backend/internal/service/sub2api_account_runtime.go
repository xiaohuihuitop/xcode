package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

type accountProbeService interface {
	ProbeAccount(context.Context, int64) (*UpstreamBillingProbeSnapshot, error)
}

// Sub2APIAccountRuntime exposes sanitized account health through the runtime
// capability port. Credentials, tokens and raw upstream payloads never cross
// this boundary.
type Sub2APIAccountRuntime struct {
	probe accountProbeService
}

func NewSub2APIAccountRuntime(probe accountProbeService) *Sub2APIAccountRuntime {
	return &Sub2APIAccountRuntime{probe: probe}
}

func (r *Sub2APIAccountRuntime) ProbeAccount(ctx context.Context, request gatewayruntime.AccountProbeRequest) (gatewayruntime.AccountProbeResult, error) {
	if r == nil || r.probe == nil {
		return gatewayruntime.AccountProbeResult{}, ErrUpstreamBillingProbeUnavailable
	}
	snapshot, err := r.probe.ProbeAccount(ctx, request.AccountID)
	if err != nil {
		runtimeErr := gatewayruntime.RuntimeErrorFromContext(err)
		return gatewayruntime.AccountProbeResult{Healthy: false, ErrorCategory: runtimeErr.Category}, err
	}
	if snapshot == nil {
		return gatewayruntime.AccountProbeResult{Healthy: false, ErrorCategory: gatewayruntime.ErrorInvalidUpstreamResponse}, nil
	}
	result := gatewayruntime.AccountProbeResult{Healthy: snapshot.Status == UpstreamBillingProbeStatusOK}
	if !result.Healthy {
		result.ErrorCategory = gatewayruntime.ErrorInvalidUpstreamResponse
	}
	return result, nil
}

var _ gatewayruntime.AccountRuntime = (*Sub2APIAccountRuntime)(nil)
