package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	PricingSourceOverride    = "override"
	ModelPricingStatusActive = "active"
)

var (
	ErrModelPricingOverrideNotFound        = errors.New("model pricing override not found")
	ErrModelPricingOverrideConflict        = errors.New("model pricing override already exists")
	ErrModelPricingPlatformModelNotEnabled = infraerrors.BadRequest(
		"MODEL_PRICING_PLATFORM_MODEL_NOT_ENABLED",
		"model is not enabled on the selected platform",
	)
)

// ModelPricingOverride is the service representation of an administrator
// price rule. It is deliberately independent from Group and Channel.
type ModelPricingOverride struct {
	ID               int64
	Adapter          string
	ModelPattern     string
	BillingMode      BillingMode
	InputPrice       *float64
	OutputPrice      *float64
	CacheWritePrice  *float64
	CacheReadPrice   *float64
	ImageInputPrice  *float64
	ImageOutputPrice *float64
	PerRequestPrice  *float64
	Intervals        []domain.ModelPricingInterval
	Status           string
}

// ModelPricingOverrideRepository is the persistence boundary for the price
// catalog. Keeping it narrow makes resolver tests independent of a database.
type ModelPricingOverrideRepository interface {
	List(ctx context.Context, adapter string) ([]ModelPricingOverride, error)
	Get(ctx context.Context, id int64) (*ModelPricingOverride, error)
	Create(ctx context.Context, override *ModelPricingOverride) error
	Update(ctx context.Context, override *ModelPricingOverride) error
	Upsert(ctx context.Context, override *ModelPricingOverride) error
	Delete(ctx context.Context, id int64) error
}

// ModelPricingCatalog resolves the most specific active rule for an adapter
// and model. Exact patterns outrank wildcards; ties are deterministic.
type ModelPricingCatalog struct {
	repo ModelPricingOverrideRepository
}

type ModelPricingRuleSnapshot struct {
	rulesByAdapter map[string][]ModelPricingOverride
}

func NewModelPricingCatalog(repo ModelPricingOverrideRepository) *ModelPricingCatalog {
	return &ModelPricingCatalog{repo: repo}
}

func (c *ModelPricingCatalog) List(ctx context.Context, adapter string) ([]ModelPricingOverride, error) {
	if c == nil || c.repo == nil {
		return []ModelPricingOverride{}, nil
	}
	return c.repo.List(ctx, adapter)
}

func (c *ModelPricingCatalog) Get(ctx context.Context, id int64) (*ModelPricingOverride, error) {
	if c == nil || c.repo == nil {
		return nil, ErrModelPricingOverrideNotFound
	}
	return c.repo.Get(ctx, id)
}

