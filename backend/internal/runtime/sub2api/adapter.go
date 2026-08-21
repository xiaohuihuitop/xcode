package sub2api

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

var (
	ErrAdapterUnavailable  = errors.New("sub2api driver adapter is unavailable")
	ErrEndpointUnavailable = errors.New("sub2api driver endpoint is unavailable")
	ErrTerminalMissing     = errors.New("sub2api driver terminal event is missing")
)

type Adapter struct {
	executors map[v1.Endpoint]EndpointExecutor
}

func (a *Adapter) Dispatch(
	ctx context.Context,
	request v1.Request,
	sink runtimebridge.EventSink,
) (v1.Result, error) {
	if a == nil {
		return v1.Result{}, ErrAdapterUnavailable
	}
	executor, ok := a.executors[request.Endpoint]
	if !ok || executor == nil {
		return v1.Result{}, ErrEndpointUnavailable
	}
	if sink == nil {
		return v1.Result{}, runtimebridge.ErrLocalRuntimeUnavailable
	}
	if err := request.Validate(); err != nil {
		return v1.Result{}, err
	}
	terminal := &terminalEventSink{sink: sink, collector: v1.NewTerminalCollector()}
	result, err := executor.Execute(ctx, request, terminal)
	if !terminal.recorded() {
		if err != nil {
			return result, errors.Join(err, ErrTerminalMissing)
		}
		return result, ErrTerminalMissing
	}
	return result, err
}

type terminalEventSink struct {
	sink      runtimebridge.EventSink
	collector *v1.TerminalCollector
}

func (s *terminalEventSink) Publish(ctx context.Context, event v1.Event) error {
	if s == nil || s.sink == nil || s.collector == nil {
		return ErrAdapterUnavailable
	}
	if event.IsTerminal() {
		if err := s.collector.RecordTerminal(event); err != nil {
			return err
		}
	}
	return s.sink.Publish(ctx, event)
}

func (s *terminalEventSink) recorded() bool {
	return s != nil && s.collector != nil && s.collector.Recorded()
}

var _ runtimebridge.Driver = (*Adapter)(nil)
