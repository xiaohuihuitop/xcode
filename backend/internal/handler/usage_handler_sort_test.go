package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userUsageSortRepoCapture struct {
	service.UsageLogRepository
	listParams pagination.PaginationParams
}

func (s *userUsageSortRepoCapture) ListWithFilters(_ context.Context, params pagination.PaginationParams, _ usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	s.listParams = params
	return nil, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize}, nil
}

func newUserUsageSortRouter(repo *userUsageSortRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage", handler.List)
	return router
}

func TestUserUsageListSortParams(t *testing.T) {
	repo := &userUsageSortRepoCapture{}
	recorder := httptest.NewRecorder()
	newUserUsageSortRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/usage?sort_by=model&sort_order=ASC", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "model", repo.listParams.SortBy)
	require.Equal(t, "ASC", repo.listParams.SortOrder)
}

func TestUserUsageListSortDefaults(t *testing.T) {
	repo := &userUsageSortRepoCapture{}
	recorder := httptest.NewRecorder()
	newUserUsageSortRouter(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/usage", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "created_at", repo.listParams.SortBy)
	require.Equal(t, "desc", repo.listParams.SortOrder)
}
