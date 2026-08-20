//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type platformPoolAccountHandlerStub struct {
	service.AdminService
	created *service.CreateAccountInput
	updated *service.UpdateAccountInput
}

func (s *platformPoolAccountHandlerStub) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	s.created = input
	return &service.Account{ID: 71, Name: input.Name, Platform: input.Platform, PlatformID: input.PlatformID, Status: service.StatusActive}, nil
}

func (s *platformPoolAccountHandlerStub) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updated = input
	return &service.Account{ID: id, Name: input.Name, PlatformID: input.PlatformID, Status: service.StatusActive}, nil
}

func setupPlatformPoolAccountHandlerRouter(svc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", handler.Create)
	router.PUT("/api/v1/admin/accounts/:id", handler.Update)
	router.POST("/api/v1/admin/accounts/bulk-update", handler.BulkUpdate)
	router.POST("/api/v1/admin/accounts/import-codex-session", handler.ImportCodexSession)
	return router
}

func TestAccountHandlerCreatePassesPlatformPoolID(t *testing.T) {
	stub := &platformPoolAccountHandlerStub{AdminService: newStubAdminService()}
	router := setupPlatformPoolAccountHandlerRouter(stub)
	body, err := json.Marshal(map[string]any{
		"name":        "gpt-primary",
		"platform":    service.PlatformOpenAI,
		"platform_id": 42,
		"type":        service.AccountTypeAPIKey,
		"credentials": map[string]any{"api_key": "test"},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, stub.created)
	require.NotNil(t, stub.created.PlatformID)
	require.Equal(t, int64(42), *stub.created.PlatformID)
}

func TestAccountHandlerCreateRequiresPlatformPoolID(t *testing.T) {
	stub := &platformPoolAccountHandlerStub{AdminService: newStubAdminService()}
	router := setupPlatformPoolAccountHandlerRouter(stub)
	body, err := json.Marshal(map[string]any{
		"name":        "gpt-primary",
		"platform":    service.PlatformOpenAI,
		"type":        service.AccountTypeAPIKey,
		"credentials": map[string]any{"api_key": "test"},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, stub.created)
}

func TestAccountHandlerUpdatePassesPlatformPoolID(t *testing.T) {
	stub := &platformPoolAccountHandlerStub{AdminService: newStubAdminService()}
	router := setupPlatformPoolAccountHandlerRouter(stub)
	body, err := json.Marshal(map[string]any{"platform_id": 42})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/71", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, stub.updated)
	require.NotNil(t, stub.updated.PlatformID)
	require.Equal(t, int64(42), *stub.updated.PlatformID)
}

func TestFilterAccountsByPlatformPoolKeepsOnlyMatchingAccounts(t *testing.T) {
	platformOne := int64(1)
	platformTwo := int64(2)
	accounts := []service.Account{
		{ID: 10, PlatformID: &platformOne},
		{ID: 20, PlatformID: &platformTwo},
		{ID: 30},
	}

	filtered := filterAccountsByPlatformPool(accounts, platformTwo)

	require.Len(t, filtered, 1)
	require.Equal(t, int64(20), filtered[0].ID)
}
