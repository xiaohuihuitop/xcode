package v1

import (
	"errors"
	"sync"
)

var (
	ErrTerminalAlreadyRecorded = errors.New("runtime terminal event already recorded")
	ErrNotTerminalEvent        = errors.New("runtime event is not terminal")
)

// IsTerminal reports whether an event closes a runtime attempt.
func (e Event) IsTerminal() bool {
	switch e.Kind {
	case EventResponseFinished, EventRuntimeFailed, EventUsageFinal, EventStreamCancelled:
		return true
	default:
		return false
	}
}

// TerminalEvent is a package-level predicate for callers that work with
// values rather than a collector.
func TerminalEvent(event Event) bool { return event.IsTerminal() }

// TerminalCollector accepts one and only one terminal event for an attempt.
// It stores a copy so callers cannot mutate the recorded outcome afterwards.
type TerminalCollector struct {
	mu       sync.Mutex
	recorded *Event
}

// TerminalRecorder is an alias kept for callers that use recorder terminology.
type TerminalRecorder = TerminalCollector

func NewTerminalCollector() *TerminalCollector { return &TerminalCollector{} }

func NewTerminalRecorder() *TerminalRecorder { return NewTerminalCollector() }

func (c *TerminalCollector) RecordTerminal(event Event) error {
	if c == nil {
		return ErrTerminalAlreadyRecorded
	}
	if !event.IsTerminal() {
		return ErrNotTerminalEvent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recorded != nil {
		return ErrTerminalAlreadyRecorded
	}
	clone := event.Clone()
	c.recorded = &clone
	return nil
}

// RecordTerminal is also exposed as a free function for small adapters that
// do not need to name the collector method in their own contract code.
func RecordTerminal(collector *TerminalCollector, event Event) error {
	if collector == nil {
		return ErrTerminalAlreadyRecorded
	}
	return collector.RecordTerminal(event)
}

func (c *TerminalCollector) TerminalEvent() *Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recorded == nil {
		return nil
	}
	clone := c.recorded.Clone()
	return &clone
}

func (c *TerminalCollector) Recorded() bool { return c.TerminalEvent() != nil }
