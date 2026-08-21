package v1

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RuntimeError struct {
	Category            string  `json:"category"`
	Message             string  `json:"message"`
	Retryable           bool    `json:"retryable"`
	AttemptedAccountIDs []int64 `json:"attempted_account_ids,omitempty"`
	UpstreamStatus      int     `json:"upstream_status,omitempty"`
}

func NewRuntimeError(category, message string, retryable bool) RuntimeError {
	return RuntimeError{Category: category, Message: sanitizeRuntimeMessage(message), Retryable: retryable}
}

func (e RuntimeError) Clone() RuntimeError {
	e.AttemptedAccountIDs = append([]int64(nil), e.AttemptedAccountIDs...)
	return e
}

func (e RuntimeError) Error() string {
	message := sanitizeRuntimeMessage(e.Message)
	if e.Category == "" {
		return message
	}
	if message == "" {
		return e.Category
	}
	return fmt.Sprintf("%s: %s", e.Category, message)
}

// MarshalJSON sanitizes diagnostics at the contract boundary. Runtime errors
// intentionally have no credential fields, and messages containing common
// credential markers are replaced before they can cross process boundaries.
func (e RuntimeError) MarshalJSON() ([]byte, error) {
	type wire RuntimeError
	e.Message = sanitizeRuntimeMessage(e.Message)
	return json.Marshal(wire(e))
}

func sanitizeRuntimeMessage(message string) string {
	if message == "" {
		return ""
	}
	lower := strings.ToLower(message)
	for _, marker := range []string{"access_token", "refresh_token", "authorization", "bearer "} {
		if strings.Contains(lower, marker) {
			return "runtime request failed"
		}
	}
	return message
}
