package v1

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validContractRequest() Request {
	return Request{
		ContractVersion: ContractVersionV1,
		RequestID:       "req-contract-test",
		Platform: PlatformRoute{
			ID:                   42,
			Code:                 "openai",
			RuntimeAdapter:       "sub2api",
			RequestedModel:       "gpt-test",
			EndpointCapabilities: []string{"chat_completions"},
		},
		Endpoint:        EndpointChatCompletions,
		InboundEndpoint: "/v1/chat/completions",
		Payload:         []byte(`{"model":"gpt-test"}`),
		Headers:         map[string]string{"Content-Type": "application/json"},
	}
}

func TestRequestRejectsMissingIdentity(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Request)
	}{
		{name: "contract version", edit: func(r *Request) { r.ContractVersion = "v9" }},
		{name: "request id", edit: func(r *Request) { r.RequestID = "" }},
		{name: "platform id", edit: func(r *Request) { r.Platform.ID = 0 }},
		{name: "endpoint", edit: func(r *Request) { r.Endpoint = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validContractRequest()
			tt.edit(&req)
			if err := req.Validate(); err == nil {
				t.Fatalf("Validate() error = nil, want failure for %s", tt.name)
			}
		})
	}
}

func TestRequestClonesMutableFields(t *testing.T) {
	original := validContractRequest()
	clone := original.Clone()

	clone.Payload[0] = 'X'
	clone.Headers["Content-Type"] = "text/plain"
	clone.Headers["X-Test"] = "clone-only"
	clone.Platform.EndpointCapabilities[0] = "responses"
	clone.InboundEndpoint = "/v1/responses"

	if string(original.Payload) == string(clone.Payload) {
		t.Fatal("Clone() shares payload backing array")
	}
	if original.Headers["Content-Type"] == clone.Headers["Content-Type"] {
		t.Fatal("Clone() shares headers map")
	}
	if _, ok := original.Headers["X-Test"]; ok {
		t.Fatal("Clone() added a header to the original request")
	}
	if original.Platform.EndpointCapabilities[0] == clone.Platform.EndpointCapabilities[0] {
		t.Fatal("Clone() shares endpoint capability storage")
	}
	if original.InboundEndpoint == clone.InboundEndpoint {
		t.Fatal("Clone() changed scalar inbound endpoint unexpectedly")
	}
}

func TestTerminalEventAllowsExactlyOneFinalEvent(t *testing.T) {
	collector := NewTerminalCollector()
	first := Event{
		Sequence: 1,
		Kind:     EventUsageFinal,
		Usage:    &UsageFacts{AccountID: 7, TerminalStatus: "success"},
	}
	if err := collector.RecordTerminal(first); err != nil {
		t.Fatalf("RecordTerminal(first) error = %v", err)
	}
	if got := collector.TerminalEvent(); got == nil || got.Sequence != first.Sequence {
		t.Fatalf("TerminalEvent() = %#v, want first terminal event", got)
	}

	second := Event{Sequence: 2, Kind: EventRuntimeFailed, Error: &RuntimeError{Category: "upstream_5xx"}}
	err := collector.RecordTerminal(second)
	if !errors.Is(err, ErrTerminalAlreadyRecorded) {
		t.Fatalf("RecordTerminal(second) error = %v, want ErrTerminalAlreadyRecorded", err)
	}
}

func TestRuntimeErrorNeverSerializesCredentialFields(t *testing.T) {
	err := RuntimeError{
		Category: "credential_invalid",
		Message:  "Authorization: Bearer super-secret access_token=do-not-leak",
	}
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"super-secret", "do-not-leak", "Authorization", "Bearer", "access_token"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("serialized RuntimeError contains forbidden credential marker %q: %s", forbidden, serialized)
		}
	}
}

func TestUsageFactsRetainPricingAndAttributionFields(t *testing.T) {
	original := UsageFacts{
		Adapter:               "openai",
		UpstreamEndpoint:      "https://upstream.test/v1/responses",
		ServiceTier:           "priority",
		ReasoningEffort:       "high",
		BillingModel:          "gpt-billing",
		OriginalModel:         "gpt-client",
		MappedModel:           "gpt-upstream",
		BillingModelSource:    "platform",
		ModelMappingChain:     "gpt-client->gpt-upstream",
		ForceCacheBilling:     true,
		CyberBlocked:          false,
		LongContextThreshold:  128000,
		LongContextMultiplier: 1.2,
		InboundEndpoint:       "/v1/responses",
		UserAgent:             "test-client",
		ClientIP:              "192.0.2.1",
		SessionID:             "session-1",
		RequestPayloadHash:    "hash-1",
	}
	clone := Event{Kind: EventUsageFinal, Usage: &original}.Clone()
	if clone.Usage == nil {
		t.Fatal("Clone() lost usage facts")
	}
	if clone.Usage.Adapter != original.Adapter || clone.Usage.UpstreamEndpoint != original.UpstreamEndpoint || clone.Usage.BillingModel != original.BillingModel {
		t.Fatalf("clone lost pricing fields: %#v", clone.Usage)
	}
	if clone.Usage.ModelMappingChain != original.ModelMappingChain || clone.Usage.LongContextMultiplier != original.LongContextMultiplier || clone.Usage.RequestPayloadHash != original.RequestPayloadHash {
		t.Fatalf("clone lost attribution fields: %#v", clone.Usage)
	}
}
