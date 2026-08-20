package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestPlatformServiceListAuthorizedModels(t *testing.T) {
	repo := &platformRepositoryStub{allRules: []PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-5.6", Enabled: true},
		{PlatformID: 1, ModelPattern: "gpt-*", Enabled: true},
		{PlatformID: 2, ModelPattern: "glm-4.6", Enabled: true},
		{PlatformID: 2, ModelPattern: " glm-4.6 ", Enabled: true},
		{PlatformID: 3, ModelPattern: "grok-4", Enabled: true},
		{PlatformID: 2, ModelPattern: "disabled-model", Enabled: false},
	}}

	models, err := NewPlatformService(repo).ListAuthorizedModels(context.Background(), []int64{1, 2})

	require.NoError(t, err)
	require.Equal(t, []string{"glm-4.6", "gpt-5.6"}, models)
}

func TestPlatformServiceListAuthorizedModelsRequiresPlatformAuthorization(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).ListAuthorizedModels(context.Background(), nil)

	require.ErrorIs(t, err, ErrAPIKeyPlatformForbidden)
}

type platformRepositoryStub struct {
	allRules    []PlatformModelRule
	platforms   []Platform
	created     *Platform
	hasAccounts bool
}

func (r *platformRepositoryStub) Create(_ context.Context, platform *Platform) error {
	r.created = platform
	return nil
}

func (r *platformRepositoryStub) ListModelRules(context.Context) ([]PlatformModelRule, error) {
	return append([]PlatformModelRule(nil), r.allRules...), nil
}

func (r *platformRepositoryStub) List(context.Context) ([]Platform, error) {
	return append([]Platform(nil), r.platforms...), nil
}

func (r *platformRepositoryStub) HasAccountsByPlatformID(context.Context, int64) (bool, error) {
	return r.hasAccounts, nil
}

type platformManagementRepositoryStub struct {
	platformRepositoryStub
	platform     *Platform
	updated      *Platform
	deleteImpact *PlatformDeleteImpact
	deleteResult *PlatformDeleteResult
	previewedID  int64
	deletedID    int64
	deleteErr    error
}

func (r *platformManagementRepositoryStub) GetByID(context.Context, int64) (*Platform, error) {
	return r.platform, nil
}

func (r *platformManagementRepositoryStub) Update(_ context.Context, platform *Platform) error {
	r.updated = platform
	return nil
}

func (r *platformManagementRepositoryStub) PreviewDelete(_ context.Context, id int64) (*PlatformDeleteImpact, error) {
	r.previewedID = id
	return r.deleteImpact, nil
}

func (r *platformManagementRepositoryStub) DeleteControlled(_ context.Context, id int64) (*PlatformDeleteResult, error) {
	r.deletedID = id
	return r.deleteResult, r.deleteErr
}

func TestPlatformServiceDeleteUsesAtomicRepositoryOperation(t *testing.T) {
	repo := &platformManagementRepositoryStub{deleteResult: &PlatformDeleteResult{
		PlatformID: 17,
		Cleaned:    PlatformDeleteImpact{UsageLogs: 3, Ops: 2, CanDelete: true},
	}}

	result, err := NewPlatformService(repo).Delete(context.Background(), 17)

	require.NoError(t, err)
	require.Equal(t, int64(17), repo.deletedID)
	require.Equal(t, repo.deleteResult, result)
}

func TestPlatformServiceDeleteRejectsInvalidID(t *testing.T) {
	_, err := NewPlatformService(&platformManagementRepositoryStub{}).Delete(context.Background(), 0)

	require.ErrorIs(t, err, ErrPlatformNotFound)
}

func TestPlatformServiceDeletePreservesInUseError(t *testing.T) {
	repo := &platformManagementRepositoryStub{
		deleteErr: ErrPlatformInUse.WithMetadata(map[string]string{"accounts": "1"}),
	}

	_, err := NewPlatformService(repo).Delete(context.Background(), 17)

	require.ErrorIs(t, err, ErrPlatformInUse)
	require.Equal(t, "1", infraerrors.FromError(err).Metadata["accounts"])
}

