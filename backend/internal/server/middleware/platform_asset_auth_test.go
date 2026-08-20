//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type platformAssetModelResolverStub struct {
	resolved *service.ResolvedPlatformModel
	err      error
}

type platformAssetSubscriptionRepoStub struct {
	service.UserSubscriptionRepository
	candidates []service.UserSubscription
}

type platformAssetAPIKeyRepoStub struct {
	service.APIKeyRepository
	getByKey func(context.Context, string) (*service.APIKey, error)
}

func (s *platformAssetAPIKeyRepoStub) GetByKey(ctx context.Context, key string) (*service.APIKey, error) {
	return s.getByKey(ctx, key)
}

func (s *platformAssetAPIKeyRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*service.APIKey, error) {
	return s.getByKey(ctx, key)
}

func (s *platformAssetAPIKeyRepoStub) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func (s *platformAssetSubscriptionRepoStub) ListActiveByUserIDAndPlanIDs(
	_ context.Context,
	_ int64,
	_ []int64,
) ([]service.UserSubscription, error) {
	return append([]service.UserSubscription(nil), s.candidates...), nil
}

func (s platformAssetModelResolverStub) ResolveModel(context.Context, string) (*service.ResolvedPlatformModel, error) {
	return s.resolved, s.err
}

func (s platformAssetModelResolverStub) ResolveModelCandidates(context.Context, string) ([]*service.ResolvedPlatformModel, error) {
	if s.resolved == nil {
		return nil, s.err
	}
	return []*service.ResolvedPlatformModel{s.resolved}, s.err
}

func TestPlatformAssetAuthorizationBuildsExplicitPlatformRouteAndPreservesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10}
	apiKey := &service.APIKey{
		ID:                 10,
		UserID:             user.ID,
		Key:                "platform-key",
		Status:             service.StatusActive,
		User:               user,
		AllowedPlatformIDs: []int64{3},
		AllowBalance:       true,
	}
	apiKeyService := service.NewAPIKeyService(&platformAssetAPIKeyRepoStub{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}, nil, nil, nil, cfg)
	resolver := platformAssetModelResolverStub{resolved: &service.ResolvedPlatformModel{
		PlatformID:           3,
		PlatformCode:         "gpt",
		AccountPlatform:      service.PlatformOpenAI,
		RequestedModel:       "gpt-4o",
		UpstreamModel:        "gpt-4o-2024-08-06",
		EndpointCapabilities: []string{string(service.OpenAIEndpointCapabilityChatCompletions)},
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	router.Use(NewPlatformAssetAuthorizationMiddleware(
		service.NewPlatformAssetProductCoreAdapter(apiKeyService, nil, resolver), cfg,
	))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		route, ok := service.GatewayPlatformAssetContextFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, int64(3), route.Platform.PlatformID)
		require.Equal(t, service.BillingSourceBalance, route.BillingAsset.Source)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-4o"}`, string(body))
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestPlatformAssetRequestCarriesModelForLiveCreateOnly(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(`{"model":"gpt-5.6"}`))
	require.True(t, platformAssetRequestCarriesModel(c))

	c.Request = httptest.NewRequest(http.MethodGet, "/v1/live/call-1", nil)
	require.False(t, platformAssetRequestCarriesModel(c))
}

func TestPlatformAssetAuthorizationRejectsKeyWithoutPlatformGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10}
	apiKey := &service.APIKey{
		ID: 10, UserID: user.ID, Key: "legacy-key", Status: service.StatusActive,
		User: user, AllowBalance: true,
	}
	apiKeyService := service.NewAPIKeyService(&platformAssetAPIKeyRepoStub{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}, nil, nil, nil, cfg)
	resolver := platformAssetModelResolverStub{resolved: &service.ResolvedPlatformModel{
		PlatformID: 3, AccountPlatform: service.PlatformOpenAI,
		EndpointCapabilities: []string{string(service.OpenAIEndpointCapabilityChatCompletions)},
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	router.Use(NewPlatformAssetAuthorizationMiddleware(
		service.NewPlatformAssetProductCoreAdapter(apiKeyService, nil, resolver), cfg,
	))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "API_KEY_PLATFORM_REQUIRED")
}

func TestPlatformAssetAuthorizationUsesAuthorizedSubscriptionWithoutLegacyBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
	planID := int64(17)
	apiKey := &service.APIKey{
		ID:                         10,
		UserID:                     user.ID,
		Key:                        "subscription-platform-key",
		Status:                     service.StatusActive,
		User:                       user,
		AllowedPlatformIDs:         []int64{3},
		AllowedSubscriptionPlanIDs: []int64{planID},
		AllowBalance:               false,
	}
	apiKeyService := service.NewAPIKeyService(&platformAssetAPIKeyRepoStub{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}, nil, nil, nil, cfg)
	now := time.Now()
	subscriptionService := service.NewSubscriptionService(&platformAssetSubscriptionRepoStub{
		candidates: []service.UserSubscription{{
			ID:                 21,
			UserID:             user.ID,
			SubscriptionPlanID: &planID,
			Status:             service.SubscriptionStatusActive,
			ExpiresAt:          now.Add(time.Hour),
			DailyWindowStart:   &now,
		}},
	}, nil, nil, cfg)
	t.Cleanup(subscriptionService.Stop)
	resolver := platformAssetModelResolverStub{resolved: &service.ResolvedPlatformModel{
		PlatformID:           3,
		PlatformCode:         "gpt",
		AccountPlatform:      service.PlatformOpenAI,
		EndpointCapabilities: []string{string(service.OpenAIEndpointCapabilityChatCompletions)},
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, subscriptionService, cfg)))
	router.Use(NewPlatformAssetAuthorizationMiddleware(
		service.NewPlatformAssetProductCoreAdapter(apiKeyService, subscriptionService, resolver), cfg,
	))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		subscription, ok := GetSubscriptionFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(21), subscription.ID)
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestGooglePlatformAssetAuthorizationResolvesModelFromPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 10}
	apiKey := &service.APIKey{
		ID:                 10,
		UserID:             user.ID,
		Key:                "google-platform-key",
		Status:             service.StatusActive,
		User:               user,
		AllowedPlatformIDs: []int64{4},
		AllowBalance:       true,
	}
	apiKeyService := service.NewAPIKeyService(&platformAssetAPIKeyRepoStub{
		getByKey: func(_ context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}, nil, nil, nil, cfg)
	resolver := platformAssetModelResolverStub{resolved: &service.ResolvedPlatformModel{
		PlatformID:      4,
		PlatformCode:    "glm",
		AccountPlatform: service.PlatformGemini,
		RequestedModel:  "gemini-2.5-pro",
	}}

	router := gin.New()
	router.Use(gin.HandlerFunc(APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, cfg)))
	router.Use(NewPlatformAssetAuthorizationGoogleMiddleware(
		service.NewPlatformAssetProductCoreAdapter(apiKeyService, nil, resolver), cfg,
	))
	router.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
		route, ok := service.GatewayPlatformAssetContextFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, int64(4), route.Platform.PlatformID)
		require.Equal(t, service.PlatformGemini, route.SchedulingScope.AccountPlatform)
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}
