package productcore

import (
	"context"
	"strings"
)

type Authorizer struct {
	platforms PlatformCatalog
	assets    AssetSelector
}

func NewAuthorizer(platforms PlatformCatalog, assets AssetSelector) *Authorizer {
	return &Authorizer{platforms: platforms, assets: assets}
}

func (a *Authorizer) Resolve(ctx context.Context, grant AccessGrant, request Request) (*Decision, error) {
	if a == nil || a.platforms == nil {
		return nil, ErrModelUnavailable
	}
	candidates, err := a.platforms.ListModelCandidates(ctx, request.Model)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrModelUnavailable
	}

	authorized := make([]*Platform, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && allowsPlatform(grant.PlatformIDs, candidate.ID) {
			authorized = append(authorized, candidate)
		}
	}
	if len(authorized) == 0 {
		return nil, ErrPlatformForbidden
	}

	endpointCandidates := make([]*Platform, 0, len(authorized))
	for _, candidate := range authorized {
		if supportsEndpoint(candidate.EndpointCapabilities, request.EndpointCapability) {
			endpointCandidates = append(endpointCandidates, candidate)
		}
	}
	if len(endpointCandidates) == 0 {
		return nil, ErrEndpointUnsupported
	}
	platform, err := selectPlatformCandidate(endpointCandidates)
	if err != nil {
		return nil, err
	}
	if a.assets == nil {
		return nil, ErrNoBillingAsset
	}
	asset, err := a.assets.Select(ctx, grant, request.SkipBilling)
	if err != nil {
		return nil, err
	}
	if asset == nil && !request.SkipBilling {
		return nil, ErrNoBillingAsset
	}
	return &Decision{Platform: clonePlatform(*platform), BillingAsset: cloneBillingAsset(asset)}, nil
}

func selectPlatformCandidate(candidates []*Platform) (*Platform, error) {
	if len(candidates) == 0 {
		return nil, ErrModelUnavailable
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate == nil {
			continue
		}
		if best == nil || candidate.MatchPriority > best.MatchPriority {
			best = candidate
			continue
		}
		if candidate.MatchPriority == best.MatchPriority && candidate.ID != best.ID {
			return nil, ErrPlatformAmbiguous
		}
	}
	if best == nil {
		return nil, ErrModelUnavailable
	}
	return best, nil
}

func allowsPlatform(allowed []int64, platformID int64) bool {
	for _, candidate := range allowed {
		if candidate == platformID {
			return true
		}
	}
	return false
}

func supportsEndpoint(configured []string, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	for _, capability := range configured {
		if strings.EqualFold(strings.TrimSpace(capability), requested) {
			return true
		}
	}
	return false
}

func clonePlatform(value Platform) Platform {
	value.EndpointCapabilities = append([]string(nil), value.EndpointCapabilities...)
	return value
}

func cloneBillingAsset(value *BillingAsset) *BillingAsset {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.SubscriptionID = cloneInt64(value.SubscriptionID)
	cloned.PlanID = cloneInt64(value.PlanID)
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
