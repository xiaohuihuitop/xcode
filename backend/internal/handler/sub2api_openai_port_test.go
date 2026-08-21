//go:build unit

package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

func TestSub2APIOpenAIPortConvertsFailoverPolicyWithoutLeakingServiceError(t *testing.T) {
	cause := &service.UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		RetryableOnSameAccount: true,
		NextAccountAction:      service.NextAccountRetry,
	}
	err := sub2APIRetryError(cause)
	if err == nil || !err.RetryNextAccount || !err.RetrySameAccount || err.StatusCode != http.StatusBadGateway {
		t.Fatalf("retry error = %#v, want policy-preserving driver error", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("retry error does not unwrap original service error")
	}
}

type usageExchangeStub struct {
	legacyExchangeStub
	size int
}

func (e usageExchangeStub) Size() int { return e.size }

func TestContractUsageFactsUsesLogicalEndpointAndWrittenState(t *testing.T) {
	got := contractUsageFacts(
		v1.Request{Endpoint: v1.EndpointResponses},
		gatewayruntime.UsageFacts{InboundEndpoint: "/v1/responses"},
		usageExchangeStub{size: 9},
	)
	if got.Endpoint != string(v1.EndpointResponses) || !got.ResponseWasPartiallySent {
		t.Fatalf("usage facts = %#v, want logical endpoint and partial response", got)
	}
}

func TestSub2APIOpenAIPortMapsRuntimeCapabilities(t *testing.T) {
	for _, item := range []struct {
		input string
		want  service.OpenAIEndpointCapability
	}{
		{input: "responses", want: service.OpenAIEndpointCapabilityResponses},
		{input: "embeddings", want: service.OpenAIEndpointCapabilityEmbeddings},
		{input: "alpha_search", want: service.OpenAIEndpointCapabilityAlphaSearch},
		{input: "messages", want: service.OpenAIEndpointCapabilityChatCompletions},
	} {
		if got := openAIEndpointCapability(item.input); got != item.want {
			t.Fatalf("openAIEndpointCapability(%q) = %q, want %q", item.input, got, item.want)
		}
	}
}
