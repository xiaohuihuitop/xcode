package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPlatformNotFound = infraerrors.NotFound("PLATFORM_NOT_FOUND", "platform not found")
	ErrPlatformExists   = infraerrors.Conflict("PLATFORM_EXISTS", "platform code already exists")
	ErrPlatformInUse    = infraerrors.Conflict("PLATFORM_IN_USE", "platform is still referenced")
	ErrPlatformInvalid  = infraerrors.BadRequest("INVALID_PLATFORM", "invalid platform configuration")
)

// PlatformRepository is deliberately small at the service boundary. The
// repository owns atomic persistence of a platform and its model rules.
type PlatformRepository interface {
	Create(ctx context.Context, platform *Platform) error
	List(ctx context.Context) ([]Platform, error)
	ListModelRules(ctx context.Context) ([]PlatformModelRule, error)
}

// PlatformManagementRepository is required only by administrator CRUD paths.
// Keeping it separate avoids expanding gateway-facing repository test doubles.
type PlatformManagementRepository interface {
	PlatformRepository
	GetByID(ctx context.Context, id int64) (*Platform, error)
	Update(ctx context.Context, platform *Platform) error
	PreviewDelete(ctx context.Context, id int64) (*PlatformDeleteImpact, error)
	DeleteControlled(ctx context.Context, id int64) (*PlatformDeleteResult, error)
}

type PlatformAccountOwnershipReader interface {
	HasAccountsByPlatformID(ctx context.Context, platformID int64) (bool, error)
}

// PlatformDeleteImpact summarizes blockers and data that a controlled platform
// deletion will permanently remove.
type PlatformDeleteImpact struct {
	Accounts  int64 `json:"accounts"`
	APIKeys   int64 `json:"api_keys"`
	UsageLogs int64 `json:"usage_logs"`
	Audits    int64 `json:"audits"`
	Ops       int64 `json:"ops"`
	Configs   int64 `json:"configs"`
	CanDelete bool  `json:"can_delete"`
}

type PlatformDeleteResult struct {
	PlatformID int64                `json:"platform_id"`
	Cleaned    PlatformDeleteImpact `json:"cleaned"`
}

// CreatePlatformInput contains all fields that must be decided at platform
// creation time. A platform is an account pool, never a billing asset.
type CreatePlatformInput struct {
	Code                 string
	Name                 string
	AccountPlatform      string
	Status               string
	EndpointCapabilities []string
	ModelRules           []PlatformModelRule
}

// UpdatePlatformInput contains only the editable platform fields. A nil field
// keeps its current value; ClearLegacyGroup explicitly removes the legacy
// compatibility reference.
type UpdatePlatformInput struct {
	Code                 *string
	Name                 *string
	AccountPlatform      *string
	Status               *string
	EndpointCapabilities *[]string
	ModelRules           *[]PlatformModelRule
}

// PlatformService validates the unique model-to-platform mapping before the
// repository makes it durable.
type PlatformService struct {
	repo PlatformRepository
}

func NewPlatformService(repo PlatformRepository) *PlatformService {
	return &PlatformService{repo: repo}
}

// List returns isolated platform configurations for administrator views. A
// disabled platform remains visible so it can be safely edited or re-enabled.
func (s *PlatformService) List(ctx context.Context) ([]Platform, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("%w: platform repository is required", ErrPlatformInvalid)
	}
	platforms, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list platforms: %w", err)
	}
	result := make([]Platform, len(platforms))
	for index := range platforms {
		result[index] = *clonePlatform(&platforms[index])
	}
	return result, nil
}

// GetByID returns one platform configuration, including disabled model rules,
// for administration. Request-time resolution only reads active rules.
func (s *PlatformService) GetByID(ctx context.Context, id int64) (*Platform, error) {
	if id <= 0 {
		return nil, ErrPlatformNotFound
	}
	repo, err := s.managementRepository()
	if err != nil {
		return nil, err
	}
	platform, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get platform: %w", err)
	}
	return clonePlatform(platform), nil
}

func (s *PlatformService) Create(ctx context.Context, input CreatePlatformInput) (*Platform, error) {
	platform, err := platformFromCreateInput(input)
	if err != nil {
		return nil, err
	}
	platform.ModelRules = bindPlatformToRules(platform.ModelRules, platform.ID, platform.Code, platform.EndpointCapabilities)

	if err := s.validateCandidate(ctx, platform, math.MaxInt64); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, platform); err != nil {
		return nil, fmt.Errorf("create platform: %w", err)
	}
	return platform, nil
}

// ResolveModelCandidates resolves against the current durable rule set. This
// avoids a stale in-process mapping after an administrator changes a platform.
func (s *PlatformService) ResolveModelCandidates(ctx context.Context, requestedModel string) ([]*ResolvedPlatformModel, error) {
	rules, err := s.repo.ListModelRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list platform model rules: %w", err)
	}
	return newPlatformModelResolver(rules).ListCandidates(requestedModel)
}

