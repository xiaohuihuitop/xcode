package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/tidwall/gjson"
)

const (
	glmQuotaTimeout      = 15 * time.Second
	glmQuotaMaxBodyBytes = 256 * 1024
)

type GLMPlatformResolver interface {
	GetByID(context.Context, int64) (*Platform, error)
}

// glmPlatformResolver adapts the deliberately small PlatformRepository read
// contract to the account-scoped lookup needed by the GLM Runtime adapter.
// It keeps GetByID out of the shared repository interface used by gateway code.
type glmPlatformResolver struct {
	repo PlatformRepository
}

func NewGLMPlatformResolver(repo PlatformRepository) GLMPlatformResolver {
	return &glmPlatformResolver{repo: repo}
}

func (r *glmPlatformResolver) GetByID(ctx context.Context, id int64) (*Platform, error) {
	if r == nil || r.repo == nil {
		return nil, fmt.Errorf("GLM platform resolver is not configured")
	}
	platforms, err := r.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list GLM platforms: %w", err)
	}
	for index := range platforms {
		if platforms[index].ID == id {
			return &platforms[index], nil
		}
	}
	return nil, ErrPlatformNotFound
}

type GLMQuotaTier struct {
	Window      string  `json:"window"`
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at,omitempty"`
}

type GLMQuotaResult struct {
	Success         bool           `json:"success"`
	CredentialValid bool           `json:"credential_valid"`
	Tiers           []GLMQuotaTier `json:"tiers,omitempty"`
	PlanLevel       string         `json:"plan_level,omitempty"`
	StatusCode      int            `json:"status_code,omitempty"`
	FetchedAt       int64          `json:"fetched_at"`
	Persisted       bool           `json:"persisted"`
	ErrorCategory   string         `json:"error_category,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// GLMQuotaService maps the standard Zhipu Coding Plan quota endpoint onto an
// XCode Platform account without introducing a second provider account type.
type GLMQuotaService struct {
	accountRepo  AccountRepository
	platformRepo GLMPlatformResolver
	httpUpstream HTTPUpstream
	cfg          *config.Config
}

func NewGLMQuotaService(
	accountRepo AccountRepository,
	platformRepo GLMPlatformResolver,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *GLMQuotaService {
	return &GLMQuotaService{
		accountRepo:  accountRepo,
		platformRepo: platformRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

func (s *GLMQuotaService) Query(ctx context.Context, accountID int64) (*GLMQuotaResult, error) {
	if s == nil || s.accountRepo == nil || s.platformRepo == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("GLM quota service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, fmt.Errorf("GLM account not found: %w", err)
	}
	if account.PlatformID == nil || *account.PlatformID <= 0 {
		return nil, fmt.Errorf("GLM account is not bound to a platform")
	}
	platform, err := s.platformRepo.GetByID(ctx, *account.PlatformID)
	if err != nil || platform == nil {
		return nil, fmt.Errorf("GLM platform not found: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(platform.Code), "glm") ||
		!strings.EqualFold(strings.TrimSpace(platform.AccountPlatform), PlatformOpenAI) ||
		!strings.EqualFold(strings.TrimSpace(account.Platform), PlatformOpenAI) {
		return nil, fmt.Errorf("account does not belong to the GLM platform")
	}
	if account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("GLM quota requires an API key account")
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, fmt.Errorf("GLM account API key is empty")
	}

	endpoint, err := glmStandardQuotaEndpoint(account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	endpoint, err = validateGLMQuotaEndpoint(s.cfg, endpoint)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, glmQuotaTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build GLM quota request: %w", err)
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en")
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, fmt.Errorf("GLM quota request failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("GLM quota request returned an empty response")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, glmQuotaMaxBodyBytes))

	now := time.Now().UTC()
	result := &GLMQuotaResult{
		CredentialValid: true,
		StatusCode:      resp.StatusCode,
		FetchedAt:       now.Unix(),
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		result.ErrorCategory = glmQuotaErrorCategory(resp.StatusCode)
		// 403 means the credential reached the upstream but lacks entitlement;
		// only 401 is treated as an invalid key for account-state decisions.
		result.CredentialValid = resp.StatusCode != http.StatusUnauthorized
		result.Error = fmt.Sprintf("GLM quota endpoint returned HTTP %d", resp.StatusCode)
		return result, nil
	}
	if success := gjson.GetBytes(body, "success"); success.Exists() && !success.Bool() {
		result.ErrorCategory = "invalid_upstream_response"
		result.Error = "GLM quota endpoint returned an unsuccessful result"
		return result, nil
	}

	result.Tiers = parseGLMQuotaTiers(gjson.GetBytes(body, "data"))
	result.PlanLevel = strings.TrimSpace(gjson.GetBytes(body, "data.level").String())
	result.Success = true
	updates := glmQuotaExtraUpdates(result.Tiers, now)
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("glm_quota_persist_failed", "account_id", account.ID, "error", err)
		return result, nil
	}
	result.Persisted = true
	return result, nil
}

func glmStandardQuotaEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("standard GLM quota endpoint is unavailable for this base URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "open.bigmodel.cn" && host != "api.z.ai" {
		return "", fmt.Errorf("standard GLM quota endpoint is unavailable for custom relay host %q", host)
	}
	parsed.Path = "/api/monitor/usage/quota/limit"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validateGLMQuotaEndpoint(cfg *config.Config, endpoint string) (string, error) {
	if cfg != nil && cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateHTTPSURL(endpoint, urlvalidator.ValidationOptions{
			AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
			RequireAllowlist: true,
			AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
		})
		if err != nil {
			return "", fmt.Errorf("GLM quota endpoint rejected by URL security policy: %w", err)
		}
		return normalized, nil
	}
	return endpoint, nil
}

func glmQuotaErrorCategory(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "credential_invalid"
	case status == http.StatusForbidden:
		return "upstream_forbidden"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status >= http.StatusInternalServerError:
		return "upstream_5xx"
	default:
		return "invalid_upstream_response"
	}
}

type glmQuotaWindow int

const (
	glmQuotaWindowUnknown glmQuotaWindow = iota
	glmQuotaWindow5h
	glmQuotaWindowWeekly
)

func classifyGLMQuotaWindow(unit int64) glmQuotaWindow {
	switch unit {
	case 3:
		return glmQuotaWindow5h
	case 6:
		return glmQuotaWindowWeekly
	default:
		return glmQuotaWindowUnknown
	}
}

func parseGLMQuotaTiers(data gjson.Result) []GLMQuotaTier {
	type entry struct {
		resetMs    int64
		hasReset   bool
		percentage float64
		resetISO   string
	}
	var fiveHour, weekly entry
	var fiveHourSet, weeklySet bool
	var unclassified, creditFallback []entry
	hasTokenLimit := false

	classify := func(item gjson.Result, value entry) {
		switch classifyGLMQuotaWindow(item.Get("unit").Int()) {
		case glmQuotaWindow5h:
			if !fiveHourSet {
				fiveHour, fiveHourSet = value, true
			} else {
				unclassified = append(unclassified, value)
			}
		case glmQuotaWindowWeekly:
			if !weeklySet {
				weekly, weeklySet = value, true
			} else {
				unclassified = append(unclassified, value)
			}
		default:
			unclassified = append(unclassified, value)
		}
	}

	data.Get("limits").ForEach(func(_, item gjson.Result) bool {
		limitType := strings.ToUpper(strings.TrimSpace(item.Get("type").String()))
		if limitType != "TOKENS_LIMIT" && limitType != "CREDIT_LIMIT" {
			return true
		}
		percentage, _ := strconv.ParseFloat(strings.TrimSpace(item.Get("percentage").String()), 64)
		reset := item.Get("nextResetTime")
		resetMs := reset.Int()
		resetISO := glmQuotaResetTime(reset)
		value := entry{resetMs: resetMs, hasReset: resetISO != "", percentage: percentage, resetISO: resetISO}
		if limitType == "TOKENS_LIMIT" {
			hasTokenLimit = true
			classify(item, value)
		} else {
			creditFallback = append(creditFallback, value)
		}
		return true
	})
	if !hasTokenLimit {
		unclassified = append(unclassified, creditFallback...)
	}
	sort.SliceStable(unclassified, func(i, j int) bool {
		if unclassified[i].hasReset != unclassified[j].hasReset {
			return !unclassified[i].hasReset
		}
		return unclassified[i].resetMs < unclassified[j].resetMs
	})
	for _, value := range unclassified {
		if !fiveHourSet {
			fiveHour, fiveHourSet = value, true
		} else if !weeklySet {
			weekly, weeklySet = value, true
		}
	}

	tiers := make([]GLMQuotaTier, 0, 2)
	if fiveHourSet {
		tiers = append(tiers, GLMQuotaTier{Window: "5h", UsedPercent: fiveHour.percentage, ResetAt: fiveHour.resetISO})
	}
	if weeklySet {
		tiers = append(tiers, GLMQuotaTier{Window: "weekly", UsedPercent: weekly.percentage, ResetAt: weekly.resetISO})
	}
	return tiers
}

func glmQuotaResetTime(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value.String()))
		if err != nil {
			return ""
		}
		return parsed.UTC().Format(time.RFC3339)
	}
	timestamp := value.Int()
	if timestamp <= 0 {
		return ""
	}
	if timestamp < 1_000_000_000_000 {
		timestamp *= 1000
	}
	return time.UnixMilli(timestamp).UTC().Format(time.RFC3339)
}

func glmQuotaExtraUpdates(tiers []GLMQuotaTier, now time.Time) map[string]any {
	updates := map[string]any{"glm_usage_updated_at": now.UTC().Format(time.RFC3339)}
	for _, tier := range tiers {
		switch tier.Window {
		case "5h":
			updates["glm_5h_used_percent"] = tier.UsedPercent
			if tier.ResetAt != "" {
				updates["glm_5h_reset_at"] = tier.ResetAt
			}
		case "weekly":
			updates["glm_weekly_used_percent"] = tier.UsedPercent
			if tier.ResetAt != "" {
				updates["glm_weekly_reset_at"] = tier.ResetAt
			}
		}
	}
	return updates
}
