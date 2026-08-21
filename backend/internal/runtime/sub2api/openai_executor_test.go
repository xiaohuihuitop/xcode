//go:build unit

package sub2api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type openAIExecutorEventSink struct {
	events []v1.Event
}

func (s *openAIExecutorEventSink) Publish(_ context.Context, event v1.Event) error {
	s.events = append(s.events, event)
	return nil
}

type openAIExecutorPort struct {
	selections []AccountSelection
	selected   int
	selectErr  error
	reports    []bool
	switches   int
	capability []string
}

func (p *openAIExecutorPort) Select(_ context.Context, _ v1.Request, _ map[int64]struct{}, capability string) (AccountSelection, error) {
	p.capability = append(p.capability, capability)
	if p.selectErr != nil {
		return AccountSelection{}, p.selectErr
	}
	if p.selected >= len(p.selections) {
		return AccountSelection{}, errors.New("no more accounts")
	}
	selection := p.selections[p.selected]
	p.selected++
	return selection, nil
}

func (p *openAIExecutorPort) MaxSwitches() int { return 2 }

func (p *openAIExecutorPort) ReportScheduleResult(_ context.Context, _ int64, _ string, success bool, _ *int) {
	p.reports = append(p.reports, success)
}

func (p *openAIExecutorPort) RecordAccountSwitch(context.Context) { p.switches++ }

type executorHTTPExchange struct {
	status  int
	headers http.Header
	body    []byte
	state   map[string]any
}

func (e *executorHTTPExchange) Request() *http.Request {
	return &http.Request{Header: e.Header()}
}

func (e *executorHTTPExchange) Header() http.Header {
	if e.headers == nil {
		e.headers = make(http.Header)
	}
	return e.headers
}

func (e *executorHTTPExchange) WriteHeader(status int) { e.status = status }

func (e *executorHTTPExchange) Write(body []byte) (int, error) {
	e.body = append(e.body, body...)
	return len(body), nil
}

func (e *executorHTTPExchange) Flush() {}

func (e *executorHTTPExchange) Written() bool { return e.status != 0 || len(e.body) > 0 }

func (e *executorHTTPExchange) Size() int { return len(e.body) }

func (e *executorHTTPExchange) SetState(key string, value any) {
	if e.state == nil {
		e.state = make(map[string]any)
	}
	e.state[key] = value
}

func (e *executorHTTPExchange) State(key string) (any, bool) {
	value, ok := e.state[key]
	return value, ok
}

func TestOpenAIExecutorFailsOverAndPublishesOneUsageEvent(t *testing.T) {
	port := &openAIExecutorPort{}
	port.selections = []AccountSelection{
		{ID: 101, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			return ForwardResult{}, &RetryError{StatusCode: http.StatusBadGateway, RetryNextAccount: true, ReportSchedule: true, Cause: errors.New("first upstream")}
		}},
		{ID: 202, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			return ForwardResult{StatusCode: http.StatusOK, UpstreamEndpoint: "/v1/chat/completions", Usage: v1.UsageFacts{AccountID: 202, InputTokens: 3, OutputTokens: 2, TerminalStatus: "success"}}, nil
		}},
	}
	sink := &openAIExecutorEventSink{}
	executor := OpenAIExecutor{Port: port}
	result, err := executor.Execute(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-openai-driver",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
		Endpoint:        v1.EndpointChatCompletions,
	}, sink)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.AccountID != 202 || result.StatusCode != http.StatusOK {
		t.Fatalf("result = %#v, want final account/status", result)
	}
	if len(sink.events) != 1 || sink.events[0].Kind != v1.EventUsageFinal {
		t.Fatalf("events = %#v, want exactly one usage_final", sink.events)
	}
	if sink.events[0].Usage == nil || sink.events[0].Usage.AccountID != 202 {
		t.Fatalf("usage = %#v, want account 202", sink.events[0].Usage)
	}
	if len(port.reports) != 2 || port.reports[0] || !port.reports[1] || port.switches != 1 {
		t.Fatalf("schedule reports/switches = %#v/%d, want failed+success and one switch", port.reports, port.switches)
	}
}