// ResolveModel remains an administrator-facing convenience for callers that
// need the highest-priority unfiltered candidate. Gateway requests use
// ResolveModelCandidates so authorization and endpoint filtering happen first.
func (s *PlatformService) ResolveModel(ctx context.Context, requestedModel string) (*ResolvedPlatformModel, error) {
	candidates, err := s.ResolveModelCandidates(ctx, requestedModel)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrPlatformModelNotFound
	}
	if len(candidates) > 1 {
		bestPriority := candidates[0].MatchPriority
		bestPlatformID := candidates[0].PlatformID
		for _, candidate := range candidates[1:] {
			if candidate.MatchPriority != bestPriority {
				break
			}
			if candidate.PlatformID != bestPlatformID {
				return nil, ErrPlatformModelAmbiguous
			}
		}
	}
	return candidates[0], nil
}

func (s *PlatformService) Update(ctx context.Context, id int64, input UpdatePlatformInput) (*Platform, error) {
	repo, err := s.managementRepository()
	if err != nil {
		return nil, err
	}
	current, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get platform: %w", err)
	}
	candidate := clonePlatform(current)
	if err := applyPlatformUpdate(candidate, input); err != nil {
		return nil, err
	}
	if !strings.EqualFold(current.AccountPlatform, candidate.AccountPlatform) {
		if ownership, ok := s.repo.(PlatformAccountOwnershipReader); ok {
			hasAccounts, ownershipErr := ownership.HasAccountsByPlatformID(ctx, id)
			if ownershipErr != nil {
				return nil, fmt.Errorf("check platform account ownership: %w", ownershipErr)
			}
			if hasAccounts {
				return nil, fmt.Errorf("%w: platform adapter cannot change while accounts are attached; create a new platform", ErrPlatformInvalid)
			}
		}
	}
	candidate.ModelRules = bindPlatformToRules(candidate.ModelRules, candidate.ID, candidate.Code, candidate.EndpointCapabilities)
	if err := s.validateCandidate(ctx, candidate, candidate.ID); err != nil {
		return nil, err
	}
	if err := repo.Update(ctx, candidate); err != nil {
		return nil, fmt.Errorf("update platform: %w", err)
	}
	return candidate, nil
}

func (s *PlatformService) PreviewDelete(ctx context.Context, id int64) (*PlatformDeleteImpact, error) {
	if id <= 0 {
		return nil, ErrPlatformNotFound
	}
	repo, err := s.managementRepository()
	if err != nil {
		return nil, err
	}
	impact, err := repo.PreviewDelete(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("preview platform delete: %w", err)
	}
	if impact == nil {
		return nil, fmt.Errorf("preview platform delete: empty impact")
	}
	result := *impact
	result.CanDelete = result.Accounts == 0 && result.APIKeys == 0
	return &result, nil
}

// Delete removes a platform after the repository atomically rechecks active
// blockers and clears only approved historical references.
func (s *PlatformService) Delete(ctx context.Context, id int64) (*PlatformDeleteResult, error) {
	if id <= 0 {
		return nil, ErrPlatformNotFound
	}
	repo, err := s.managementRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.DeleteControlled(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("delete platform: %w", err)
	}
	return result, nil
}

func (s *PlatformService) validateCandidate(ctx context.Context, platform *Platform, candidateID int64) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("%w: platform repository is required", ErrPlatformInvalid)
	}
	if platform.IsActive() && len(platform.EndpointCapabilities) == 0 {
		return fmt.Errorf("%w: active platform requires at least one endpoint capability", ErrPlatformInvalid)
	}
	if platform.IsActive() && !hasEnabledPlatformModelRule(platform.ModelRules) {
		return fmt.Errorf("%w: active platform requires at least one enabled model rule", ErrPlatformInvalid)
	}

	existing, err := s.repo.ListModelRules(ctx)
	if err != nil {
		return fmt.Errorf("list platform model rules: %w", err)
	}
	existing = excludePlatformRules(existing, candidateID)
	candidateRules := platformRulesForValidation(platform.ModelRules, candidateID, platform.Code, platform.EndpointCapabilities, platform.IsActive())
	rules := make([]PlatformModelRule, 0, len(existing)+len(candidateRules))
	rules = append(rules, existing...)
	rules = append(rules, candidateRules...)
	if err := validatePlatformModelRules(rules); err != nil {
		return err
	}
	return nil
}

func (s *PlatformService) managementRepository() (PlatformManagementRepository, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("%w: platform repository is required", ErrPlatformInvalid)
	}
	repo, ok := s.repo.(PlatformManagementRepository)
	if !ok {
		return nil, fmt.Errorf("%w: platform repository does not support management", ErrPlatformInvalid)
	}
	return repo, nil
}

