package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/productcore"
)

var ErrProductDecisionProviderUnavailable = errors.New("product decision provider is unavailable")

// ProductDecisionProvider adapts the existing ProductCore resolver to the
// pure application gateway contract. The constructed APIKey is a request
// snapshot; no runtime service receives it.
type ProductDecisionProvider struct {
	adapter *PlatformAssetProductCoreAdapter
}

func NewProductDecisionProvider(adapter *PlatformAssetProductCoreAdapter) *ProductDecisionProvider {
	return &ProductDecisionProvider{adapter: adapter}
}

func (p *ProductDecisionProvider) Resolve(
	ctx context.Context,
	grant productcore.AccessGrant,
	request productcore.Request,
) (*productcore.Decision, error) {
	if p == nil || p.adapter == nil {
		return nil, ErrProductDecisionProviderUnavailable
	}
	apiKey := apiKeySnapshotFromGrant(grant)
	resolution, err := p.adapter.Resolve(
		ctx,
		apiKey,
		request.Model,
		productCoreEndpointPath(request.EndpointCapability),
		request.SkipBilling,
	)
	if err != nil {
		return nil, err
	}
	if resolution == nil || resolution.Decision == nil {
		return nil, ErrProductDecisionProviderUnavailable
	}
	return resolution.Decision, nil
}

func apiKeySnapshotFromGrant(grant productcore.AccessGrant) *APIKey {
	return &APIKey{
		ID:                         grant.KeyID,
		UserID:                     grant.UserID,
		User:                       &User{ID: grant.UserID, Balance: grant.Balance},
		AllowedPlatformIDs:         append([]int64(nil), grant.PlatformIDs...),
		AllowedSubscriptionPlanIDs: append([]int64(nil), grant.SubscriptionPlanIDs...),
		AllowBalance:               grant.AllowBalance,
	}
}

func productCoreEndpointPath(capability string) string {
	normalized := strings.ToLower(strings.TrimSpace(capability))
	if strings.Contains(normalized, "/") {
		return normalized
	}
	switch normalized {
	case "chat", "chat_completions", "chat-completions":
		return "/v1/chat/completions"
	case "responses":
		return "/v1/responses"
	case "messages":
		return "/v1/messages"
	case "embeddings":
		return "/v1/embeddings"
	default:
		return capability
	}
}
