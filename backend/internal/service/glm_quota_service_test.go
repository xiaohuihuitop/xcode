//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type glmQuotaAccountRepoStub struct {
	AccountRepository
	account *Account
	updates map[string]any
}

func (r *glmQuotaAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *glmQuotaAccountRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

type glmQuotaPlatformRepoStub struct {
	PlatformRepository
	platform *Platform
}

func (r *glmQuotaPlatformRepoStub) GetByID(context.Context, int64) (*Platform, error) {
	return r.platform, nil
}

type glmQuotaPlatformListRepoStub struct {
	PlatformRepository
	platforms []Platform
}

func (r *glmQuotaPlatformListRepoStub) List(context.Context) ([]Platform, error) {
	return r.platforms, nil
}

type glmQuotaHTTPUpstreamStub struct {
	request  *http.Request
	response *http.Response
	calls    int
}

func (s *glmQuotaHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.calls++
	s.request = req
	return s.response, nil
}

func (s *glmQuotaHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

func newGLMQuotaTestAccount(baseURL string) *Account {
	platformID := int64(2)
	return &Account{
		ID:          17,
		Platform:    PlatformOpenAI,
		PlatformID:  &platformID,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":                 "glm-secret",
			"base_url":                baseURL,
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"X-GLM-Tenant": "tenant-a",
			},
		},
	}
}

func newGLMQuotaTestService(account *Account, response *http.Response) (*GLMQuotaService, *glmQuotaAccountRepoStub, *glmQuotaHTTPUpstreamStub) {
	accountRepo := &glmQuotaAccountRepoStub{account: account}
	upstream := &glmQuotaHTTPUpstreamStub{response: response}
	service := NewGLMQuotaService(
		accountRepo,
		&glmQuotaPlatformRepoStub{platform: &Platform{ID: 2, Code: "glm", AccountPlatform: PlatformOpenAI}},
		upstream,
		&config.Config{},
	)
	return service, accountRepo, upstream
}

func TestGLMQuotaServiceQueriesStandardCodingPlanAndPersistsRuntimeFacts(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"success":true,
			"data":{"level":"pro","limits":[
				{"type":"TOKENS_LIMIT","unit":3,"percentage":25,"nextResetTime":1787500000000},
				{"type":"TOKENS_LIMIT","unit":6,"percentage":"40","nextResetTime":1787900000000}
			]}
		}`)),
	}
	service, repo, upstream := newGLMQuotaTestService(
		newGLMQuotaTestAccount("https://open.bigmodel.cn/api/coding/paas/v4"),
		response,
	)
	require.Equal(t, map[string]string{"x-glm-tenant": "tenant-a"}, service.accountRepo.(*glmQuotaAccountRepoStub).account.GetHeaderOverrides())

	result, err := service.Query(context.Background(), 17)

	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.CredentialValid)
	require.True(t, result.Persisted)
	require.Equal(t, "pro", result.PlanLevel)
	require.Len(t, result.Tiers, 2)
	require.Equal(t, "5h", result.Tiers[0].Window)
	require.Equal(t, 25.0, result.Tiers[0].UsedPercent)
	require.Equal(t, "weekly", result.Tiers[1].Window)
	require.Equal(t, 40.0, result.Tiers[1].UsedPercent)
	require.Equal(t, "https://open.bigmodel.cn/api/monitor/usage/quota/limit", upstream.request.URL.String())
	require.Equal(t, "glm-secret", upstream.request.Header.Get("Authorization"))
	require.Equal(t, []string{"tenant-a"}, upstream.request.Header["x-glm-tenant"])
	require.Equal(t, 25.0, repo.updates["glm_5h_used_percent"])
	require.Equal(t, 40.0, repo.updates["glm_weekly_used_percent"])
	require.NotEmpty(t, repo.updates["glm_usage_updated_at"])
}

func TestGLMQuotaServiceRejectsCustomRelayWithoutSendingCredential(t *testing.T) {
	service, repo, upstream := newGLMQuotaTestService(
		newGLMQuotaTestAccount("https://relay.example.com/v1"),
		&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))},
	)

	_, err := service.Query(context.Background(), 17)

	require.ErrorContains(t, err, "standard GLM quota endpoint is unavailable")
	require.Zero(t, upstream.calls)
	require.Nil(t, repo.updates)
}

func TestGLMQuotaServiceClassifiesAuthenticationAndUpstreamFailures(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantCategory string
		credential   bool
	}{
		{name: "401", status: http.StatusUnauthorized, wantCategory: "credential_invalid"},
		{name: "403", status: http.StatusForbidden, wantCategory: "upstream_forbidden", credential: true},
		{name: "429", status: http.StatusTooManyRequests, wantCategory: "rate_limited", credential: true},
		{name: "5xx", status: http.StatusBadGateway, wantCategory: "upstream_5xx", credential: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, repo, _ := newGLMQuotaTestService(
				newGLMQuotaTestAccount("https://api.z.ai/api/coding/paas/v4"),
				&http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(`{"error":"failed"}`))},
			)

			result, err := service.Query(context.Background(), 17)

			require.NoError(t, err)
			require.False(t, result.Success)
			require.Equal(t, test.credential, result.CredentialValid)
			require.Equal(t, test.wantCategory, result.ErrorCategory)
			require.Nil(t, repo.updates)
		})
	}
}

func TestGLMPlatformResolverFindsPlatformByIDWithoutExpandingRepositoryContract(t *testing.T) {
	resolver := NewGLMPlatformResolver(&glmQuotaPlatformListRepoStub{platforms: []Platform{
		{ID: 1, Code: "codex"},
		{ID: 2, Code: "glm", AccountPlatform: PlatformOpenAI},
	}})

	platform, err := resolver.GetByID(context.Background(), 2)

	require.NoError(t, err)
	require.Equal(t, "glm", platform.Code)

	_, err = resolver.GetByID(context.Background(), 99)
	require.ErrorIs(t, err, ErrPlatformNotFound)
}