func TestOpenAIExecutorDoesNotFailoverAfterResponseWasWritten(t *testing.T) {
	exchange := &executorHTTPExchange{}
	calls := 0
	port := &openAIExecutorPort{selections: []AccountSelection{
		{ID: 101, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			calls++
			_, _ = exchange.Write([]byte("data: partial\n\n"))
			return ForwardResult{}, &RetryError{StatusCode: http.StatusBadGateway, RetryNextAccount: true, Cause: errors.New("stream failed")}
		}},
		{ID: 202, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			calls++
			return ForwardResult{StatusCode: http.StatusOK, Usage: v1.UsageFacts{AccountID: 202}}, nil
		}},
	}}
	sink := &openAIExecutorEventSink{}
	ctx := runtimebridge.WithLocalExchange(context.Background(), exchange)
	result, err := (OpenAIExecutor{Port: port}).Execute(ctx, v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-written-response",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
		Endpoint:        v1.EndpointChatCompletions,
	}, sink)
	if err == nil || result.StatusCode != http.StatusBadGateway || calls != 1 {
		t.Fatalf("result/error/calls = %#v/%v/%d, want one failed attempt without failover", result, err, calls)
	}
	if !strings.Contains(string(exchange.body), "data: partial") || len(sink.events) != 1 || sink.events[0].Kind != v1.EventRuntimeFailed {
		t.Fatalf("exchange/events = %q/%#v, want original partial body and one failed terminal", exchange.body, sink.events)
	}
}

func TestOpenAIExecutorUsesCountTokensNoAccountEnvelope(t *testing.T) {
	exchange := &executorHTTPExchange{}
	port := &openAIExecutorPort{selectErr: &RetryError{
		StatusCode:         http.StatusBadGateway,
		ClientErrorType:    "api_error",
		ClientErrorMessage: "No available accounts",
		Cause:              errors.New("no available accounts"),
	}}
	sink := &openAIExecutorEventSink{}
	ctx := runtimebridge.WithLocalExchange(context.Background(), exchange)
	_, _ = (OpenAIExecutor{Port: port}).Execute(ctx, v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-count-tokens-no-account",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "claude-test"},
		Endpoint:        v1.EndpointCountTokens,
	}, sink)
	body := string(exchange.body)
	if exchange.status != http.StatusBadGateway || !strings.Contains(body, `"type":"api_error"`) || !strings.Contains(body, "No available accounts") {
		t.Fatalf("status/body = %d/%s, want count_tokens api_error envelope", exchange.status, body)
	}
}

func TestOpenAIExecutorRetriesSameAccountBeforeSwitching(t *testing.T) {
	delays := 0
	port := &openAIExecutorPort{selections: []AccountSelection{
		{ID: 101, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			return ForwardResult{}, &RetryError{StatusCode: http.StatusBadGateway, RetryNextAccount: true, RetrySameAccount: true, Cause: errors.New("temporary")}
		}},
		{ID: 101, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			return ForwardResult{StatusCode: http.StatusOK, Usage: v1.UsageFacts{AccountID: 101}}, nil
		}},
	}}
	sink := &openAIExecutorEventSink{}
	result, err := (OpenAIExecutor{Port: port, retryDelay: func(context.Context) error {
		delays++
		return nil
	}}).Execute(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-same-account",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
		Endpoint:        v1.EndpointChatCompletions,
	}, sink)
	if err != nil || result.AccountID != 101 || len(sink.events) != 1 || sink.events[0].Kind != v1.EventUsageFinal || delays != 1 {
		t.Fatalf("same-account retry result/error/events/delays = %#v/%v/%#v/%d", result, err, sink.events, delays)
	}
}

func TestOpenAIExecutorRoutesEndpointCapabilityFromContract(t *testing.T) {
	for _, endpoint := range []struct {
		name, want string
	}{
		{name: "chat_completions", want: "chat_completions"},
		{name: "responses", want: "responses"},
		{name: "messages", want: "messages"},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			port := &openAIExecutorPort{selections: []AccountSelection{{ID: 1, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
				return ForwardResult{StatusCode: http.StatusOK, Usage: v1.UsageFacts{AccountID: 1}}, nil
			}}}}
			_, err := (OpenAIExecutor{Port: port}).Execute(context.Background(), v1.Request{
				ContractVersion: v1.ContractVersionV1,
				RequestID:       "capability-" + endpoint.name,
				Platform:        v1.PlatformRoute{ID: 1, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
				Endpoint:        v1.Endpoint(endpoint.name),
			}, &openAIExecutorEventSink{})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(port.capability) != 1 || port.capability[0] != endpoint.want {
				t.Fatalf("capability = %#v, want %q", port.capability, endpoint.want)
			}
		})
	}
}

