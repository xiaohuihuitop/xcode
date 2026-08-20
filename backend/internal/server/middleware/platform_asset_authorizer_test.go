//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type platformAssetAuthorizerStub struct {
	t                *testing.T
	expectedModel    string
	expectedEndpoint string
	resolution       *service.PlatformAssetResolution
	err              error
	calls            int
}

func (s *platformAssetAuthorizerStub) Resolve(
	_ context.Context,
	_ *service.APIKey,
	model, endpoint string,
	skipBilling bool,
) (*service.PlatformAssetResolution, error) {
	s.calls++
	if s.expectedModel != "" {
		require.Equal(s.t, s.expectedModel, model)
	}
	if s.expectedEndpoint != "" {
		require.Equal(s.t, s.expectedEndpoint, endpoint)
	}
	require.False(s.t, skipBilling)
	return s.resolution, s.err
}

func TestPlatformAssetAuthorizationUsesFacadeAndKeepsSubscriptionContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	subscriptionID := int64(21)
	subscription := &service.UserSubscription{ID: subscriptionID}
	authorizer := &platformAssetAuthorizerStub{
		t:                t,
		expectedModel:    "gpt-4o",
		expectedEndpoint: "/v1/chat/completions",
		resolution: &service.PlatformAssetResolution{
			Decision: &productcore.Decision{
				Platform: productcore.Platform{ID: 3, AccountPlatform: service.PlatformOpenAI},
				BillingAsset: &productcore.BillingAsset{
					Source: "subscription", SubscriptionID: &subscriptionID, RateMultiplier: 0.5,
				},
			},
			Subscription: subscription,
		},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedPlatformIDs: []int64{3}})
		c.Next()
	})
	router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-4o"}`, string(body))
		route, ok := service.GatewayPlatformAssetContextFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, int64(3), route.Platform.PlatformID)
		gotSubscription, ok := GetSubscriptionFromContext(c)
		require.True(t, ok)
		require.Same(t, subscription, gotSubscription)
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, 1, authorizer.calls)
}

func TestPlatformAssetAuthorizationFacadeRejectsKeyWithoutPlatformGrant(t *testing.T) {
	authorizer := &platformAssetAuthorizerStub{t: t, err: service.ErrAPIKeyPlatformForbidden}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{})
		c.Next()
	})
	router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "API_KEY_PLATFORM_FORBIDDEN")
	require.Equal(t, 1, authorizer.calls)
}

func TestPlatformAssetAuthorizationKeepsEndpointErrorEnvelope(t *testing.T) {
	authorizer := &platformAssetAuthorizerStub{
		t:                t,
		expectedModel:    "gpt-4o",
		expectedEndpoint: "/v1/responses",
		err:              service.ErrPlatformEndpointUnsupported,
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedPlatformIDs: []int64{3}})
		c.Next()
	})
	router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
	router.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o"}`))
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "PLATFORM_ENDPOINT_UNSUPPORTED")
}
