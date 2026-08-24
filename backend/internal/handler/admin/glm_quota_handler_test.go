package admin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type glmQuotaHandlerAccountRepo struct {
	service.AccountRepository
	account *service.Account
	updates map[string]any
}

func (r *glmQuotaHandlerAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

func (r *glmQuotaHandlerAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

type glmQuotaHandlerPlatformRepo struct {
	service.PlatformRepository
}

func (r *glmQuotaHandlerPlatformRepo) GetByID(context.Context, int64) (*service.Platform, error) {
	return &service.Platform{ID: 2, Code: "glm", AccountPlatform: service.PlatformOpenAI}, nil
}

type glmQuotaHandlerHTTPUpstream struct {
	response *http.Response
}

func (u *glmQuotaHandlerHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.response, nil
}

func (u *glmQuotaHandlerHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func newGLMQuotaHandlerTestHandler(body string, status int) (*GLMQuotaHandler, *glmQuotaHandlerAccountRepo) {
	platformID := int64(2)
	repo := &glmQuotaHandlerAccountRepo{account: &service.Account{
		ID:         17,
		Platform:   service.PlatformOpenAI,
		PlatformID: &platformID,
		Type:       service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "handler-secret",
			"base_url": "https://open.bigmodel.cn/api/coding/paas/v4",
		},
	}}
	quotaService := service.NewGLMQuotaService(
		repo,
		&glmQuotaHandlerPlatformRepo{},
		&glmQuotaHandlerHTTPUpstream{response: &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
		}},
		&config.Config{},
	)
	return NewGLMQuotaHandler(quotaService), repo
}

func TestGLMQuotaHandlerReturnsRuntimeFactsWithoutCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo := newGLMQuotaHandlerTestHandler(`{"success":true,"data":{"level":"pro","limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":25,"nextResetTime":1787500000000}]}}`, http.StatusOK)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts/17/glm-quota", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "17"}}

	handler.Query(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"window":"5h"`)
	require.Contains(t, recorder.Body.String(), `"plan_level":"pro"`)
	require.NotContains(t, recorder.Body.String(), "handler-secret")
	require.NotNil(t, repo.updates)
}

func TestGLMQuotaHandlerRejectsInvalidAccountID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newGLMQuotaHandlerTestHandler(`{}`, http.StatusOK)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts/not-an-id/glm-quota", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "not-an-id"}}

	handler.Query(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGLMQuotaHandlerExposesUpstreamFailureCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newGLMQuotaHandlerTestHandler(`{"error":"rate limited"}`, http.StatusTooManyRequests)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts/17/glm-quota", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "17"}}

	handler.Query(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"error_category":"rate_limited"`)
	require.Contains(t, recorder.Body.String(), `"credential_valid":true`)
}
