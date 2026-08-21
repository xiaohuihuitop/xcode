package runtimebridge

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

func (r *LocalRuntime) Dispatch(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
	if r == nil || r.driver == nil {
		return gatewayruntime.Result{}, ErrLocalRuntimeUnavailable
	}
	if sink == nil {
		return gatewayruntime.Result{}, gatewayruntime.ErrUsageSinkUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = WithLocalExchange(ctx, request.Exchange)
	contractRequest := requestFromRuntime(request)
	if err := contractRequest.Validate(); err != nil {
		return gatewayruntime.Result{}, err
	}
	events := &localEventSink{
		request:   request,
		recorder:  gatewayruntime.NewTerminalRecorder(sink),
		collector: v1.NewTerminalCollector(),
	}
	result, err := r.driver.Dispatch(ctx, contractRequest, events)
	if !events.recorded() {
		if err != nil {
			return fromV1Result(request, result), errors.Join(err, ErrLocalRuntimeTerminalMissing)
		}
		return fromV1Result(request, result), ErrLocalRuntimeTerminalMissing
	}
	return fromV1Result(request, result), err
}

func fromV1Result(request gatewayruntime.Request, result v1.Result) gatewayruntime.Result {
	responseHeaders := make(http.Header, len(result.ResponseHeaders))
	for key, values := range result.ResponseHeaders {
		responseHeaders[key] = append([]string(nil), values...)
	}
	facts := usageFactsFromV1(request, result.Usage)
	return gatewayruntime.Result{
		StatusCode:       result.StatusCode,
		AccountID:        result.AccountID,
		UpstreamEndpoint: result.UpstreamEndpoint,
		UpstreamModel:    result.UpstreamModel,
		Response: gatewayruntime.Response{
			Header:   responseHeaders,
			Body:     append([]byte(nil), result.Body...),
			Streamed: result.Streamed,
		},
		Usage: facts,
	}
}
