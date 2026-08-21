//go:build unit

package runtimebridge

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type exchangeStub struct{}

func (exchangeStub) Request() *http.Request    { return nil }
func (exchangeStub) Header() http.Header       { return make(http.Header) }
func (exchangeStub) WriteHeader(int)           {}
func (exchangeStub) Write([]byte) (int, error) { return 0, nil }
func (exchangeStub) Flush()                    {}
func (exchangeStub) Written() bool             { return false }
func (exchangeStub) Size() int                 { return 0 }
func (exchangeStub) SetState(string, any)      {}
func (exchangeStub) State(string) (any, bool)  { return nil, false }

func TestLocalExchangeContextRoundTrip(t *testing.T) {
	exchange := exchangeStub{}
	ctx := WithLocalExchange(context.Background(), exchange)
	got, ok := LocalExchangeFromContext(ctx)
	if !ok || got != exchange {
		t.Fatalf("LocalExchangeFromContext() = %v/%v, want original exchange/true", got, ok)
	}
}

func TestLocalRuntimePassesExchangeToDriver(t *testing.T) {
	var got gatewayruntime.HTTPExchange
	runtime := NewLocalRuntime(driverFunc(func(ctx context.Context, _ v1.Request, sink EventSink) (v1.Result, error) {
		got, _ = LocalExchangeFromContext(ctx)
		_ = sink.Publish(ctx, v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: &v1.UsageFacts{AccountID: 7, TerminalStatus: "success"}})
		return v1.Result{StatusCode: 200, AccountID: 7}, nil
	}))
	exchange := exchangeStub{}
	_, err := runtime.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:  "request-exchange",
		PlatformID: 42,
		Endpoint:   gatewayruntime.EndpointResponses,
		Exchange:   exchange,
	}, &usageSinkStub{})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got != exchange {
		t.Fatalf("driver exchange = %v, want original exchange", got)
	}
}

type driverFunc func(context.Context, v1.Request, EventSink) (v1.Result, error)

func (f driverFunc) Dispatch(ctx context.Context, request v1.Request, sink EventSink) (v1.Result, error) {
	return f(ctx, request, sink)
}
