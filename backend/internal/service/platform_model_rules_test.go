package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePlatformModelRulesAllowsCrossPlatformOverlap(t *testing.T) {
	err := validatePlatformModelRules([]PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-*", EndpointCapabilities: []string{"chat_completions"}, Enabled: true},
		{PlatformID: 2, ModelPattern: "gpt-4o", EndpointCapabilities: []string{"responses"}, Enabled: true},
	})

	require.NoError(t, err)
}

func TestValidatePlatformModelRulesRejectsDuplicatePatternOnSamePlatform(t *testing.T) {
	err := validatePlatformModelRules([]PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-4o", EndpointCapabilities: []string{"chat_completions"}, Enabled: true},
		{PlatformID: 1, ModelPattern: "gpt-4o", EndpointCapabilities: []string{"responses"}, Enabled: true},
	})

	require.ErrorContains(t, err, "duplicate pattern")
}

func TestValidatePlatformModelRulesDoesNotOwnEndpointCapabilities(t *testing.T) {
	err := validatePlatformModelRules([]PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-4o", Enabled: true},
	})

	require.NoError(t, err)
}

func TestResolvePlatformModelUsesExactBeforeSuffixWildcard(t *testing.T) {
	resolver := newPlatformModelResolver([]PlatformModelRule{
		{PlatformID: 1, PlatformCode: PlatformOpenAI, ModelPattern: "gpt-*", EndpointCapabilities: []string{"chat_completions"}, Enabled: true},
		{PlatformID: 2, PlatformCode: "grok", ModelPattern: "gpt-4o", UpstreamModel: "gpt-4o-2024-08-06", EndpointCapabilities: []string{"responses"}, Enabled: true},
	})

	got, err := resolver.Resolve("gpt-4o")

	require.NoError(t, err)
	require.Equal(t, int64(2), got.PlatformID)
	require.Equal(t, "grok", got.PlatformCode)
	require.Equal(t, "gpt-4o-2024-08-06", got.UpstreamModel)
}

func TestResolvePlatformModelRejectsAnUnmatchedModel(t *testing.T) {
	resolver := newPlatformModelResolver([]PlatformModelRule{
		{PlatformID: 1, PlatformCode: PlatformOpenAI, ModelPattern: "gpt-*", EndpointCapabilities: []string{"chat_completions"}, Enabled: true},
	})

	_, err := resolver.Resolve("claude-sonnet-4")

	require.ErrorIs(t, err, ErrPlatformModelNotFound)
}

func TestResolvePlatformModelCandidatesKeepsEndpointSpecificPlatforms(t *testing.T) {
	resolver := newPlatformModelResolver([]PlatformModelRule{
		{ID: 11, PlatformID: 1, PlatformCode: PlatformOpenAI, ModelPattern: "gpt-*", EndpointCapabilities: []string{"chat_completions"}, Enabled: true},
		{ID: 12, PlatformID: 2, PlatformCode: "grok", ModelPattern: "gpt-4o", EndpointCapabilities: []string{"responses"}, Enabled: true},
	})

	candidates, err := resolver.ListCandidates("gpt-4o")

	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, int64(2), candidates[0].PlatformID)
	require.Greater(t, candidates[0].MatchPriority, candidates[1].MatchPriority)
}