func TestPlatformServicePreviewDeleteReturnsIndependentImpact(t *testing.T) {
	repo := &platformManagementRepositoryStub{deleteImpact: &PlatformDeleteImpact{
		UsageLogs: 12,
		Audits:    4,
		Ops:       7,
		CanDelete: true,
	}}

	impact, err := NewPlatformService(repo).PreviewDelete(context.Background(), 17)

	require.NoError(t, err)
	require.Equal(t, int64(17), repo.previewedID)
	require.Equal(t, int64(12), impact.UsageLogs)
	impact.UsageLogs = 99
	require.Equal(t, int64(12), repo.deleteImpact.UsageLogs)
}

func TestPlatformServicePreviewDeleteRejectsInvalidID(t *testing.T) {
	_, err := NewPlatformService(&platformManagementRepositoryStub{}).PreviewDelete(context.Background(), 0)

	require.ErrorIs(t, err, ErrPlatformNotFound)
}

func TestPlatformServiceCreateAllowsCrossPlatformModelOverlap(t *testing.T) {
	repo := &platformRepositoryStub{
		allRules: []PlatformModelRule{{
			PlatformID:           1,
			PlatformCode:         PlatformOpenAI,
			ModelPattern:         "gpt-*",
			EndpointCapabilities: []string{"chat_completions"},
			Enabled:              true,
		}},
	}
	svc := NewPlatformService(repo)

	_, err := svc.Create(context.Background(), CreatePlatformInput{
		Code:                 "grok",
		Name:                 "Grok",
		AccountPlatform:      PlatformOpenAI,
		EndpointCapabilities: []string{"responses"},
		ModelRules: []PlatformModelRule{{
			ModelPattern:         "gpt-4o",
			EndpointCapabilities: []string{"responses"},
			Enabled:              true,
		}},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
}

func TestPlatformServiceCreateBindsPlatformEndpointsToRules(t *testing.T) {
	repo := &platformRepositoryStub{}

	created, err := NewPlatformService(repo).Create(context.Background(), CreatePlatformInput{
		Code:                 "openai-primary",
		Name:                 "OpenAI Primary",
		AccountPlatform:      PlatformOpenAI,
		Status:               PlatformStatusActive,
		EndpointCapabilities: []string{"responses", "chat_completions", "responses"},
		ModelRules: []PlatformModelRule{{
			ModelPattern:  "gpt-5.6",
			UpstreamModel: "gpt-5.6",
			Enabled:       true,
		}},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"chat_completions", "responses"}, created.EndpointCapabilities)
	require.Equal(t, created.EndpointCapabilities, created.ModelRules[0].EndpointCapabilities)
}

func TestPlatformServiceRejectsActivePlatformWithoutEndpoints(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).Create(context.Background(), CreatePlatformInput{
		Code:            "openai-primary",
		Name:            "OpenAI Primary",
		AccountPlatform: PlatformOpenAI,
		Status:          PlatformStatusActive,
		ModelRules:      []PlatformModelRule{{ModelPattern: "gpt-5.6", Enabled: true}},
	})

	require.ErrorIs(t, err, ErrPlatformInvalid)
}

func TestPlatformServiceRejectsActivePlatformWithoutModels(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).Create(context.Background(), CreatePlatformInput{
		Code:                 "openai-primary",
		Name:                 "OpenAI Primary",
		AccountPlatform:      PlatformOpenAI,
		Status:               PlatformStatusActive,
		EndpointCapabilities: []string{"chat_completions"},
	})

	require.ErrorIs(t, err, ErrPlatformInvalid)
}

func TestPlatformServiceRejectsUnknownAccountPlatform(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).Create(context.Background(), CreatePlatformInput{
		Code:                 "custom",
		Name:                 "Custom",
		AccountPlatform:      "unsupported",
		Status:               PlatformStatusActive,
		EndpointCapabilities: []string{"chat_completions"},
		ModelRules:           []PlatformModelRule{{ModelPattern: "custom-model", Enabled: true}},
	})

	require.ErrorIs(t, err, ErrPlatformInvalid)
}

