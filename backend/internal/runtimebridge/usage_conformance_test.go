//go:build unit

package runtimebridge

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type conformanceSink struct {
	events []gatewayruntime.UsageEvent
	err    error
}

func (s *conformanceSink) RecordFinal(_ context.Context, event gatewayruntime.UsageEvent) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func TestLocalRuntimeFailedAttemptNeverBecomesSuccessfulUsage(t *testing.T) {
	sink := &conformanceSink{}
	runtime := NewLocalRuntime(driverFunc(func(_ context.Context, _ v1.Request, events EventSink) (v1.Result, error) {
		if err := events.Publish(context.Background(), v1.Event{Sequence: 1, Kind: v1.EventRuntimeFailed, Error: &v1.RuntimeError{Category: "upstream_5xx", Message: "failed"}}); err != nil {
			return v1.Result{}, err
		}
		return v1.Result{StatusCode: 502}, errors.New("failed")
	}))
	_, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{RequestID: "failed-attempt", PlatformID: 1, Endpoint: gatewayruntime.EndpointResponses}, sink)
	if err == nil || len(sink.events) != 1 || sink.events[0].Success {
		t.Fatalf("error/events = %v/%#v, want one failed usage event", err, sink.events)
	}
}

func TestLocalRuntimePreservesFinalAccountLatencyAndCacheFacts(t *testing.T) {
	sink := &conformanceSink{}
	runtime := NewLocalRuntime(driverFunc(func(_ context.Context, _ v1.Request, events EventSink) (v1.Result, error) {
		facts := &v1.UsageFacts{AccountID: 202, InputTokens: 10, OutputTokens: 4, CacheReadTokens: 8, FirstTokenMilliseconds: 11, DurationMilliseconds: 42, TerminalStatus: "success"}
		if err := events.Publish(context.Background(), v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: facts}); err != nil {
			return v1.Result{}, err
		}
		return v1.Result{StatusCode: 200, AccountID: 202, Usage: *facts}, nil
	}))
	result, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{RequestID: "final-facts", PlatformID: 1, Endpoint: gatewayruntime.EndpointResponses}, sink)
	if err != nil || result.AccountID != 202 || len(sink.events) != 1 {
		t.Fatalf("result/error/events = %#v/%v/%#v", result, err, sink.events)
	}
	facts := sink.events[0].Facts
	if facts.AccountID != 202 || facts.CacheReadTokens != 8 || facts.FirstTokenMilliseconds != 11 || facts.DurationMilliseconds != 42 {
		t.Fatalf("facts = %#v, want final account/cache/latency preserved", facts)
	}
}

func TestLocalRuntimeReturnsUsageSinkFailure(t *testing.T) {
	wantErr := errors.New("usage sink unavailable")
	sink := &conformanceSink{err: wantErr}
	runtime := NewLocalRuntime(driverFunc(func(_ context.Context, _ v1.Request, events EventSink) (v1.Result, error) {
		return v1.Result{}, events.Publish(context.Background(), v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: &v1.UsageFacts{AccountID: 1}})
	}))
	_, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{RequestID: "sink-failure", PlatformID: 1, Endpoint: gatewayruntime.EndpointResponses}, sink)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch() error = %v, want sink error", err)
	}
}
