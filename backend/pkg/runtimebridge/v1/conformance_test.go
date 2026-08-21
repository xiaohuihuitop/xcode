package v1

import "testing"

func TestContractRequestMatrixValidatesAcrossEndpoints(t *testing.T) {
	for _, endpoint := range []Endpoint{
		EndpointChatCompletions,
		EndpointResponses,
		EndpointMessages,
		EndpointImages,
		EndpointCountTokens,
	} {
		t.Run(string(endpoint), func(t *testing.T) {
			request := Request{
				ContractVersion: ContractVersionV1,
				RequestID:       "conformance-" + string(endpoint),
				Platform:        PlatformRoute{ID: 42, RuntimeAdapter: "openai", RequestedModel: "gpt-test"},
				Endpoint:        endpoint,
				Payload:         []byte(`{"model":"gpt-test"}`),
			}
			if err := request.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			clone := request.Clone()
			clone.Payload[0] = '['
			if request.Payload[0] != '{' {
				t.Fatal("Clone() did not isolate request payload")
			}
		})
	}
}
