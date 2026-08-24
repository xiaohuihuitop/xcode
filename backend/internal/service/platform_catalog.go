package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// PlatformCatalogModel is the public, platform-owned model view. It contains
// only the rule that an administrator enabled on a Platform and an optional
// reference price resolved by the adapter/model pricing catalog.
type PlatformCatalogModel struct {
	Pattern              string           `json:"pattern"`
	UpstreamModel        string           `json:"upstream_model,omitempty"`
	EndpointCapabilities []string         `json:"endpoint_capabilities"`
	Pricing              *ResolvedPricing `json:"pricing,omitempty"`
}

// PlatformCatalogPlatform is shared by the user available-platform page and
// the model plaza. No Group, Channel, account or billing multiplier crosses
// this boundary.
type PlatformCatalogPlatform struct {
	ID                   int64                  `json:"id"`
	Code                 string                 `json:"code"`
	Name                 string                 `json:"name"`
	AccountPlatform      string                 `json:"account_platform"`
	EndpointCapabilities []string               `json:"endpoint_capabilities"`
	Models               []PlatformCatalogModel `json:"models"`
}

// PlatformPricingResolver resolves one request-scoped catalog against a
// single pricing-rule snapshot.
type PlatformPricingResolver interface {
	ResolveBatch(ctx context.Context, inputs []PricingInput) ([]*ResolvedPricing, error)
}

type PlatformCatalogRepository interface {
	List(ctx context.Context) ([]Platform, error)
}

// PlatformCatalogService builds user-facing catalog views from Platform
// rules. PlatformRepository is intentionally the only data source for pools.
type PlatformCatalogService struct {
	repo    PlatformCatalogRepository
	pricing PlatformPricingResolver
}

func NewPlatformCatalogService(repo PlatformCatalogRepository, pricing PlatformPricingResolver) *PlatformCatalogService {
	return &PlatformCatalogService{repo: repo, pricing: pricing}
}

func (s *PlatformCatalogService) ListAvailable(ctx context.Context) ([]PlatformCatalogPlatform, error) {
	return s.list(ctx, false)
}

func (s *PlatformCatalogService) ListPlaza(ctx context.Context) ([]PlatformCatalogPlatform, error) {
	return s.list(ctx, true)
}

func (s *PlatformCatalogService) ListPricingCatalog(ctx context.Context) ([]PlatformCatalogPlatform, error) {
	return s.list(ctx, true)
}

func (s *PlatformCatalogService) list(ctx context.Context, withPricing bool) ([]PlatformCatalogPlatform, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("platform catalog repository is required")
	}
	platforms, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list platform catalog: %w", err)
	}
	out := make([]PlatformCatalogPlatform, 0, len(platforms))
	for i := range platforms {
		platform := platforms[i]
		if !platform.IsActive() {
			continue
		}
		item := PlatformCatalogPlatform{
			ID:                   platform.ID,
			Code:                 platform.Code,
			Name:                 platform.Name,
			AccountPlatform:      platform.AccountPlatform,
			EndpointCapabilities: append([]string(nil), platform.EndpointCapabilities...),
			Models:               make([]PlatformCatalogModel, 0, len(platform.ModelRules)),
		}
		if item.EndpointCapabilities == nil {
			item.EndpointCapabilities = []string{}
		}
		for j := range platform.ModelRules {
			rule := platform.ModelRules[j]
			if !rule.Enabled || strings.TrimSpace(rule.ModelPattern) == "" {
				continue
			}
			model := PlatformCatalogModel{
				Pattern:              strings.TrimSpace(rule.ModelPattern),
				UpstreamModel:        strings.TrimSpace(rule.UpstreamModel),
				EndpointCapabilities: append([]string(nil), rule.EndpointCapabilities...),
			}
			if len(model.EndpointCapabilities) == 0 {
				model.EndpointCapabilities = append([]string(nil), platform.EndpointCapabilities...)
			}
			if model.EndpointCapabilities == nil {
				model.EndpointCapabilities = []string{}
			}
			item.Models = append(item.Models, model)
		}
		sort.SliceStable(item.Models, func(a, b int) bool {
			return strings.ToLower(item.Models[a].Pattern) < strings.ToLower(item.Models[b].Pattern)
		})
		if len(item.Models) == 0 {
			continue
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if strings.EqualFold(out[a].Name, out[b].Name) {
			return strings.ToLower(out[a].Code) < strings.ToLower(out[b].Code)
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	if withPricing && s.pricing != nil {
		inputs := make([]PricingInput, 0)
		for i := range out {
			for j := range out[i].Models {
				model := &out[i].Models[j]
				pricingModel := model.UpstreamModel
				if pricingModel == "" {
					pricingModel = model.Pattern
				}
				inputs = append(inputs, PricingInput{
					Model:        pricingModel,
					Adapter:      out[i].AccountPlatform,
					PlatformCode: out[i].Code,
					PublicModel:  model.Pattern,
				})
			}
		}
		if len(inputs) == 0 {
			return out, nil
		}
		resolved, resolveErr := s.pricing.ResolveBatch(ctx, inputs)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve platform catalog pricing: %w", resolveErr)
		}
		if len(resolved) != len(inputs) {
			return nil, fmt.Errorf("resolve platform catalog pricing: got %d results for %d inputs", len(resolved), len(inputs))
		}
		index := 0
		for i := range out {
			for j := range out[i].Models {
				out[i].Models[j].Pricing = resolved[index]
				index++
			}
		}
	}
	return out, nil
}
