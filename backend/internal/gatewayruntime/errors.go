package gatewayruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type ErrorCategory string

const (
	ErrorCredentialInvalid       ErrorCategory = "credential_invalid"
	ErrorUpstreamForbidden       ErrorCategory = "upstream_forbidden"
	ErrorRateLimited             ErrorCategory = "rate_limited"
	ErrorUpstreamTimeout         ErrorCategory = "upstream_timeout"
	ErrorUpstream5xx             ErrorCategory = "upstream_5xx"
	ErrorNoAvailableAccount      ErrorCategory = "no_available_account"
	ErrorInvalidUpstreamResponse ErrorCategory = "invalid_upstream_response"
	ErrorClientCancelled         ErrorCategory = "client_cancelled"
)

type RuntimeError struct {
	Category  ErrorCategory
	Retryable bool
	AccountID int64
	Endpoint  Endpoint
	message   string
}

func NewRuntimeError(category ErrorCategory, retryable bool, message string) *RuntimeError {
	return &RuntimeError{Category: category, Retryable: retryable, message: safeRuntimeMessage(message)}
}

// RuntimeErrorFromStatus normalizes transport-level status results without
// copying upstream bodies or credentials into the application boundary.
func RuntimeErrorFromStatus(status int, message string) *RuntimeError {
	switch {
	case status == http.StatusUnauthorized:
		return NewRuntimeError(ErrorCredentialInvalid, true, message)
	case status == http.StatusForbidden:
		return NewRuntimeError(ErrorUpstreamForbidden, true, message)
	case status == http.StatusTooManyRequests:
		return NewRuntimeError(ErrorRateLimited, true, message)
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return NewRuntimeError(ErrorUpstreamTimeout, true, message)
	case status >= http.StatusInternalServerError && status <= 599:
		return NewRuntimeError(ErrorUpstream5xx, true, message)
	default:
		return NewRuntimeError(ErrorInvalidUpstreamResponse, false, message)
	}
}

func RuntimeErrorFromContext(err error) *RuntimeError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return NewRuntimeError(ErrorClientCancelled, false, "client request cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewRuntimeError(ErrorUpstreamTimeout, true, "request timed out")
	}
	return NewRuntimeError(ErrorInvalidUpstreamResponse, false, err.Error())
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "runtime error"
	}
	if e.message == "" {
		return string(e.Category)
	}
	return fmt.Sprintf("%s: %s", e.Category, e.message)
}

func safeRuntimeMessage(message string) string {
	if message == "" {
		return ""
	}
	// Runtime errors are safe diagnostics, not an upstream body or credential
	// container. Keep only a short single-line summary.
	for _, forbidden := range []string{"access_token", "refresh_token", "Authorization", "Bearer "} {
		if containsRuntimeSecretMarker(message, forbidden) {
			return "upstream request failed"
		}
	}
	return message
}

func containsRuntimeSecretMarker(value, marker string) bool {
	for i := 0; i+len(marker) <= len(value); i++ {
		if value[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}
