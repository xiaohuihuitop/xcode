package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/productcore"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIWSPlatformAssetAuthorizerStub struct {
	resolution  *service.PlatformAssetResolution
	model       string
	endpoint    string
	skipBilling bool
}

func (s *openAIWSPlatformAssetAuthorizerStub) Resolve(
	_ context.Context,
	_ *service.APIKey,
	model string,
	endpoint string,
	skipBilling bool,
) (*service.PlatformAssetResolution, error) {
	s.model = model
	s.endpoint = endpoint
	s.skipBilling = skipBilling
	return s.resolution, nil
}

func TestResponsesWebSocketResolvesPlatformAssetAfterReadingFirstMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	subscriptionID := int64(20)
	planID := int64(1)
	authorizer := &openAIWSPlatformAssetAuthorizerStub{
		resolution: &service.PlatformAssetResolution{
			Decision: &productcore.Decision{
				Platform: productcore.Platform{
					ID:                   1,
					Code:                 "codex",
					AccountPlatform:      service.PlatformOpenAI,
					RequestedModel:       "gpt-5.6-sol",
					UpstreamModel:        "gpt-5.6-sol",
					EndpointCapabilities: []string{string(service.OpenAIEndpointCapabilityResponses)},
				},
				BillingAsset: &productcore.BillingAsset{
					Source:         service.BillingSourceSubscription,
					SubscriptionID: &subscriptionID,
					PlanID:         &planID,
					RateMultiplier: 1,
				},
			},
			Subscription: &service.UserSubscription{ID: subscriptionID},
		},
	}
	handler := &OpenAIGatewayHandler{}
	handler.SetPlatformAssetAuthorizer(authorizer)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	err := handler.resolveResponsesWebSocketPlatformAsset(c, &service.APIKey{ID: 26}, "gpt-5.6-sol")

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", authorizer.model)
	require.Equal(t, "/v1/responses", authorizer.endpoint)
	require.False(t, authorizer.skipBilling)
	require.Equal(t, int64(-2), *service.PlatformSchedulingID(c.Request.Context()))
	require.Equal(t, int64(1), *service.PlatformAssetID(c.Request.Context()))
	subscription, ok := middleware2.GetSubscriptionFromContext(c)
	require.True(t, ok)
	require.Equal(t, subscriptionID, subscription.ID)
}

func TestOpenAIWSNextAttemptMessageUsesCurrentTurnPayload(t *testing.T) {
	firstMessage := []byte(`{"type":"response.create","input":"first"}`)
	currentTurn := []byte(`{"type":"response.create","input":"turn-2"}`)

	next, ok := openAIWSNextAttemptMessage(firstMessage, currentTurn, true)

	require.True(t, ok)
	require.Equal(t, currentTurn, next)
	next[0] = 'x'
	require.Equal(t, byte('{'), currentTurn[0])
}

func TestOpenAIWSNextAttemptMessageRejectsMissingCurrentTurnPayload(t *testing.T) {
	next, ok := openAIWSNextAttemptMessage([]byte(`{"type":"response.create"}`), nil, true)

	require.False(t, ok)
	require.Nil(t, next)
}

func TestOpenAIWSNextAttemptMessageKeepsInitialMessageForFirstTurnFailover(t *testing.T) {
	firstMessage := []byte(`{"type":"response.create","input":"first"}`)

	next, ok := openAIWSNextAttemptMessage(firstMessage, nil, false)

	require.True(t, ok)
	require.Equal(t, firstMessage, next)
}
