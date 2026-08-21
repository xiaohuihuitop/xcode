//go:build unit

package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type legacyExchangeStub struct{}

func (legacyExchangeStub) Request() *http.Request    { return nil }
func (legacyExchangeStub) Header() http.Header       { return make(http.Header) }
func (legacyExchangeStub) WriteHeader(int)           {}
func (legacyExchangeStub) Write([]byte) (int, error) { return 0, nil }
func (legacyExchangeStub) Flush()                    {}
func (legacyExchangeStub) Written() bool             { return false }
func (legacyExchangeStub) Size() int                 { return 0 }
func (legacyExchangeStub) SetState(string, any)      {}
func (legacyExchangeStub) State(string) (any, bool)  { return nil, false }

func TestGatewayRequestFromContractPreservesExchangeAndRoute(t *testing.T) {
	exchange := legacyExchangeStub{}
	request := v1.Request{
		ContractVersion: v1.ContractVersionV1,
		RequestID:       "legacy-request",
		Platform: v1.PlatformRoute{
			ID:             42,
			Code:           "openai-main",
			RuntimeAdapter: "openai",
			RequestedModel: "gpt-test",
			UpstreamModel:  "gpt-test-upstream",
		},
		Endpoint:        v1.EndpointResponses,
		InboundEndpoint: "/v1/responses",
		Stream:          true,
		Payload:         []byte(`{"model":"gpt-test"}`),
		Owner:           v1.OwnerRef{UserID: 8, APIKeyID: 9},
		Session:         v1.SessionMetadata{SessionID: "session-1"},
	}

	got := gatewayRequestFromContract(request, exchange)
	if got.RequestID != request.RequestID || got.PlatformID != 42 || got.Adapter != "openai" {
		t.Fatalf("gateway request identity = %#v, want contract route copied", got)
	}
	if got.Exchange != exchange || got.InboundEndpoint != "/v1/responses" || !got.Stream {
		t.Fatalf("gateway request transport = %#v, want exchange/path/stream preserved", got)
	}
	if got.Metadata.APIKeyID != 9 || got.Metadata.UserID != 8 || got.Metadata.SessionID != "session-1" {
		t.Fatalf("gateway request owner metadata = %#v, want scalar owner copied", got.Metadata)
	}
}

func TestContractEventFromGatewayUsagePreservesFacts(t *testing.T) {
	event := gatewayruntime.UsageEvent{
		RequestID: "usage-request",
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			Adapter:                "openai",
			RequestedModel:         "gpt-test",
			UpstreamModel:          "gpt-upstream",
			AccountID:              7,
			UpstreamEndpoint:       "https://upstream.test/v1/responses",
			BillingModel:           "gpt-billing",
			InputTokens:            3,
			OutputTokens:           5,
			DurationMilliseconds:   34,
			FirstTokenMilliseconds: 12,
			TerminalStatus:         "success",
		},
	}

	converted := contractEventFromGatewayUsage(event)
	if converted.Kind != v1.EventUsageFinal || converted.Usage == nil {
		t.Fatalf("converted event = %#v, want usage_final with usage", converted)
	}
	if converted.Usage.AccountID != 7 || converted.Usage.InputTokens != 3 || converted.Usage.DurationMilliseconds != 34 || converted.Usage.FirstTokenMilliseconds != 12 {
		t.Fatalf("converted usage = %#v, want account/tokens/latency preserved", converted.Usage)
	}
	convertedFacts := usageFactsFromGateway(v1.Request{
		Endpoint: v1.EndpointResponses,
		Platform: v1.PlatformRoute{RuntimeAdapter: "openai"},
	}, event.Facts)
	if convertedFacts.Endpoint != string(v1.EndpointResponses) || convertedFacts.UpstreamEndpoint != event.Facts.UpstreamEndpoint || convertedFacts.BillingModel != event.Facts.BillingModel {
		t.Fatalf("converted endpoint facts = %#v, want capability and upstream endpoint", convertedFacts)
	}

}
