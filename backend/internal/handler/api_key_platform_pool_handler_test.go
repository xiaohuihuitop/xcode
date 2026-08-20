//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type apiKeyPlatformPoolListerStub struct {
	platforms []service.Platform
}

func (s apiKeyPlatformPoolListerStub) List(context.Context) ([]service.Platform, error) {
	return append([]service.Platform(nil), s.platforms...), nil
}

func TestAPIKeyHandlerAvailablePlatformsReturnsOnlyActivePoolMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAPIKeyHandler(nil, apiKeyPlatformPoolListerStub{platforms: []service.Platform{
		{ID: 11, Code: "openai-primary", Name: "OpenAI Primary", AccountPlatform: service.PlatformOpenAI, Status: service.PlatformStatusActive},
		{ID: 12, Code: "grok-paused", Name: "Grok Paused", AccountPlatform: service.PlatformGrok, Status: service.StatusDisabled},
	},
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	router.GET("/api/v1/platforms/available", handler.GetAvailablePlatforms)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/platforms/available", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":[{"id":11,"code":"openai-primary","name":"OpenAI Primary","account_platform":"openai"}]}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "model_rules")
	require.NotContains(t, recorder.Body.String(), "legacy_group_id")
}

func TestAPIKeyCreateContractDoesNotExposeLegacyGroupFields(t *testing.T) {
	req := CreateAPIKeyRequest{
		Name:                "platform-key",
		PlatformIDs:         []int64{7},
		SubscriptionPlanIDs: []int64{11, 12},
		AllowBalance:        apiKeyBoolPtr(true),
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)
	require.NotContains(t, string(body), `"group_id"`)
	require.NotContains(t, string(body), `"group_ids"`)

	var decoded CreateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"ignored","platform_ids":[7],"group_id":99,"group_ids":[99]}`), &decoded))
	require.Equal(t, []int64{7}, decoded.PlatformIDs)
	require.Equal(t, "ignored", decoded.Name)
}

func apiKeyBoolPtr(value bool) *bool { return &value }
