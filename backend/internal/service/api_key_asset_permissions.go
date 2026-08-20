package service

import (
	"sort"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAPIKeyPlatformRequired      = infraerrors.BadRequest("API_KEY_PLATFORM_REQUIRED", "api key must authorize at least one platform")
	ErrAPIKeyBillingSourceRequired = infraerrors.BadRequest("API_KEY_BILLING_SOURCE_REQUIRED", "api key must authorize a subscription plan or balance")
)

// APIKeyAssetPermissions separates account-pool access from the user assets
// that the key may spend once a platform has been resolved.
type APIKeyAssetPermissions struct {
	PlatformIDs         []int64 `json:"platform_ids"`
	SubscriptionPlanIDs []int64 `json:"subscription_plan_ids"`
	AllowBalance        bool    `json:"allow_balance"`
}

func NormalizeAPIKeyAssetPermissions(permissions APIKeyAssetPermissions) APIKeyAssetPermissions {
	return APIKeyAssetPermissions{
		PlatformIDs:         normalizePositiveIDs(permissions.PlatformIDs),
		SubscriptionPlanIDs: normalizePositiveIDs(permissions.SubscriptionPlanIDs),
		AllowBalance:        permissions.AllowBalance,
	}
}

func ValidateAPIKeyAssetPermissions(permissions APIKeyAssetPermissions) error {
	normalized := NormalizeAPIKeyAssetPermissions(permissions)
	if len(normalized.PlatformIDs) == 0 {
		return ErrAPIKeyPlatformRequired
	}
	if len(normalized.SubscriptionPlanIDs) == 0 && !normalized.AllowBalance {
		return ErrAPIKeyBillingSourceRequired
	}
	return nil
}

func newAPIKeyFromCreateRequest(req CreateAPIKeyRequest) *APIKey {
	allowBalance := true
	if req.AllowBalance != nil {
		allowBalance = *req.AllowBalance
	}
	permissions := NormalizeAPIKeyAssetPermissions(APIKeyAssetPermissions{
		PlatformIDs:         req.PlatformIDs,
		SubscriptionPlanIDs: req.SubscriptionPlanIDs,
		AllowBalance:        allowBalance,
	})
	return &APIKey{
		Name:                       req.Name,
		IPWhitelist:                append([]string(nil), req.IPWhitelist...),
		IPBlacklist:                append([]string(nil), req.IPBlacklist...),
		Quota:                      req.Quota,
		RateLimit5h:                req.RateLimit5h,
		RateLimit1d:                req.RateLimit1d,
		RateLimit7d:                req.RateLimit7d,
		AllowedPlatformIDs:         permissions.PlatformIDs,
		AllowedSubscriptionPlanIDs: permissions.SubscriptionPlanIDs,
		AllowBalance:               permissions.AllowBalance,
	}
}

func createAPIKeyAssetPermissionsProvided(req CreateAPIKeyRequest) bool {
	return req.PlatformIDs != nil || req.SubscriptionPlanIDs != nil || req.AllowBalance != nil
}

func updateAPIKeyAssetPermissionsProvided(req UpdateAPIKeyRequest) bool {
	return req.PlatformIDs != nil || req.SubscriptionPlanIDs != nil || req.AllowBalance != nil
}

func updatedAPIKeyAssetPermissions(key *APIKey, req UpdateAPIKeyRequest) APIKeyAssetPermissions {
	permissions := APIKeyAssetPermissions{}
	if key != nil {
		permissions.PlatformIDs = key.AllowedPlatformIDs
		permissions.SubscriptionPlanIDs = key.AllowedSubscriptionPlanIDs
		permissions.AllowBalance = key.AllowBalance
	}
	if req.PlatformIDs != nil {
		permissions.PlatformIDs = *req.PlatformIDs
	}
	if req.SubscriptionPlanIDs != nil {
		permissions.SubscriptionPlanIDs = *req.SubscriptionPlanIDs
	}
	if req.AllowBalance != nil {
		permissions.AllowBalance = *req.AllowBalance
	}
	return NormalizeAPIKeyAssetPermissions(permissions)
}

func normalizePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			seen[id] = struct{}{}
		}
	}
	normalized := make([]int64, 0, len(seen))
	for id := range seen {
		normalized = append(normalized, id)
	}
	sort.Slice(normalized, func(left, right int) bool {
		return normalized[left] < normalized[right]
	})
	return normalized
}
