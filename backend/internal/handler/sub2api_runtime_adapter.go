package handler

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

var (
	ErrSub2APIRuntimeUnavailable         = errors.New("sub2api runtime adapter is unavailable")
	ErrSub2APIRuntimeEndpointUnavailable = errors.New("sub2api runtime endpoint is unavailable")
	ErrSub2APIRuntimeExchangeUnavailable = errors.New("sub2api runtime exchange is unavailable")
	ErrSub2APIRuntimeTerminalMissing     = errors.New("sub2api runtime terminal usage is missing")
)

type Sub2APIEndpointExecutor interface {
	Execute(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error)
}

type Sub2APIRuntimeAdapter struct {
	executors map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor
}

func NewSub2APIRuntimeAdapter(executors map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor) *Sub2APIRuntimeAdapter {
	cloned := make(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor, len(executors))
	for endpoint, executor := range executors {
		if executor != nil {
			cloned[endpoint] = executor
		}
	}
	return &Sub2APIRuntimeAdapter{executors: cloned}
}

func (a *Sub2APIRuntimeAdapter) Dispatch(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
	if a == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeUnavailable
	}
	executor, ok := a.executors[request.Endpoint]
	if !ok {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	if sink == nil {
		return gatewayruntime.Result{}, gatewayruntime.ErrUsageSinkUnavailable
	}
	recorder := gatewayruntime.NewTerminalRecorder(sink)
	result, err := executor.Execute(ctx, request, recorder)
	if !recorder.Recorded() {
		if err != nil {
			return result, errors.Join(err, ErrSub2APIRuntimeTerminalMissing)
		}
		return result, ErrSub2APIRuntimeTerminalMissing
	}
	return result, err
}
