//go:build unit

package runtimebridge

import (
	"context"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

func TestLocalRuntimeConformanceMatchesDeterministicDriverMatrix(t *testing.T) {
	endpoints := []v1.Endpoint{v1.EndpointChatCompletions, v1.EndpointResponses, v1.EndpointMessages}
	for _, endpoint := range endpoints {
		t.Run(string(endpoint), func(t *testing.T) {
			request := gatewayruntime.Request{RequestID: "matrix-" + string(endpoint), PlatformID: 42, Adapter: "openai", Endpoint: gatewayruntime.Endpoint(endpoint), RequestedModel: "gpt-test", UpstreamModel: "gpt-upstream"}
			newDriver := func() *LocalRuntime {
				return NewLocalRuntime(driverFunc(func(_ context.Context, request v1.Request, sink EventSink) (v1.Result, error) {
					facts := &v1.UsageFacts{AccountID: 202, PlatformID: request.Platform.ID, Endpoint: string(request.Endpoint), RequestedModel: request.Platform.RequestedModel, UpstreamModel: request.Platform.UpstreamModel, InputTokens: 4, OutputTokens: 2, TerminalStatus: "success"}
					if err := sink.Publish(context.Background(), v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: facts}); err != nil {
						return v1.Result{}, err
					}
					return v1.Result{StatusCode: 200, AccountID: facts.AccountID, UpstreamModel: facts.UpstreamModel, Usage: *facts}, nil
				}))
			}
			leftSink := &conformanceSink{}
			rightSink := &conformanceSink{}
			left, leftErr := newDriver().Dispatch(context.Background(), request, leftSink)
			right, rightErr := newDriver().Dispatch(context.Background(), request, rightSink)
			if leftErr != nil || rightErr != nil || !reflect.DeepEqual(left, right) || !reflect.DeepEqual(leftSink.events, rightSink.events) {
				t.Fatalf("conformance mismatch left=%#v/%#v right=%#v/%#v", left, leftErr, right, rightErr)
			}
		})
	}
}
