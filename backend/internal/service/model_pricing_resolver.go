package service

import (
	"context"
	"errors"
	"strings"
)

// PricingSource 定价来源标识
const (
	PricingSourceLiteLLM  = "litellm"
	PricingSourceFallback = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自 LiteLLM 或 fallback）
	BasePricing     *ModelPricing
	OfficialPricing *ModelPricing
	OfficialMode    BillingMode
	OfficialSource  PricingSourceInfo
	MatchedOverride *ModelPricingOverride

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 官方目录的独立分层；不得复用管理员售价规则。
	OfficialIntervals    []PricingInterval
	OfficialRequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice                 float64
	DefaultPerRequestPriceExplicit         bool
	OfficialDefaultPerRequestPrice         float64
	OfficialDefaultPerRequestPriceExplicit bool

	// 来源标识
	Source string // "channel", "litellm", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Channel → LiteLLM → Fallback。
type ModelPricingResolver struct {
	billingService *BillingService
	pricingCatalog *ModelPricingCatalog
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(billingService *BillingService) *ModelPricingResolver {
	return NewModelPricingResolverWithCatalog(billingService, nil)
}

// NewModelPricingResolverWithCatalog wires the independent adapter/model
// override catalog ahead of LiteLLM and the temporary legacy channel path.
func NewModelPricingResolverWithCatalog(billingService *BillingService, catalog *ModelPricingCatalog) *ModelPricingResolver {
	return &ModelPricingResolver{
		billingService: billingService,
		pricingCatalog: catalog,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model        string
	Adapter      string // resolved account adapter; independent pricing path
	PlatformCode string // administrator-owned platform code, when a platform route exists
	PublicModel  string // model name requested by the client before upstream mapping
}

// Resolve 解析模型定价。
// 1. 获取基础定价（LiteLLM → Fallback）
// 2. 未命中覆盖时使用 LiteLLM 或静态 fallback
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) (*ResolvedPricing, error) {
	var override *ModelPricingOverride
	if r.pricingCatalog != nil {
		var err error
		override, err = r.pricingCatalog.ResolveForPricingInput(ctx, input)
		if err != nil {
			return nil, err
		}
	}
	return r.resolveWithOverride(input, override)
}

// ResolveBatch resolves a request-scoped set of pricing inputs against one
// rule snapshot. Repository or base-pricing errors fail the entire batch.
func (r *ModelPricingResolver) ResolveBatch(ctx context.Context, inputs []PricingInput) ([]*ResolvedPricing, error) {
	var snapshot *ModelPricingRuleSnapshot
	if r.pricingCatalog != nil {
		var err error
		snapshot, err = r.pricingCatalog.LoadSnapshot(ctx)
		if err != nil {
			return nil, err
		}
	}

	resolved := make([]*ResolvedPricing, len(inputs))
	for i := range inputs {
		var override *ModelPricingOverride
		if snapshot != nil {
			override = snapshot.ResolveForPricingInput(inputs[i])
		}
		item, err := r.resolveWithOverride(inputs[i], override)
		if errors.Is(err, ErrModelPricingUnavailable) {
			pricingModel := pricingModelForInput(inputs[i])
			resolved[i] = &ResolvedPricing{
				OfficialSource: PricingSourceInfo{
					Type: PricingSourceUnavailable, Name: "Unavailable", MatchedModel: pricingModel,
				},
				Source: string(PricingSourceUnavailable),
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		resolved[i] = item
	}
	return resolved, nil
}

func (r *ModelPricingResolver) resolveWithOverride(input PricingInput, override *ModelPricingOverride) (*ResolvedPricing, error) {

	pricingModel := pricingModelForInput(input)
	lookup, source, lookupErr := r.resolveBasePricing(pricingModel)
	var basePricing *ModelPricing
	var officialSource PricingSourceInfo
	if lookup != nil {
		basePricing = lookup.Pricing
		officialSource = lookup.Source
	} else if errors.Is(lookupErr, ErrModelPricingUnavailable) {
		officialSource = PricingSourceInfo{
			Type: PricingSourceUnavailable, Name: "Unavailable", MatchedModel: pricingModel,
		}
	}
	if override != nil {
		if lookupErr != nil && !errors.Is(lookupErr, ErrModelPricingUnavailable) {
			return nil, lookupErr
		}
		resolved := pricingOverrideToResolved(override, basePricing, officialSource)
		if lookup != nil {
			resolved.OfficialMode = lookup.Mode
			resolved.OfficialDefaultPerRequestPrice = lookup.DefaultPerRequestPrice
			resolved.OfficialDefaultPerRequestPriceExplicit = lookup.DefaultPerRequestPriceExplicit
			if lookup.Mode != resolved.Mode {
				resolved.BasePricing = &ModelPricing{}
				resolved.SupportsCacheBreakdown = false
				applyOverridePrices(override, resolved.BasePricing)
			}
			if lookup.Mode == resolved.Mode && override.PerRequestPrice == nil {
				resolved.DefaultPerRequestPrice = lookup.DefaultPerRequestPrice
				resolved.DefaultPerRequestPriceExplicit = lookup.DefaultPerRequestPriceExplicit
			}
		}
		return resolved, nil
	}
	if lookupErr != nil {
		return nil, lookupErr
	}
	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		OfficialMode:           BillingModeToken,
		BasePricing:            cloneModelPricing(basePricing),
		OfficialPricing:        cloneModelPricing(basePricing),
		OfficialSource:         officialSource,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}
	if lookup != nil {
		resolved.Mode = lookup.Mode
		resolved.OfficialMode = lookup.Mode
		resolved.DefaultPerRequestPrice = lookup.DefaultPerRequestPrice
		resolved.DefaultPerRequestPriceExplicit = lookup.DefaultPerRequestPriceExplicit
		resolved.OfficialDefaultPerRequestPrice = lookup.DefaultPerRequestPrice
		resolved.OfficialDefaultPerRequestPriceExplicit = lookup.DefaultPerRequestPriceExplicit
	}

	return resolved, nil
}

func pricingModelForInput(input PricingInput) string {
	if model := strings.TrimSpace(input.Model); model != "" {
		return model
	}
	return strings.TrimSpace(input.PublicModel)
}

// resolveBasePricing 从 LiteLLM 或 Fallback 获取基础定价
func (r *ModelPricingResolver) resolveBasePricing(model string) (*ModelPricingLookup, string, error) {
	pricing, err := r.billingService.LookupModelPricing(model)
	if err != nil {
		return nil, PricingSourceFallback, err
	}
	if pricing.Source.Type == PricingSourceCodeFallback {
		return pricing, PricingSourceFallback, nil
	}
	return pricing, PricingSourceLiteLLM, nil
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(pricingOverride *ModelPricingOverrideInput, resolved *ResolvedPricing) {
	// 过滤掉所有价格字段都为空的无效 interval
	validIntervals := filterValidIntervals(pricingOverride.Intervals)

	// 如果有有效的区间定价，使用区间
	if len(validIntervals) > 0 {
		resolved.Intervals = validIntervals
		// 区间不匹配时回退到 BasePricing，也需要覆盖图片价格
		if resolved.BasePricing == nil {
			resolved.BasePricing = &ModelPricing{}
		} else {
			// 防止修改 fallbackPrices 中的共享指针
			cloned := *resolved.BasePricing
			resolved.BasePricing = &cloned
		}
		if pricingOverride.ImageOutputPrice != nil {
			resolved.BasePricing.ImageOutputPricePerToken = *pricingOverride.ImageOutputPrice
		} else {
			resolved.BasePricing.ImageOutputPricePerToken = 0
		}
		resolved.BasePricing.ImageOutputPriceExplicit = true
		applyOverrideImageInputPrice(pricingOverride, resolved.BasePricing)
		return
	}

	// 否则用 flat 字段覆盖 BasePricing
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	if pricingOverride.InputPrice != nil {
		resolved.BasePricing.InputPricePerToken = *pricingOverride.InputPrice
		resolved.BasePricing.InputPricePerTokenPriority = *pricingOverride.InputPrice
	}
	if pricingOverride.OutputPrice != nil {
		resolved.BasePricing.OutputPricePerToken = *pricingOverride.OutputPrice
		resolved.BasePricing.OutputPricePerTokenPriority = *pricingOverride.OutputPrice
	}
	if pricingOverride.CacheWritePrice != nil {
		resolved.BasePricing.CacheCreationPricePerToken = *pricingOverride.CacheWritePrice
		resolved.BasePricing.CacheCreationPricePerTokenPriority = *pricingOverride.CacheWritePrice
		resolved.BasePricing.CacheCreationPriceExplicit = true
		resolved.BasePricing.CacheCreation5mPrice = *pricingOverride.CacheWritePrice
		resolved.BasePricing.CacheCreation1hPrice = *pricingOverride.CacheWritePrice
	}
	if pricingOverride.CacheReadPrice != nil {
		resolved.BasePricing.CacheReadPricePerToken = *pricingOverride.CacheReadPrice
		resolved.BasePricing.CacheReadPricePerTokenPriority = *pricingOverride.CacheReadPrice
	}
	// 渠道定价覆盖一切：显式配置则用配置值，未配置则归零（不回退到 LiteLLM）
	if pricingOverride.ImageOutputPrice != nil {
		resolved.BasePricing.ImageOutputPricePerToken = *pricingOverride.ImageOutputPrice
	} else {
		resolved.BasePricing.ImageOutputPricePerToken = 0
	}
	resolved.BasePricing.ImageOutputPriceExplicit = true
	applyOverrideImageInputPrice(pricingOverride, resolved.BasePricing)
}

// applyOverrideImageInputPrice 应用渠道图片输入价：显式配置则用配置值；
// 未配置时归零，使 computeTokenBreakdown 回退到文本输入价（向后兼容，
// 避免 commit 引入的 LiteLLM 图片输入价泄漏进渠道自定义定价）。
// 与 image_output 不同，此处不设 Explicit 标志——图片输入未配置应回退文本价，
// 而非硬置 0。
func applyOverrideImageInputPrice(pricingOverride *ModelPricingOverrideInput, pricing *ModelPricing) {
	if pricingOverride != nil && pricingOverride.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *pricingOverride.ImageInputPrice
	} else {
		pricing.ImageInputPricePerToken = 0
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(pricingOverride *ModelPricingOverrideInput, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidIntervals(pricingOverride.Intervals)
	if pricingOverride.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *pricingOverride.PerRequestPrice
	}
}

// filterValidIntervals 过滤掉所有价格字段都为空的无效 interval。
// 前端可能创建了只有 min/max 但无价格的空 interval。
func filterValidIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil ||
			iv.PerRequestPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 如果有区间列表，找到匹配区间并构造 ModelPricing；否则直接返回 BasePricing。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	iv := FindMatchingInterval(resolved.Intervals, totalContextTokens)
	if iv == nil {
		return resolved.BasePricing
	}

	return intervalToModelPricing(iv, resolved.BasePricing, resolved.SupportsCacheBreakdown, nil)
}

// intervalToModelPricing 将区间定价转换为 ModelPricing
func intervalToModelPricing(
	iv *PricingInterval,
	basePricing *ModelPricing,
	supportsCacheBreakdown bool,
	pricingOverride *ModelPricingOverrideInput,
) *ModelPricing {
	pricing := cloneModelPricing(basePricing)
	if pricing == nil {
		pricing = &ModelPricing{SupportsCacheBreakdown: supportsCacheBreakdown}
	}
	if iv.InputPrice != nil {
		pricing.InputPricePerToken = *iv.InputPrice
		pricing.InputPricePerTokenPriority = *iv.InputPrice
		pricing.InputPriceExplicit = true
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerToken = *iv.OutputPrice
		pricing.OutputPricePerTokenPriority = *iv.OutputPrice
		pricing.OutputPriceExplicit = true
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPrice = *iv.CacheWritePrice
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *iv.CacheReadPrice
		pricing.CacheReadPriceExplicit = true
	}
	// 渠道定价存在时，ImageOutputPrice 显式覆盖；图片输入价用渠道级配置
	// （区间不携带图片输入价，与 image_output 一致）。
	if pricingOverride != nil {
		pricing.ImageOutputPriceExplicit = true
		if pricingOverride.ImageOutputPrice != nil {
			pricing.ImageOutputPricePerToken = *pricingOverride.ImageOutputPrice
		}
		applyOverrideImageInputPrice(pricingOverride, pricing)
	}
	return pricing
}

// GetRequestTierPrice 根据层级标签获取按次价格，并返回是否命中。
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) (float64, bool) {
	for _, tier := range resolved.RequestTiers {
		if tier.TierLabel == tierLabel && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPriceByContext 根据 context token 数获取按次价格，并返回是否命中。
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) (float64, bool) {
	iv := FindMatchingInterval(resolved.RequestTiers, totalContextTokens)
	if iv != nil && iv.PerRequestPrice != nil {
		return *iv.PerRequestPrice, true
	}
	return 0, false
}