func (c *ModelPricingCatalog) Create(ctx context.Context, input ModelPricingOverride) (*ModelPricingOverride, error) {
	if err := validateModelPricingOverride(&input); err != nil {
		return nil, err
	}
	if err := c.repo.Create(ctx, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

func (c *ModelPricingCatalog) Update(ctx context.Context, id int64, input ModelPricingOverride) (*ModelPricingOverride, error) {
	input.ID = id
	if err := validateModelPricingOverride(&input); err != nil {
		return nil, err
	}
	if err := c.repo.Update(ctx, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

func (c *ModelPricingCatalog) Delete(ctx context.Context, id int64) error {
	if c == nil || c.repo == nil {
		return ErrModelPricingOverrideNotFound
	}
	return c.repo.Delete(ctx, id)
}

func (c *ModelPricingCatalog) GetExact(ctx context.Context, adapter, modelPattern string) (*ModelPricingOverride, error) {
	if c == nil || c.repo == nil {
		return nil, nil
	}
	adapter = strings.ToLower(strings.TrimSpace(adapter))
	modelPattern = strings.TrimSpace(modelPattern)
	if adapter == "" || modelPattern == "" {
		return nil, nil
	}
	rules, err := c.repo.List(ctx, adapter)
	if err != nil {
		return nil, fmt.Errorf("list model pricing overrides: %w", err)
	}
	for i := range rules {
		if strings.EqualFold(strings.TrimSpace(rules[i].Status), ModelPricingStatusActive) &&
			strings.EqualFold(strings.TrimSpace(rules[i].Adapter), adapter) &&
			strings.EqualFold(strings.TrimSpace(rules[i].ModelPattern), modelPattern) {
			result := rules[i]
			return &result, nil
		}
	}
	return nil, nil
}

func (c *ModelPricingCatalog) UpsertPlatformSale(
	ctx context.Context,
	platform *Platform,
	modelPattern string,
	input ModelPricingOverride,
) (*ModelPricingOverride, error) {
	if c == nil || c.repo == nil {
		return nil, fmt.Errorf("model pricing repository is required")
	}
	modelPattern = strings.TrimSpace(modelPattern)
	if platform == nil || platform.ID <= 0 || strings.TrimSpace(platform.Code) == "" || !platform.IsActive() {
		return nil, ErrModelPricingPlatformModelNotEnabled
	}
	canonicalModelPattern := ""
	for i := range platform.ModelRules {
		if platform.ModelRules[i].Enabled && strings.EqualFold(strings.TrimSpace(platform.ModelRules[i].ModelPattern), modelPattern) {
			canonicalModelPattern = strings.TrimSpace(platform.ModelRules[i].ModelPattern)
			break
		}
	}
	if canonicalModelPattern == "" {
		return nil, ErrModelPricingPlatformModelNotEnabled
	}
	existing, err := c.GetExact(ctx, platform.Code, canonicalModelPattern)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		canonicalModelPattern = strings.TrimSpace(existing.ModelPattern)
	}
	input.ID = 0
	input.Adapter = platform.Code
	input.ModelPattern = canonicalModelPattern
	if err := validateModelPricingOverride(&input); err != nil {
		return nil, err
	}
	if err := c.repo.Upsert(ctx, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

func validateModelPricingOverride(item *ModelPricingOverride) error {
	if item == nil {
		return fmt.Errorf("model pricing override is nil")
	}
	item.Adapter = strings.ToLower(strings.TrimSpace(item.Adapter))
	item.ModelPattern = strings.TrimSpace(item.ModelPattern)
	if item.Adapter == "" || item.ModelPattern == "" {
		return fmt.Errorf("adapter and model_pattern are required")
	}
	if item.BillingMode == "" {
		item.BillingMode = BillingModeToken
	}
	if !item.BillingMode.IsValid() || item.BillingMode == BillingModeVideo {
		return fmt.Errorf("unsupported billing_mode: %s", item.BillingMode)
	}
	if item.Status == "" {
		item.Status = ModelPricingStatusActive
	}
	item.Status = strings.ToLower(strings.TrimSpace(item.Status))
	if item.Status != ModelPricingStatusActive && item.Status != "disabled" {
		return fmt.Errorf("unsupported status: %s", item.Status)
	}
	for _, price := range []*float64{item.InputPrice, item.OutputPrice, item.CacheWritePrice, item.CacheReadPrice, item.ImageInputPrice, item.ImageOutputPrice, item.PerRequestPrice} {
		if price != nil && *price < 0 {
			return fmt.Errorf("pricing values must be non-negative")
		}
	}
	if item.Intervals == nil {
		item.Intervals = []domain.ModelPricingInterval{}
	}
	intervals := make([]PricingInterval, 0, len(item.Intervals))
	for _, interval := range item.Intervals {
		intervals = append(intervals, domainIntervalToPricingInterval(interval))
	}
	if err := ValidateIntervals(intervals, item.BillingMode); err != nil {
		return err
	}
	return nil
}

func (c *ModelPricingCatalog) LoadSnapshot(ctx context.Context) (*ModelPricingRuleSnapshot, error) {
	snapshot := &ModelPricingRuleSnapshot{rulesByAdapter: make(map[string][]ModelPricingOverride)}
	if c == nil || c.repo == nil {
		return snapshot, nil
	}
	rules, err := c.repo.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list model pricing overrides: %w", err)
	}
	for _, rule := range rules {
		adapter := strings.ToLower(strings.TrimSpace(rule.Adapter))
		if adapter == "" {
			continue
		}
		snapshot.rulesByAdapter[adapter] = append(snapshot.rulesByAdapter[adapter], rule)
	}
	return snapshot, nil
}

func (s *ModelPricingRuleSnapshot) ResolveForPricingInput(input PricingInput) *ModelPricingOverride {
	if s == nil {
		return nil
	}
	for _, identity := range pricingIdentitiesForInput(input) {
		if override := resolveModelPricingRules(s.rulesByAdapter[identity.adapter], identity.model); override != nil {
			return override
		}
	}
	return nil
}

func (c *ModelPricingCatalog) Resolve(ctx context.Context, adapter, model string) (*ModelPricingOverride, error) {
	if c == nil || c.repo == nil {
		return nil, nil
	}
	adapter = strings.ToLower(strings.TrimSpace(adapter))
	model = strings.TrimSpace(model)
	if adapter == "" || model == "" {
		return nil, nil
	}
	rules, err := c.repo.List(ctx, adapter)
	if err != nil {
		return nil, fmt.Errorf("list model pricing overrides: %w", err)
	}
	return resolveModelPricingRules(rules, model), nil
}

func resolveModelPricingRules(rules []ModelPricingOverride, model string) *ModelPricingOverride {
	modelLower := strings.ToLower(model)
	matches := make([]ModelPricingOverride, 0, len(rules))
	for _, rule := range rules {
		if strings.ToLower(strings.TrimSpace(rule.Status)) != ModelPricingStatusActive {
			continue
		}
		if modelPatternMatches(rule.ModelPattern, modelLower) {
			matches = append(matches, rule)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		iExact := strings.EqualFold(strings.TrimSpace(matches[i].ModelPattern), model)
		jExact := strings.EqualFold(strings.TrimSpace(matches[j].ModelPattern), model)
		if iExact != jExact {
			return iExact
		}
		iSpecificity := patternSpecificity(matches[i].ModelPattern)
		jSpecificity := patternSpecificity(matches[j].ModelPattern)
		if iSpecificity != jSpecificity {
			return iSpecificity > jSpecificity
		}
		return strings.ToLower(matches[i].ModelPattern) < strings.ToLower(matches[j].ModelPattern)
	})
	selected := matches[0]
	return &selected
}

// ResolveForPricingInput resolves platform-specific rules before legacy
// adapter rules. Platform codes distinguish pools such as codex and glm even
// when both pools use the same account adapter.
func (c *ModelPricingCatalog) ResolveForPricingInput(ctx context.Context, input PricingInput) (*ModelPricingOverride, error) {
	identities := pricingIdentitiesForInput(input)
	if c == nil || c.repo == nil {
		return nil, nil
	}
	rulesByAdapter := make(map[string][]ModelPricingOverride, len(identities))
	for _, identity := range identities {
		rules, loaded := rulesByAdapter[identity.adapter]
		if !loaded {
			var err error
			rules, err = c.repo.List(ctx, identity.adapter)
			if err != nil {
				return nil, fmt.Errorf("list model pricing overrides: %w", err)
			}
			rulesByAdapter[identity.adapter] = rules
		}
		override := resolveModelPricingRules(rules, identity.model)
		if override != nil {
			return override, nil
		}
	}
	return nil, nil
}

type pricingIdentity struct {
	adapter string
	model   string
}

func pricingIdentitiesForInput(input PricingInput) []pricingIdentity {
	identities := make([]pricingIdentity, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(adapter, model string) {
		adapter = strings.ToLower(strings.TrimSpace(adapter))
		model = strings.TrimSpace(model)
		if adapter == "" || model == "" {
			return
		}
		key := adapter + "\x00" + strings.ToLower(model)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		identities = append(identities, pricingIdentity{adapter: adapter, model: model})
	}
	add(input.PlatformCode, input.PublicModel)
	add(input.PlatformCode, input.Model)
	add(input.Adapter, input.PublicModel)
	add(input.Adapter, input.Model)
	return identities
}

func modelPatternMatches(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = strings.ToLower(strings.TrimSpace(model))
	if pattern == "" || model == "" {
		return false
	}
	if pattern == model {
		return true
	}
	matched, err := path.Match(pattern, model)
	return err == nil && matched
}

func patternSpecificity(pattern string) int {
	return len(strings.NewReplacer("*", "", "?", "").Replace(strings.TrimSpace(pattern)))
}

func pricingOverrideToResolved(
	override *ModelPricingOverride,
	base *ModelPricing,
	officialSource PricingSourceInfo,
) *ResolvedPricing {
	matchedOverride := *override
	matchedOverride.Intervals = append([]domain.ModelPricingInterval(nil), override.Intervals...)
	resolved := &ResolvedPricing{
		Mode:                   override.BillingMode,
		BasePricing:            cloneModelPricing(base),
		OfficialPricing:        cloneModelPricing(base),
		OfficialSource:         officialSource,
		MatchedOverride:        &matchedOverride,
		Source:                 PricingSourceOverride,
		SupportsCacheBreakdown: base != nil && base.SupportsCacheBreakdown,
	}
	if resolved.Mode == "" {
		resolved.Mode = BillingModeToken
	}
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	}
	applyOverridePrices(override, resolved.BasePricing)
	resolved.Intervals = make([]PricingInterval, 0, len(override.Intervals))
	for _, interval := range override.Intervals {
		resolved.Intervals = append(resolved.Intervals, domainIntervalToPricingInterval(interval))
	}
	resolved.Intervals = filterValidIntervals(resolved.Intervals)
	if override.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *override.PerRequestPrice
		resolved.DefaultPerRequestPriceExplicit = true
	}
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage {
		resolved.RequestTiers = append([]PricingInterval(nil), resolved.Intervals...)
	}
	return resolved
}

func cloneModelPricing(pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	clone := *pricing
	return &clone
}

func applyOverridePrices(override *ModelPricingOverride, pricing *ModelPricing) {
	if override.InputPrice != nil {
		pricing.InputPricePerToken = *override.InputPrice
		pricing.InputPricePerTokenPriority = *override.InputPrice
		pricing.InputPriceExplicit = true
	}
	if override.OutputPrice != nil {
		pricing.OutputPricePerToken = *override.OutputPrice
		pricing.OutputPricePerTokenPriority = *override.OutputPrice
		pricing.OutputPriceExplicit = true
	}
	if override.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *override.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *override.CacheWritePrice
		pricing.CacheCreation5mPrice = *override.CacheWritePrice
		pricing.CacheCreation1hPrice = *override.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
	}
	if override.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *override.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *override.CacheReadPrice
		pricing.CacheReadPriceExplicit = true
	}
	if override.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *override.ImageInputPrice
		pricing.ImageInputPriceExplicit = true
	}
	if override.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *override.ImageOutputPrice
		pricing.ImageOutputPriceExplicit = true
	}
}

func domainIntervalToPricingInterval(interval domain.ModelPricingInterval) PricingInterval {
	return PricingInterval{
		MinTokens:       interval.MinTokens,
		MaxTokens:       interval.MaxTokens,
		TierLabel:       interval.TierLabel,
		InputPrice:      interval.InputPrice,
		OutputPrice:     interval.OutputPrice,
		CacheWritePrice: interval.CacheWritePrice,
		CacheReadPrice:  interval.CacheReadPrice,
		PerRequestPrice: interval.PerRequestPrice,
		SortOrder:       interval.SortOrder,
	}
}
