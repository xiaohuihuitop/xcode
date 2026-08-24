package service

import "fmt"

// newOpenAICompatBufferedReadFailoverError classifies a broken read from a
// buffered Responses stream. No client bytes have been committed at this
// point, so replaying the request on another account is safe.
func newOpenAICompatBufferedReadFailoverError(cause error) *UpstreamFailoverError {
	if cause == nil {
		cause = fmt.Errorf("upstream response body read failed")
	}
	return &UpstreamFailoverError{
		StatusCode:               502,
		Scope:                    GatewayFailureScopeAccount,
		Reason:                   GatewayFailureReason("openai_buffered_read"),
		NextAccountAction:        NextAccountRetry,
		Cause:                    cause,
		SafeToFailoverAfterWrite: false,
	}
}
