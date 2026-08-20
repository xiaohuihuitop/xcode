//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAvailablePlatformHandlerRequiresUser(t *testing.T) {
	h := NewAvailablePlatformHandler(nil)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/platforms/available", nil)
	h.List(c)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAvailablePlatformHandlerUsesPlatformCatalog(t *testing.T) {
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 1, Code: "openai", Name: "OpenAI", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "gpt-*", Enabled: true}},
		}},
	}, nil)
	h := NewAvailablePlatformHandler(catalog)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/platforms/available", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	h.List(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"code":"openai"`)
	require.Contains(t, w.Body.String(), `"pattern":"gpt-*"`)
}

func (s platformCatalogPlatformRepoStubForHandler) Create(context.Context, *service.Platform) error {
	return nil
}
