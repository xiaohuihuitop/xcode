//go:build unit

package productcore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthorizerSelectsPlatformBeforeBillingAsset(t *testing.T) {
	subscriptionID := int64(21)
	platforms := platformCatalogStub{platforms: []*Platform{{
		ID: 3, AccountPlatform: "openai",
		EndpointCapabilities: []string{"chat_completions"},
	}}}
	assets := &assetSelectorStub{asset: &BillingAsset{
		Source: "subscription", SubscriptionID: &subscriptionID, RateMultiplier: 0.5,
	}}
	authorizer := NewAuthorizer(platforms, assets)

	decision, err := authorizer.Resolve(context.Background(), AccessGrant{
		KeyID: 10, UserID: 7, PlatformIDs: []int64{3}, AllowBalance: true,
	}, Request{Model: "gpt-4o", EndpointCapability: "chat_completions"})

	require.NoError(t, err)
	require.Equal(t, int64(3), decision.Platform.ID)
	require.Equal(t, "subscription", decision.BillingAsset.Source)
	require.True(t, assets.called)
}

func TestAuthorizerRejectsUnapprovedPlatformBeforeSelectingAsset(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{{
		ID: 3, AccountPlatform: "openai", EndpointCapabilities: []string{"chat_completions"},
	}}}
	assets := &assetSelectorStub{}
	_, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{4},
	}, Request{Model: "gpt-4o", EndpointCapability: "chat_completions"})

	require.ErrorIs(t, err, ErrPlatformForbidden)
	require.False(t, assets.called)
}

func TestAuthorizerRejectsUnsupportedEndpointBeforeSelectingAsset(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{{
		ID: 3, EndpointCapabilities: []string{"chat_completions"},
	}}}
	assets := &assetSelectorStub{}
	_, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{3},
	}, Request{Model: "gpt-4o", EndpointCapability: "responses"})

	require.ErrorIs(t, err, ErrEndpointUnsupported)
	require.False(t, assets.called)
}

func TestAuthorizerAllowsUnclassifiedEndpoint(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{{
		ID: 3, EndpointCapabilities: []string{"chat_completions"},
	}}}
	assets := &assetSelectorStub{asset: &BillingAsset{Source: "balance", RateMultiplier: 1}}

	decision, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{3}, AllowBalance: true,
	}, Request{Model: "gpt-4o"})

	require.NoError(t, err)
	require.Equal(t, "balance", decision.BillingAsset.Source)
	require.True(t, assets.called)
}

func TestAuthorizerSelectsAuthorizedEndpointCandidate(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{
		{ID: 3, AccountPlatform: "openai", EndpointCapabilities: []string{"chat_completions"}, MatchPriority: 100},
		{ID: 4, AccountPlatform: "openai", EndpointCapabilities: []string{"responses"}, MatchPriority: 100},
	}}
	assets := &assetSelectorStub{asset: &BillingAsset{Source: "balance", RateMultiplier: 1}}

	decision, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{4}, AllowBalance: true,
	}, Request{Model: "gpt-5", EndpointCapability: "responses"})

	require.NoError(t, err)
	require.Equal(t, int64(4), decision.Platform.ID)
}

func TestAuthorizerRejectsAmbiguousSamePriorityCandidates(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{
		{ID: 3, AccountPlatform: "openai", EndpointCapabilities: []string{"responses"}, MatchPriority: 100},
		{ID: 4, AccountPlatform: "glm", EndpointCapabilities: []string{"responses"}, MatchPriority: 100},
	}}

	_, err := NewAuthorizer(platforms, &assetSelectorStub{}).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{3, 4},
	}, Request{Model: "shared-model", EndpointCapability: "responses"})

	require.ErrorIs(t, err, ErrPlatformAmbiguous)
}

func TestAuthorizerRejectsEmptyCapabilitiesForClassifiedEndpoint(t *testing.T) {
	platforms := platformCatalogStub{platforms: []*Platform{{ID: 3}}}

	_, err := NewAuthorizer(platforms, &assetSelectorStub{}).Resolve(context.Background(), AccessGrant{
		PlatformIDs: []int64{3},
	}, Request{Model: "gpt-5", EndpointCapability: "responses"})

	require.ErrorIs(t, err, ErrEndpointUnsupported)
}

type platformCatalogStub struct {
	platforms []*Platform
	err       error
}

func (s platformCatalogStub) ListModelCandidates(context.Context, string) ([]*Platform, error) {
	return s.platforms, s.err
}

type assetSelectorStub struct {
	asset  *BillingAsset
	err    error
	called bool
}

func (s *assetSelectorStub) Select(context.Context, AccessGrant, bool) (*BillingAsset, error) {
	s.called = true
	return s.asset, s.err
}
