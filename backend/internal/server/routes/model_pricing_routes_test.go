//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPricingStaticRoutesAreRegisteredBeforeIDRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		c.Header("X-Matched-Route", c.FullPath())
	})
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{ModelPricing: adminhandler.NewModelPricingHandler(
		service.NewModelPricingCatalog(nil),
		service.NewPlatformCatalogService(nil, nil),
		service.NewPlatformService(nil),
	)}}
	registerModelPricingRoutes(router.Group("/api/v1/admin"), handlers)

	tests := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/v1/admin/model-pricing/catalog", "/api/v1/admin/model-pricing/catalog"},
		{http.MethodPut, "/api/v1/admin/model-pricing/platform-sale", "/api/v1/admin/model-pricing/platform-sale"},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))
		require.Equal(t, tt.want, recorder.Header().Get("X-Matched-Route"))
	}
}
