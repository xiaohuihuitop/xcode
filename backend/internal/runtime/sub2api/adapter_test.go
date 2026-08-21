//go:build unit

package sub2api

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type executorStub struct {
	execute func(context.Context, v1.Request, runtimebridge.EventSink) (v1.Result, error)
}

func (s executorStub) Execute(ctx context.Context, request v1.Request, sink runtimebridge.EventSink) (v1.Result, error) {
	return s.execute(ctx, request, sink)
}

type eventSinkStub struct {
	events []v1.Event
}

func (s *eventSinkStub) Publish(_ context.Context, event v1.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestRegistryRejectsDuplicateUnknownAndNilExecutors(t *testing.T) {
	registry := NewRegistry()
	executor := executorStub{execute: func(context.Context, v1.Request, runtimebridge.EventSink) (v1.Result, error) {
		return v1.Result{}, nil
	}}

	if err := registry.Register(v1.EndpointResponses, executor); err != nil {
		t.Fatalf("Register(responses) error = %v", err)
	}
	if !errors.Is(registry.Register(v1.EndpointResponses, executor), ErrDuplicateEndpoint) {
		t.Fatalf("duplicate Register() error = %v", registry.Register(v1.EndpointResponses, executor))
	}
	if !errors.Is(registry.Register(v1.Endpoint("unknown"), executor), ErrInvalidEndpoint) {
		t.Fatalf("unknown Register() error = %v", registry.Register(v1.Endpoint("unknown"), executor))
	}
	if !errors.Is(registry.Register(v1.EndpointChatCompletions, nil), ErrInvalidExecutor) {
		t.Fatalf("nil Register() error = %v", registry.Register(v1.EndpointChatCompletions, nil))
	}
}

func TestAdapterRequiresExactlyOneTerminalEvent(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(v1.EndpointResponses, executorStub{execute: func(_ context.Context, request v1.Request, sink runtimebridge.EventSink) (v1.Result, error) {
		if err := sink.Publish(context.Background(), v1.Event{
			Sequence: 1,
			Kind:     v1.EventUsageFinal,
			Usage:    &v1.UsageFacts{AccountID: 7, TerminalStatus: "success"},
		}); err != nil {
			return v1.Result{}, err
		}
		return v1.Result{StatusCode: 200, AccountID: 7}, nil
	}}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	adapter, err := registry.Adapter()
	if err != nil {
		t.Fatalf("Adapter() error = %v", err)
	}
	events := &eventSinkStub{}
	result, err := adapter.Dispatch(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "request-adapter",
		Platform:        v1.PlatformRoute{ID: 42},
		Endpoint:        v1.EndpointResponses,
	}, events)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.AccountID != 7 || len(events.events) != 1 {
		t.Fatalf("result/events = %#v/%#v, want account 7 and one event", result, events.events)
	}
}

func TestAdapterRejectsExecutorWithoutTerminalEvent(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(v1.EndpointResponses, executorStub{execute: func(context.Context, v1.Request, runtimebridge.EventSink) (v1.Result, error) {
		return v1.Result{StatusCode: 200}, nil
	}}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	adapter, err := registry.Adapter()
	if err != nil {
		t.Fatalf("Adapter() error = %v", err)
	}
	_, err = adapter.Dispatch(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "request-no-terminal",
		Platform:        v1.PlatformRoute{ID: 42},
		Endpoint:        v1.EndpointResponses,
	}, &eventSinkStub{})
	if !errors.Is(err, ErrTerminalMissing) {
		t.Fatalf("Dispatch() error = %v, want ErrTerminalMissing", err)
	}
}

func TestRegistryRejectsEmptyAdapter(t *testing.T) {
	if _, err := NewRegistry().Adapter(); !errors.Is(err, ErrRegistryUnavailable) {
		t.Fatalf("Adapter(empty) error = %v, want ErrRegistryUnavailable", err)
	}
}

func TestDeferredEndpointFamiliesAreExplicit(t *testing.T) {
	want := map[v1.Endpoint]bool{
		v1.EndpointCountTokens:  true,
		v1.EndpointEmbeddings:   true,
		v1.EndpointAlphaSearch:  true,
		v1.EndpointGeminiNative: true,
		v1.EndpointImages:       true,
		v1.EndpointVideos:       true,
		v1.EndpointLive:         true,
		v1.EndpointWebSocket:    true,
	}
	got := make(map[v1.Endpoint]bool, len(DeferredEndpointFamilies))
	for _, endpoint := range DeferredEndpointFamilies {
		got[endpoint] = true
	}
	if len(got) != len(want) {
		t.Fatalf("deferred endpoint families = %#v, want %#v", got, want)
	}
	for endpoint := range want {
		if !got[endpoint] {
			t.Fatalf("deferred endpoint %q is missing", endpoint)
		}
	}
}
