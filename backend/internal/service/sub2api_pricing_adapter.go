package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

var ErrSub2APIPricingAdapterUnavailable = errors.New("sub2api pricing adapter is unavailable")

type Sub2APIPricingAdapter struct {
	billing  *BillingService
	resolver *ModelPricingResolver
}

func NewSub2APIPricingAdapter(billing *BillingService, resolver *ModelPricingResolver) *Sub2APIPricingAdapter {
	return &Sub2APIPricingAdapter{billing: billing, resolver: resolver}
}

func (a *Sub2APIPricingAdapter) Quote(ctx context.Context, request gatewayruntime.PricingRequest) (gatewayruntime.PricingQuote, error) {
	if a == nil || a.billing == nil {
		return gatewayruntime.PricingQuote{}, ErrSub2APIPricingAdapterUnavailable
	}
	tokens := UsageTokens{
		InputTokens:         request.Tokens.InputTokens,
		OutputTokens:        request.Tokens.OutputTokens,
		CacheCreationTokens: request.Tokens.CacheCreationTokens,
		CacheReadTokens:     request.Tokens.CacheReadTokens,
		ImageInputTokens:    request.Tokens.ImageInputTokens,
		ImageOutputTokens:   request.Tokens.ImageOutputTokens,
	}
	var (
		cost *CostBreakdown
		err  error
	)
	if a.resolver != nil {
		cost, err = a.billing.CalculateCostUnified(CostInput{
			Ctx:                       ctx,
			Model:                     request.Model,
			Adapter:                   request.Adapter,
			Tokens:                    tokens,
			RequestCount:              request.RequestCount,
			SizeTier:                  request.SizeTier,
			ServiceTier:               request.ServiceTier,
			Resolver:                  a.resolver,
			RateMultiplier:            1,
			LongContextBillingEnabled: request.LongContextBillingEnabled,
		})
	} else {
		cost, err = a.billing.CalculateCostWithServiceTier(request.Model, tokens, 1, request.ServiceTier)
	}
	if err != nil {
		return gatewayruntime.PricingQuote{}, err
	}
	if cost == nil {
		return gatewayruntime.PricingQuote{}, ErrModelPricingUnavailable
	}
	return gatewayruntime.PricingQuote{
		InputCost:   cost.InputCost + cost.ImageInputCost,
		OutputCost:  cost.OutputCost + cost.ImageOutputCost,
		TotalCost:   cost.TotalCost,
		BillingMode: cost.BillingMode,
	}, nil
}