func TestOpenAIExecutorFailureContainsAttemptedAccountIDs(t *testing.T) {
	port := &openAIExecutorPort{selections: []AccountSelection{
		{ID: 101, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
			return ForwardResult{}, &RetryError{StatusCode: http.StatusBadGateway, RetryNextAccount: false, Cause: errors.New("upstream failure")}
		}},
	}}
	sink := &openAIExecutorEventSink{}
	_, err := (OpenAIExecutor{Port: port}).Execute(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-failed-account",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
		Endpoint:        v1.EndpointResponses,
	}, sink)
	if err == nil || len(sink.events) != 1 || sink.events[0].Error == nil {
		t.Fatalf("error/events = %v/%#v, want one failed terminal", err, sink.events)
	}
	if len(sink.events[0].Error.AttemptedAccountIDs) != 1 || sink.events[0].Error.AttemptedAccountIDs[0] != 101 {
		t.Fatalf("attempted accounts = %#v, want [101]", sink.events[0].Error.AttemptedAccountIDs)
	}
}

func TestOpenAIExecutorPreservesResponseHeadersAndUsageFacts(t *testing.T) {
	port := &openAIExecutorPort{selections: []AccountSelection{{ID: 202, Forward: func(context.Context, v1.Request) (ForwardResult, error) {
		return ForwardResult{StatusCode: http.StatusOK, ResponseHeaders: map[string][]string{"X-Upstream": {"responses"}}, UpstreamEndpoint: "/v1/responses", Usage: v1.UsageFacts{AccountID: 202, CacheReadTokens: 8, FirstTokenMilliseconds: 11, DurationMilliseconds: 42}}, nil
	}}}}
	result, err := (OpenAIExecutor{Port: port}).Execute(context.Background(), v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-facts",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test", UpstreamModel: "gpt-upstream"},
		Endpoint:        v1.EndpointResponses,
	}, &openAIExecutorEventSink{})
	if err != nil || result.ResponseHeaders["X-Upstream"][0] != "responses" || result.Usage.CacheReadTokens != 8 || result.Usage.FirstTokenMilliseconds != 11 || result.Usage.DurationMilliseconds != 42 {
		t.Fatalf("result/error = %#v/%v, want headers and usage facts preserved", result, err)
	}
}

func TestOpenAIExecutorUsesClientCancelledStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	port := &openAIExecutorPort{}
	sink := &openAIExecutorEventSink{}
	result, err := (OpenAIExecutor{Port: port}).Execute(ctx, v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "req-cancelled",
		Platform:        v1.PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
		Endpoint:        v1.EndpointChatCompletions,
	}, sink)
	if err == nil || result.StatusCode != 499 || len(sink.events) != 1 || sink.events[0].Error == nil {
		t.Fatalf("cancelled result/error/events = %#v/%v/%#v, want 499 failed terminal", result, err, sink.events)
	}
	if sink.events[0].Error.Message != "Client closed request" {
		t.Fatalf("cancelled error message = %q, want safe client message", sink.events[0].Error.Message)
	}
}

func TestOpenAIExecutorRejectsMissingTerminalSink(t *testing.T) {
	executor := OpenAIExecutor{}
	_, err := executor.Execute(context.Background(), v1.Request{ContractVersion: v1.ContractVersionV1, RequestID: "req", Platform: v1.PlatformRoute{ID: 1}, Endpoint: v1.EndpointResponses}, nil)
	if !errors.Is(err, ErrExecutorUnavailable) {
		t.Fatalf("Execute() error = %v, want ErrExecutorUnavailable", err)
	}
}

func TestOpenAIExecutorDoesNotDependOnGinOrHandler(t *testing.T) {
	if _, ok := runtimebridge.LocalExchangeFromContext(context.Background()); ok {
		t.Fatal("empty context unexpectedly contains a local exchange")
	}
}