func platformFromCreateInput(input CreatePlatformInput) (*Platform, error) {
	code, err := normalizePlatformCode(input.Code)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: platform name is required", ErrPlatformInvalid)
	}
	accountPlatform, err := normalizePlatformAccountPlatform(input.AccountPlatform)
	if err != nil {
		return nil, err
	}
	status, err := normalizePlatformStatus(input.Status)
	if err != nil {
		return nil, err
	}
	endpointCapabilities := normalizeEndpointCapabilities(input.EndpointCapabilities)
	if status == PlatformStatusActive && len(endpointCapabilities) == 0 {
		return nil, fmt.Errorf("%w: active platform requires at least one endpoint capability", ErrPlatformInvalid)
	}

	return &Platform{
		Code:                 code,
		Name:                 name,
		AccountPlatform:      accountPlatform,
		Status:               status,
		EndpointCapabilities: endpointCapabilities,
		ModelRules:           clonePlatformModelRules(input.ModelRules),
	}, nil
}

func applyPlatformUpdate(platform *Platform, input UpdatePlatformInput) error {
	if input.Code != nil {
		code, err := normalizePlatformCode(*input.Code)
		if err != nil {
			return err
		}
		platform.Code = code
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return fmt.Errorf("%w: platform name is required", ErrPlatformInvalid)
		}
		platform.Name = name
	}
	if input.AccountPlatform != nil {
		accountPlatform, err := normalizePlatformAccountPlatform(*input.AccountPlatform)
		if err != nil {
			return err
		}
		platform.AccountPlatform = accountPlatform
	}
	if input.Status != nil {
		status, err := normalizePlatformStatus(*input.Status)
		if err != nil {
			return err
		}
		platform.Status = status
	}
	if input.EndpointCapabilities != nil {
		platform.EndpointCapabilities = normalizeEndpointCapabilities(*input.EndpointCapabilities)
	}
	if input.ModelRules != nil {
		platform.ModelRules = clonePlatformModelRules(*input.ModelRules)
	}
	return nil
}

func normalizePlatformStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" {
		return PlatformStatusActive, nil
	}
	if status != PlatformStatusActive && status != StatusDisabled {
		return "", fmt.Errorf("%w: unsupported platform status %q", ErrPlatformInvalid, raw)
	}
	return status, nil
}

func normalizePlatformCode(raw string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" || len(code) > 50 {
		return "", fmt.Errorf("%w: platform code is required and must be at most 50 characters", ErrPlatformInvalid)
	}
	for index, char := range code {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return "", fmt.Errorf("%w: platform code contains an invalid character at index %d", ErrPlatformInvalid, index)
	}
	return code, nil
}

func normalizePlatformAccountPlatform(raw string) (string, error) {
	platform := strings.ToLower(strings.TrimSpace(raw))
	if platform == "" || len(platform) > 50 {
		return "", fmt.Errorf("%w: account platform is required and must be at most 50 characters", ErrPlatformInvalid)
	}
	switch platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok:
		return platform, nil
	default:
		return "", fmt.Errorf("%w: unsupported account platform %q", ErrPlatformInvalid, raw)
	}
}

func hasEnabledPlatformModelRule(rules []PlatformModelRule) bool {
	for _, rule := range rules {
		if rule.Enabled && strings.TrimSpace(rule.ModelPattern) != "" {
			return true
		}
	}
	return false
}

func platformRulesForValidation(rules []PlatformModelRule, platformID int64, platformCode string, endpoints []string, enabled bool) []PlatformModelRule {
	cloned := bindPlatformToRules(rules, platformID, platformCode, endpoints)
	for index := range cloned {
		cloned[index].Enabled = cloned[index].Enabled && enabled
	}
	return cloned
}

func bindPlatformToRules(rules []PlatformModelRule, platformID int64, platformCode string, endpoints []string) []PlatformModelRule {
	cloned := clonePlatformModelRules(rules)
	for index := range cloned {
		cloned[index].PlatformID = platformID
		cloned[index].PlatformCode = platformCode
		cloned[index].EndpointCapabilities = append([]string(nil), endpoints...)
	}
	return cloned
}

func excludePlatformRules(rules []PlatformModelRule, platformID int64) []PlatformModelRule {
	if platformID <= 0 {
		return append([]PlatformModelRule(nil), rules...)
	}
	filtered := make([]PlatformModelRule, 0, len(rules))
	for index := range rules {
		if rules[index].PlatformID != platformID {
			filtered = append(filtered, rules[index])
		}
	}
	return filtered
}

func clonePlatformModelRules(rules []PlatformModelRule) []PlatformModelRule {
	cloned := make([]PlatformModelRule, len(rules))
	copy(cloned, rules)
	for index := range cloned {
		cloned[index].EndpointCapabilities = append([]string(nil), rules[index].EndpointCapabilities...)
	}
	return cloned
}

func clonePlatform(platform *Platform) *Platform {
	if platform == nil {
		return nil
	}
	cloned := *platform
	cloned.ModelRules = clonePlatformModelRules(platform.ModelRules)
	cloned.EndpointCapabilities = append([]string(nil), platform.EndpointCapabilities...)
	if platform.SchedulingConfig != nil {
		cloned.SchedulingConfig = make(map[string]any, len(platform.SchedulingConfig))
		for key, value := range platform.SchedulingConfig {
			cloned.SchedulingConfig[key] = value
		}
	}
	return &cloned
}

func clonePlatformInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