func TestPlatformServiceResolveModelUsesActiveRepositoryRules(t *testing.T) {
	repo := &platformRepositoryStub{
		allRules: []PlatformModelRule{{
			ID:                   22,
			PlatformID:           2,
			PlatformCode:         PlatformOpenAI,
			ModelPattern:         "gpt-4o",
			UpstreamModel:        "gpt-4o-2024-08-06",
			EndpointCapabilities: []string{"chat_completions"},
			Enabled:              true,
		}},
	}
	svc := NewPlatformService(repo)

	resolved, err := svc.ResolveModel(context.Background(), "gpt-4o")

	require.NoError(t, err)
	require.Equal(t, int64(2), resolved.PlatformID)
	require.Equal(t, PlatformOpenAI, resolved.PlatformCode)
	require.Equal(t, "gpt-4o-2024-08-06", resolved.UpstreamModel)
}

func TestPlatformServiceResolveModelReturnsNotFoundWithoutCandidates(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).ResolveModel(context.Background(), "unknown-model")

	require.ErrorIs(t, err, ErrPlatformModelNotFound)
}

func TestPlatformServiceUpdateReplacesOwnRuleWithoutSelfConflict(t *testing.T) {
	repo := &platformManagementRepositoryStub{
		platformRepositoryStub: platformRepositoryStub{
			allRules: []PlatformModelRule{{
				PlatformID:           1,
				PlatformCode:         PlatformOpenAI,
				ModelPattern:         "gpt-*",
				EndpointCapabilities: []string{"chat_completions"},
				Enabled:              true,
			}},
		},
		platform: &Platform{
			ID:                   1,
			Code:                 PlatformOpenAI,
			Name:                 "OpenAI",
			EndpointCapabilities: []string{"chat_completions"},
			Status:               PlatformStatusActive,
			ModelRules: []PlatformModelRule{{
				PlatformID:   1,
				PlatformCode: PlatformOpenAI,
				ModelPattern: "gpt-*",
				Enabled:      true,
			}},
		},
	}
	svc := NewPlatformService(repo)
	rules := []PlatformModelRule{{ModelPattern: "gpt-*", EndpointCapabilities: []string{"chat_completions"}, Enabled: true}}

	updated, err := svc.Update(context.Background(), 1, UpdatePlatformInput{ModelRules: &rules})

	require.NoError(t, err)
	require.Equal(t, int64(1), updated.ID)
	require.NotNil(t, repo.updated)
	require.Equal(t, int64(1), repo.updated.ModelRules[0].PlatformID)
	require.Equal(t, PlatformOpenAI, repo.updated.ModelRules[0].PlatformCode)
}

func TestPlatformServiceUpdateRejectsAdapterChangeWhenAccountsExist(t *testing.T) {
	repo := &platformManagementRepositoryStub{
		platformRepositoryStub: platformRepositoryStub{hasAccounts: true},
		platform:               &Platform{ID: 1, Code: "openai-main", Name: "OpenAI", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive},
	}

	anthropic := PlatformAnthropic
	_, err := NewPlatformService(repo).Update(context.Background(), 1, UpdatePlatformInput{AccountPlatform: &anthropic})

	require.ErrorIs(t, err, ErrPlatformInvalid)
	require.Nil(t, repo.updated)
}

func TestPlatformServiceListReturnsIndependentPlatformCopies(t *testing.T) {
	repo := &platformRepositoryStub{platforms: []Platform{{
		ID:              7,
		Code:            "gpt",
		Name:            "GPT",
		AccountPlatform: PlatformOpenAI,
		Status:          PlatformStatusActive,
		ModelRules: []PlatformModelRule{{
			ID:           11,
			ModelPattern: "gpt-4o",
			Enabled:      true,
		}},
	}}}

	platforms, err := NewPlatformService(repo).List(context.Background())

	require.NoError(t, err)
	require.Len(t, platforms, 1)
	platforms[0].Name = "mutated"
	platforms[0].ModelRules[0].ModelPattern = "mutated"
	require.Equal(t, "GPT", repo.platforms[0].Name)
	require.Equal(t, "gpt-4o", repo.platforms[0].ModelRules[0].ModelPattern)
}
