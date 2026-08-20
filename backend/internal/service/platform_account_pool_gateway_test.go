//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

type platformPoolSchedulingRepoStub struct {
	AccountRepository
	poolAccounts   []Account
	legacyAccounts map[string][]Account
	poolID         int64
	poolPlatform   string
	poolCalls      int
	legacyCalls    int
}

func (r *platformPoolSchedulingRepoStub) ListSchedulableByPlatformPool(
	_ context.Context,
	platformID int64,
	accountPlatform string,
) ([]Account, error) {
	r.poolCalls++
	r.poolID = platformID
	r.poolPlatform = accountPlatform
	return append([]Account(nil), r.poolAccounts...), nil
}

func (r *platformPoolSchedulingRepoStub) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.legacyCalls++
	return append([]Account(nil), r.legacyAccounts[platform]...), nil
}

func (r *platformPoolSchedulingRepoStub) ListSchedulableUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	r.legacyCalls++
	accounts := make([]Account, 0, len(platforms))
	for _, platform := range platforms {
		accounts = append(accounts, r.legacyAccounts[platform]...)
	}
	return accounts, nil
}

func TestGatewayServiceSelectsOnlyExplicitPlatformPool(t *testing.T) {
	platformID := int64(42)
	repo := &platformPoolSchedulingRepoStub{
		poolAccounts: []Account{{
			ID:          420,
			PlatformID:  &platformID,
			Platform:    PlatformAnthropic,
			Status:      StatusActive,
			Schedulable: true,
		}},
		legacyAccounts: map[string][]Account{
			PlatformAnthropic: {{ID: 419, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true}},
		},
	}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID:      platformID,
		PlatformCode:    "anthropic-primary",
		AccountPlatform: PlatformAnthropic,
	})

	account, err := (&GatewayService{accountRepo: repo}).SelectAccountForModelWithExclusions(ctx, nil, "", "", nil)

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(420), account.ID)
	require.Equal(t, 1, repo.poolCalls)
	require.Equal(t, platformID, repo.poolID)
	require.Equal(t, PlatformAnthropic, repo.poolPlatform)
	require.Zero(t, repo.legacyCalls)
}

func TestOpenAIGatewayServiceSelectsOnlyExplicitPlatformPool(t *testing.T) {
	platformID := int64(43)
	repo := &platformPoolSchedulingRepoStub{
		poolAccounts: []Account{{
			ID:          430,
			PlatformID:  &platformID,
			Platform:    PlatformOpenAI,
			Status:      StatusActive,
			Schedulable: true,
		}},
		legacyAccounts: map[string][]Account{
			PlatformOpenAI: {{ID: 429, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}},
		},
	}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID:      platformID,
		PlatformCode:    "gpt-primary",
		AccountPlatform: PlatformOpenAI,
	})

	account, err := (&OpenAIGatewayService{accountRepo: repo}).SelectAccountForModelWithExclusions(ctx, nil, "", "", nil)

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(430), account.ID)
	require.Equal(t, 1, repo.poolCalls)
	require.Equal(t, platformID, repo.poolID)
	require.Equal(t, PlatformOpenAI, repo.poolPlatform)
	require.Zero(t, repo.legacyCalls)
}

func TestPlatformScopedAccountIgnoresAccountAdminModelAndEndpointPolicy(t *testing.T) {
	platformID := int64(7)
	account := &Account{
		ID: 12, PlatformID: &platformID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping":       map[string]any{"gpt-old": "gpt-old"},
			"openai_capabilities": []any{"chat_completions"},
		},
		Status: StatusActive, Schedulable: true,
	}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
	})

	require.True(t, platformRouteOwnsModelPolicy(ctx))
	require.True(t, isOpenAICompatibleAccountEligibleForRequest(
		ctx, account, PlatformOpenAI, "gpt-5.6", false, OpenAIEndpointCapabilityResponses,
	))
	require.False(t, isOpenAICompatibleAccountEligibleForRequest(
		context.Background(), account, PlatformOpenAI, "gpt-5.6", false, OpenAIEndpointCapabilityResponses,
	))
}

func TestPlatformScopedAccountKeepsTechnicalOpenAIConstraints(t *testing.T) {
	platformID := int64(8)
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 8, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
	})

	compactBlocked := &Account{
		ID: 21, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Extra: map[string]any{"openai_compact_mode": OpenAICompactModeForceOff},
	}
	require.False(t, isOpenAICompatibleAccountEligibleForRequest(
		ctx, compactBlocked, PlatformOpenAI, "gpt-5.6", true, OpenAIEndpointCapabilityResponses,
	), "Platform routing must not bypass the account's compact capability")

	imageAccount := &Account{
		ID: 22, PlatformID: &platformID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true,
	}
	require.True(t, accountSupportsOpenAICapabilitiesForRequest(
		ctx, imageAccount, OpenAIEndpointCapabilityResponses, OpenAIImagesCapabilityNative,
	), "native image capability remains an adapter-level account constraint")

	mediaBlocked := &Account{
		ID: 23, PlatformID: &platformID, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Extra: map[string]any{GrokMediaEligibleExtraKey: false},
	}
	require.False(t, accountSupportsOpenAICapabilitiesForRequest(
		ctx, mediaBlocked, OpenAIEndpointCapabilityGrokMediaGeneration, ""),
		"Grok media eligibility remains an adapter-level account constraint")
}

func TestPlatformScopedAccountKeepsOpenAITechnicalEndpointConstraints(t *testing.T) {
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 9, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
	})

	apiKey := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Extra:    map[string]any{},
	}
	require.False(t, accountSupportsOpenAIEndpointForRequest(ctx, apiKey, OpenAIEndpointCapabilityLive),
		"Live requires an OpenAI OAuth account")

	oauth := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
	}
	require.True(t, accountSupportsOpenAIEndpointForRequest(ctx, oauth, OpenAIEndpointCapabilityLive),
		"OpenAI OAuth remains eligible for Live")

	serviceAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeServiceAccount,
		Status:   StatusActive,
	}
	require.False(t, accountSupportsOpenAIEndpointForRequest(ctx, serviceAccount, OpenAIEndpointCapabilityLive),
		"non-OAuth accounts cannot be routed to Live")

	upstream := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeUpstream,
		Status:   StatusActive,
	}
	require.False(t, accountSupportsOpenAIEndpointForRequest(ctx, upstream, OpenAIEndpointCapabilityAlphaSearch),
		"alpha search keeps its account-type restriction")

	responsesDisabled := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
		},
	}
	require.False(t, accountSupportsOpenAIEndpointForRequest(ctx, responsesDisabled, OpenAIEndpointCapabilityResponses),
		"Responses probe/mode remains an account technical constraint")
}
