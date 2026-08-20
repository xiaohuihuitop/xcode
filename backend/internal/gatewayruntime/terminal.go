package gatewayruntime

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrTerminalAlreadyRecorded = errors.New("runtime terminal event already recorded")
	ErrUsageSinkUnavailable    = errors.New("runtime usage sink unavailable")
)

type TerminalRecorder struct {
	mu       sync.Mutex
	recorded bool
	sink     UsageSink
}

func NewTerminalRecorder(sink UsageSink) *TerminalRecorder {
	return &TerminalRecorder{sink: sink}
}

// Recorded reports whether a terminal event has already been accepted by the
// recorder. It is used by adapter boundaries to reject executors that return
// without producing the required exactly-once usage outcome.
func (r *TerminalRecorder) Recorded() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	recorded := r.recorded
	r.mu.Unlock()
	return recorded
}

func (r *TerminalRecorder) RecordFinal(ctx context.Context, event UsageEvent) error {
	if r == nil {
		return ErrUsageSinkUnavailable
	}
	r.mu.Lock()
	if r.recorded {
		r.mu.Unlock()
		return ErrTerminalAlreadyRecorded
	}
	r.recorded = true
	sink := r.sink
	r.mu.Unlock()
	if sink == nil {
		return ErrUsageSinkUnavailable
	}
	return sink.RecordFinal(ctx, event)
}
