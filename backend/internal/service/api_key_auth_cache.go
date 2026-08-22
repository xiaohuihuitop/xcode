package service

import "time"

// APIKeyAuthSnapshot API Key 认证缓存快照（仅包含认证所需字段）
type APIKeyAuthSnapshot struct {
	Version                    int                    `json:"version"`
	APIKeyID                   int64                  `json:"api_key_id"`
	UserID                     int64                  `json:"user_id"`
	Name                       string                 `json:"name"`
	Status                     string                 `json:"status"`
	IPWhitelist                []string               `json:"ip_whitelist,omitempty"`
	IPBlacklist                []string               `json:"ip_blacklist,omitempty"`
	User                       APIKeyAuthUserSnapshot `json:"user"`
	AllowedPlatformIDs         []int64                `json:"allowed_platform_ids,omitempty"`
	AllowedSubscriptionPlanIDs []int64                `json:"allowed_subscription_plan_ids,omitempty"`
	AllowAllSubscriptions      bool                   `json:"allow_all_subscriptions"`
	AllowBalance               bool                   `json:"allow_balance"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at check time)
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// APIKeyAuthUserSnapshot 用户快照
type APIKeyAuthUserSnapshot struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	Role        string  `json:"role"`
	Balance     float64 `json:"balance"`
	Concurrency int     `json:"concurrency"`

	// Balance notification fields (required for CheckBalanceAfterDeduction)
	Email                      string             `json:"email"`
	Username                   string             `json:"username"`
	BalanceNotifyEnabled       bool               `json:"balance_notify_enabled"`
	BalanceNotifyThresholdType string             `json:"balance_notify_threshold_type"`
	BalanceNotifyThreshold     *float64           `json:"balance_notify_threshold,omitempty"`
	BalanceNotifyExtraEmails   []NotifyEmailEntry `json:"balance_notify_extra_emails,omitempty"`
	TotalRecharged             float64            `json:"total_recharged"`

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）；用于 billing_cache_service.checkRPM 兜底判断。
	RPMLimit int `json:"rpm_limit"`
}

// APIKeyAuthCacheEntry 缓存条目，支持负缓存
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}
