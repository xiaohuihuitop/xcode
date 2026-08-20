package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	PricingSourceOverride    = "override"
	ModelPricingStatusActive = "active"
)

var (
	ErrModelPricingOverrideNotFound = errors.New("model pricing override not found")
	ErrModelPricingOverrideConflict = errors.New("model pricing override already exists")
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
	Delete(ctx context.Context, id int64) error
}

// ModelPricingCatalog resolves the most specific active rule for an adapter
// and model. Exact patterns outrank wildcards; ties are deterministic.
type ModelPricingCatalog struct {
	repo ModelPricingOverrideRepository
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
		return nil, nil
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
	return &selected, nil
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

func pricingOverrideToResolved(override *ModelPricingOverride, base *ModelPricing) *ResolvedPricing {
	resolved := &ResolvedPricing{
		Mode:                   override.BillingMode,
		BasePricing:            cloneModelPricing(base),
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
	}
	if override.OutputPrice != nil {
		pricing.OutputPricePerToken = *override.OutputPrice
		pricing.OutputPricePerTokenPriority = *override.OutputPrice
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
	}
	if override.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *override.ImageInputPrice
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
