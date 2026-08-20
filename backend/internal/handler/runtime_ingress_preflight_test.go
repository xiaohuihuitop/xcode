//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildRuntimeProductPreflightSpecClassifiesMigratedEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		request       gatewayruntime.Request
		wantProtocol  string
		wantAudit     bool
		wantUserSlot  bool
		wantBilling   bool
		wantImageSlot bool
		wantAuditBody string
	}{
		{
			name: "openai chat completions",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointChatCompletions,
				Adapter:        service.PlatformOpenAI,
				RequestedModel: "gpt-5.6",
				Payload:        []byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}],"stream":true}`),
			},
			wantProtocol: service.ContentModerationProtocolOpenAIChat, wantAudit: true, wantUserSlot: true, wantBilling: true,
			wantAuditBody: `{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}],"stream":true}`,
		},
		{
			name: "openai responses",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointResponses,
				Adapter:        service.PlatformOpenAI,
				RequestedModel: "gpt-5.6",
				Payload:        []byte(`{"model":"gpt-5.6","input":"hello"}`),
			},
			wantProtocol: service.ContentModerationProtocolOpenAIResponses, wantAudit: true, wantUserSlot: true, wantBilling: true,
			wantAuditBody: `{"model":"gpt-5.6","input":"hello"}`,
		},
		{
			name: "openai messages",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointMessages,
				Adapter:        service.PlatformOpenAI,
				RequestedModel: "gpt-5.6",
				Payload:        []byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
			},
			wantProtocol: service.ContentModerationProtocolAnthropicMessages, wantAudit: true, wantUserSlot: true, wantBilling: true,
			wantAuditBody: `{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`,
		},
		{
			name: "openai embeddings",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointEmbeddings,
				Adapter:        service.PlatformOpenAI,
				RequestedModel: "text-embedding-3-small",
				Payload:        []byte(`{"model":"text-embedding-3-small","input":"hello"}`),
			},
			wantProtocol: "openai_embeddings", wantAudit: true, wantUserSlot: true, wantBilling: true,
			wantAuditBody: `{"model":"text-embedding-3-small","input":"hello"}`,
		},
		{
			name: "openai images",
			request: gatewayruntime.Request{
				Endpoint:        gatewayruntime.EndpointImages,
				Adapter:         service.PlatformOpenAI,
				InboundEndpoint: "/v1/images/generations",
				Payload:         []byte(`{"model":"gpt-image-1","prompt":"draw a cat"}`),
				Metadata:        gatewayruntime.RequestMetadata{Headers: map[string]string{"Content-Type": "application/json"}},
			},
			wantProtocol: "openai_images", wantAudit: true, wantUserSlot: true, wantBilling: true, wantImageSlot: true,
			wantAuditBody: `{"prompt":"draw a cat"}`,
		},
		{
			name: "grok video lookup",
			request: gatewayruntime.Request{
				Endpoint:        gatewayruntime.EndpointVideos,
				Adapter:         service.PlatformGrok,
				InboundEndpoint: "/v1/videos/vid_123",
			},
			wantUserSlot: true, wantBilling: true,
		},
		{
			name: "grok count tokens",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointCountTokens,
				Adapter:        service.PlatformGrok,
				RequestedModel: "grok-4",
			},
		},
		{
			name: "openai count tokens",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointCountTokens,
				Adapter:        service.PlatformOpenAI,
				RequestedModel: "gpt-5.6",
				Payload:        []byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
			},
			wantBilling: true,
		},
		{
			name: "gemini native",
			request: gatewayruntime.Request{
				Endpoint:       gatewayruntime.EndpointGeminiNative,
				Adapter:        service.PlatformGemini,
				RequestedModel: "gemini-2.5-flash",
				Payload:        []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
			},
			wantProtocol: "gemini", wantAudit: true, wantUserSlot: true, wantBilling: true,
			wantAuditBody: `{"contents":[{"parts":[{"text":"hello"}]}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildRuntimeProductPreflightSpec(tt.request, &service.OpenAIGatewayService{})
			require.NoError(t, err)
			require.Equal(t, tt.wantProtocol, got.Protocol)
			require.Equal(t, tt.wantAudit, got.Audit)
			require.Equal(t, tt.wantUserSlot, got.UserSlot)
			require.Equal(t, tt.wantBilling, got.Billing)
			require.Equal(t, tt.wantImageSlot, got.ImageSlot)
			require.Equal(t, tt.wantAuditBody, string(got.AuditBody))
		})
	}
}

func TestBuildRuntimeProductPreflightSpecRejectsInvalidRequestsBeforeProductSlots(t *testing.T) {
	tests := []struct {
		name    string
		request gatewayruntime.Request
		wantErr string
	}{
		{
			name: "empty embeddings body",
			request: gatewayruntime.Request{
				Endpoint: gatewayruntime.EndpointEmbeddings,
				Adapter:  service.PlatformOpenAI,
			},
			wantErr: "Request body is empty",
		},
		{
			name: "missing grok video model",
			request: gatewayruntime.Request{
				Endpoint:        gatewayruntime.EndpointVideos,
				Adapter:         service.PlatformGrok,
				InboundEndpoint: "/v1/videos/generations",
				Payload:         []byte(`{"prompt":"hello"}`),
			},
			wantErr: "model is required",
		},
		{
			name: "missing grok video request id",
			request: gatewayruntime.Request{
				Endpoint:        gatewayruntime.EndpointVideos,
				Adapter:         service.PlatformGrok,
				InboundEndpoint: "/v1/videos/",
			},
			wantErr: "request_id is required",
		},
		{
			name: "empty gemini body",
			request: gatewayruntime.Request{
				Endpoint: gatewayruntime.EndpointGeminiNative,
				Adapter:  service.PlatformGemini,
			},
			wantErr: "Request body is empty",
		},
		{
			name: "image model on chat completions",
			request: gatewayruntime.Request{
				Endpoint: gatewayruntime.EndpointChatCompletions,
				Adapter:  service.PlatformOpenAI,
				Payload:  []byte(`{"model":"gpt-image-1","messages":[{"role":"user","content":"draw"}]}`),
			},
			wantErr: "This model is not supported on the Chat Completions endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRuntimeProductPreflightSpec(tt.request, &service.OpenAIGatewayService{})
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestRuntimeOpenAICyberBlockFormatMatchesProtocolEnvelope(t *testing.T) {
	tests := []struct {
		endpoint gatewayruntime.Endpoint
		want     cyberSessionBlockFormat
	}{
		{gatewayruntime.EndpointChatCompletions, cyberBlockFormatChat},
		{gatewayruntime.EndpointResponses, cyberBlockFormatResponses},
		{gatewayruntime.EndpointMessages, cyberBlockFormatAnthropic},
	}
	for _, tt := range tests {
		got, ok := runtimeOpenAICyberBlockFormat(tt.endpoint)
		require.True(t, ok)
		require.Equal(t, tt.want, got)
	}
	_, ok := runtimeOpenAICyberBlockFormat(gatewayruntime.EndpointImages)
	require.False(t, ok)
}
