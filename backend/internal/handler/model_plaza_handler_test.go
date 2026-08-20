//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaHandler_NilSettingServiceFailsClosed404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ModelPlazaHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/model-plaza", nil)

	h.Get(c)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestModelPlazaHandlerReturnsPlatformCatalogWithoutLegacyFields(t *testing.T) {
	settings := modelPlazaSettingsStub{runtime: service.ModelPlazaRuntime{Enabled: true}}
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 7, Code: "openai", Name: "OpenAI", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "gpt-*", Enabled: true}},
		}},
	}, nil)
	h := NewModelPlazaHandler(catalog, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/model-plaza", h.Get)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/model-plaza", nil))

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"platforms"`)
	require.NotContains(t, w.Body.String(), `"groups"`)
	require.NotContains(t, w.Body.String(), `"group_id"`)
	require.NotContains(t, w.Body.String(), `"channel_id"`)
}

type platformCatalogPlatformRepoStubForHandler struct {
	platforms []service.Platform
}

func (s platformCatalogPlatformRepoStubForHandler) List(_ context.Context) ([]service.Platform, error) {
	return append([]service.Platform(nil), s.platforms...), nil
}

type modelPlazaSettingsStub struct {
	runtime service.ModelPlazaRuntime
}

func (s modelPlazaSettingsStub) GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime {
	return s.runtime
}
