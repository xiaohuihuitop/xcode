package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type authorizedPlatformModelListerStub struct {
	models []string
	ids    []int64
}

type platformModelsResponseForTest struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func platformModelIDsForTest(models []struct {
	ID string `json:"id"`
}) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func (s *authorizedPlatformModelListerStub) ListAuthorizedModels(_ context.Context, ids []int64) ([]string, error) {
	s.ids = append([]int64(nil), ids...)
	return append([]string(nil), s.models...), nil
}

func TestGatewayModels_UsesAuthorizedPlatformCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lister := &authorizedPlatformModelListerStub{models: []string{"glm-4.6", "gpt-5.6"}}
	h := &GatewayHandler{platformModels: lister}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		AllowedPlatformIDs: []int64{7, 8},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{7, 8}, lister.ids)
	var got platformModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"glm-4.6", "gpt-5.6"}, platformModelIDsForTest(got.Data))
}
