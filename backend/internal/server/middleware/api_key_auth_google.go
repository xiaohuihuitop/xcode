package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyAuthGoogle is a Google-style error wrapper for API key auth.
func APIKeyAuthGoogle(apiKeyService *service.APIKeyService, cfg *config.Config) gin.HandlerFunc {
	return APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, cfg)
}

// APIKeyAuthWithSubscriptionGoogle behaves like ApiKeyAuthWithSubscription but returns Google-style errors:
// {"error":{"code":401,"message":"...","status":"UNAUTHENTICATED"}}
//
// It is intended for Gemini native endpoints (/v1beta) to match Gemini SDK expectations.
func APIKeyAuthWithSubscriptionGoogle(apiKeyService *service.APIKeyService, _ *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rejectInvalidAuthAbuse(c, apiKeyService) {
			abortWithGoogleError(c, 429, "Too many invalid authentication attempts; retry later")
			return
		}
		if apiKeyHeadersTooLarge(c) {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			abortWithGoogleError(c, 401, "Invalid API key")
			return
		}
		if v := strings.TrimSpace(c.Query("api_key")); v != "" {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectQueryAPIKeyDeprecated)
			abortWithGoogleError(c, 400, "Query parameter api_key is deprecated. Use Authorization header or key instead.")
			return
		}
		apiKeyString := extractAPIKeyForGoogle(c)
		if apiKeyString == "" {
			recordInvalidAuthFailure(c, apiKeyService)
			if hasAPIKeyCredentialInput(c) {
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			} else {
				MarkIngressRejected(c, IngressRejectAPIKeyRequired)
			}
			abortWithGoogleError(c, 401, "API key is required")
			return
		}
		if len(apiKeyString) > service.MaxAPIKeyCredentialBytes {
			recordInvalidAuthFailure(c, apiKeyService)
			MarkIngressRejected(c, IngressRejectInvalidAPIKey)
			abortWithGoogleError(c, 401, "Invalid API key")
			return
		}

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				recordInvalidAuthFailure(c, apiKeyService)
				MarkIngressRejected(c, IngressRejectInvalidAPIKey)
				abortWithGoogleError(c, 401, "Invalid API key")
				return
			}
			if errors.Is(err, service.ErrAPIKeyAuthOverloaded) {
				MarkIngressRejected(c, IngressRejectAPIKeyAuthOverloaded)
				abortWithGoogleError(c, 503, "API key authentication is temporarily unavailable")
				return
			}
			abortWithGoogleError(c, 500, "Failed to validate API key")
			return
		}

		// 同 api_key_auth.go：早退中断前也写入 Ops 回退 key，便于错误日志展示
		// user/group/platform。
		SetOpsFallbackAPIKey(c, apiKey)

		// disabled / 未知状态 → 无条件拦截（expired 和 quota_exhausted 留给计费阶段，
		// 与主中间件 api_key_auth.go 保持一致）。
		if !apiKey.IsActive() &&
			apiKey.Status != service.StatusAPIKeyExpired &&
			apiKey.Status != service.StatusAPIKeyQuotaExhausted {
			MarkIngressRejected(c, IngressRejectAPIKeyDisabled)
			abortWithGoogleError(c, 401, "API key is disabled")
			return
		}

		// 检查 IP 限制（白名单/黑名单）。与主中间件保持一致，避免 Gemini 端点绕过 Key 的 IP ACL。
		if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
			clientIP := ip.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
			if !allowed {
				if clientIP == "" {
					clientIP = "unknown"
				}
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
				MarkIngressRejected(c, IngressRejectIPRestricted)
				abortWithGoogleError(c, 403, fmt.Sprintf("Access denied. Your IP is %s", clientIP))
				return
			}
		}

		if apiKey.User == nil {
			abortWithGoogleError(c, 401, "User associated with API key not found")
			return
		}
		if !apiKey.User.IsActive() {
			MarkIngressRejected(c, IngressRejectUserInactive)
			abortWithGoogleError(c, 401, "User account is not active")
			return
		}
		if !service.UsesPlatformAssetPermissions(apiKey) {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyPlatformUnassigned)
			MarkIngressRejected(c, IngressRejectPlatformRequired)
			abortWithGoogleError(c, http.StatusForbidden, "API Key 未授权任何平台")
			return
		}
		completePlatformAssetAPIKeyAuthGoogle(c, apiKey, apiKeyService, cfg)
	}
}

// completePlatformAssetAPIKeyAuthGoogle mirrors the V2 branch of the standard
// API-key middleware while preserving Gemini's native error envelope. Platform
// authorization and billing are intentionally deferred until the request model
// is resolved by NewPlatformAssetAuthorizationGoogleMiddleware.
func completePlatformAssetAPIKeyAuthGoogle(
	c *gin.Context,
	apiKey *service.APIKey,
	apiKeyService *service.APIKeyService,
	cfg *config.Config,
) {
	if cfg == nil || cfg.RunMode != config.RunModeSimple {
		switch apiKey.Status {
		case service.StatusAPIKeyQuotaExhausted:
			abortWithGoogleError(c, http.StatusTooManyRequests, "API key quota exhausted")
			return
		case service.StatusAPIKeyExpired:
			abortWithGoogleError(c, http.StatusForbidden, "API key expired")
			return
		}
		if apiKey.IsExpired() {
			abortWithGoogleError(c, http.StatusForbidden, "API key expired")
			return
		}
		if apiKey.IsQuotaExhausted() {
			abortWithGoogleError(c, http.StatusTooManyRequests, "API key quota exhausted")
			return
		}
	}

	c.Set(string(ContextKeyAPIKey), apiKey)
	c.Set(string(ContextKeyUser), AuthSubject{
		UserID:      apiKey.User.ID,
		Concurrency: apiKey.User.Concurrency,
	})
	c.Set(string(ContextKeyUserRole), apiKey.User.Role)
	_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
	c.Next()
}

// extractAPIKeyForGoogle extracts API key for Google/Gemini endpoints.
// Priority: x-goog-api-key > Authorization: Bearer > x-api-key > query key
// This allows OpenClaw and other clients using Bearer auth to work with Gemini endpoints.
func extractAPIKeyForGoogle(c *gin.Context) string {
	// 1) preferred: Gemini native header
	if k := strings.TrimSpace(c.GetHeader("x-goog-api-key")); k != "" {
		return k
	}

	// 2) fallback: Authorization: Bearer <key>
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if k := strings.TrimSpace(parts[1]); k != "" {
				return k
			}
		}
	}

	// 3) x-api-key header (backward compatibility)
	if k := strings.TrimSpace(c.GetHeader("x-api-key")); k != "" {
		return k
	}

	// 4) query parameter key (for specific paths)
	if allowGoogleQueryKey(c.Request.URL.Path) {
		if v := strings.TrimSpace(c.Query("key")); v != "" {
			return v
		}
	}

	return ""
}

func allowGoogleQueryKey(path string) bool {
	return strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta")
}

func abortWithGoogleError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
	c.Abort()
}
