//go:build unit

package runtimebridge

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type driverStub struct {
	dispatch func(context.Context, v1.Request, EventSink) (v1.Result, error)
}

func (s driverStub) Dispatch(ctx context.Context, request v1.Request, sink EventSink) (v1.Result, error) {
	return s.dispatch(ctx, request, sink)
}

type usageSinkStub struct {
	events []gatewayruntime.UsageEvent
}

func (s *usageSinkStub) RecordFinal(_ context.Context, event gatewayruntime.UsageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestRequestFromDecisionCopiesRouteWithoutBillingAsset(t *testing.T) {
	planID := int64(9)
	request := gatewayruntime.Request{
		RequestID:       "request-route",
		Endpoint:        gatewayruntime.EndpointResponses,
		InboundEndpoint: "/v1/responses",
		Payload:         []byte(`{"model":"gpt-test"}`),
	}
	decision := productcore.Decision{
		Platform: productcore.Platform{
			ID:                   42,
			Code:                 "openai-main",
			AccountPlatform:      "openai",
			RequestedModel:       "gpt-test",
			UpstreamModel:        "gpt-test-upstream",
			EndpointCapabilities: []string{"responses"},
		},
		BillingAsset: &productcore.BillingAsset{
			Source:         "subscription",
			SubscriptionID: &planID,
			RateMultiplier: 1.5,
		},
	}

	got := RequestFromDecision(request, decision)
	if got.Platform.ID != 42 || got.Platform.Code != "openai-main" {
		t.Fatalf("route = %#v, want platform route copied", got.Platform)
	}
	if got.Platform.RequestedModel != "gpt-test" || got.Platform.UpstreamModel != "gpt-test-upstream" {
		t.Fatalf("models = %#v, want decision models", got.Platform)
	}
	if got.Owner.APIKeyID != 0 || got.Owner.UserID != 0 {
		t.Fatalf("route conversion must not invent owner IDs: %#v", got.Owner)
	}
	if got.Payload[0] != '{' {
		t.Fatalf("payload was not copied: %q", got.Payload)
	}
}

func TestLocalRuntimePublishesOneTerminalUsageEvent(t *testing.T) {
	sink := &usageSinkStub{}
	runtime := NewLocalRuntime(driverStub{dispatch: func(_ context.Context, request v1.Request, eventSink EventSink) (v1.Result, error) {
		if request.RequestID != "request-success" {
			t.Fatalf("request id = %q, want request-success", request.RequestID)
		}
		if err := eventSink.Publish(context.Background(), v1.Event{
			Sequence: 1,
			Kind:     v1.EventUsageFinal,
			Usage: &v1.UsageFacts{
				AccountID:              7,
				PlatformID:             42,
				Endpoint:               string(v1.EndpointResponses),
				InputTokens:            3,
				OutputTokens:           5,
				FirstTokenMilliseconds: 12,
				DurationMilliseconds:   34,
				TerminalStatus:         "success",
			},
		}); err != nil {
			return v1.Result{}, err
		}
		return v1.Result{StatusCode: 200, AccountID: 7, UpstreamModel: request.Platform.UpstreamModel}, nil
	}})

	result, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:       "request-success",
		PlatformID:      42,
		Endpoint:        gatewayruntime.EndpointResponses,
		Adapter:         "openai",
		RequestedModel:  "gpt-test",
		UpstreamModel:   "gpt-test-upstream",
		InboundEndpoint: "/v1/responses",
	}, sink)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.AccountID != 7 || result.StatusCode != 200 {
		t.Fatalf("result = %#v, want account 7/status 200", result)
	}
	if len(sink.events) != 1 || !sink.events[0].Success {
		t.Fatalf("usage events = %#v, want one successful event", sink.events)
	}
	if sink.events[0].Facts.DurationMilliseconds != 34 || sink.events[0].Facts.FirstTokenMilliseconds != 12 {
		t.Fatalf("latency facts = %#v, want duration 34/first token 12", sink.events[0].Facts)
	}
}

func TestLocalRuntimeRejectsMissingTerminalEvent(t *testing.T) {
	runtime := NewLocalRuntime(driverStub{dispatch: func(context.Context, v1.Request, EventSink) (v1.Result, error) {
		return v1.Result{StatusCode: 200}, nil
	}})

	_, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:  "request-missing-terminal",
		PlatformID: 42,
		Endpoint:   gatewayruntime.EndpointResponses,
	}, &usageSinkStub{})
	if !errors.Is(err, ErrLocalRuntimeTerminalMissing) {
		t.Fatalf("Dispatch() error = %v, want ErrLocalRuntimeTerminalMissing", err)
	}
}

func TestLocalRuntimePreservesDriverErrorAfterTerminalEvent(t *testing.T) {
	wantErr := errors.New("driver returned after terminal")
	sink := &usageSinkStub{}
	runtime := NewLocalRuntime(driverStub{dispatch: func(_ context.Context, _ v1.Request, eventSink EventSink) (v1.Result, error) {
		if err := eventSink.Publish(context.Background(), v1.Event{
			Sequence: 1,
			Kind:     v1.EventRuntimeFailed,
			Error:    &v1.RuntimeError{Category: "upstream_5xx", Message: "upstream failed", Retryable: true},
		}); err != nil {
			return v1.Result{}, err
		}
		return v1.Result{StatusCode: 502}, wantErr
	}})

	result, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:  "request-driver-error",
		PlatformID: 42,
		Endpoint:   gatewayruntime.EndpointResponses,
	}, sink)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch() error = %v, want driver error", err)
	}
	if result.StatusCode != 502 || len(sink.events) != 1 || sink.events[0].Success {
		t.Fatalf("result/events = %#v/%#v, want failed terminal", result, sink.events)
	}
}
