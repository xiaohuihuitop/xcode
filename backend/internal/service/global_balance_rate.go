package service

import (
	"context"
	"math"
)

const defaultGlobalBalanceRateMultiplier = 1.0

// GlobalBalanceRateProvider reads the single multiplier used only when a
// request consumes user balance rather than a subscription instance.
type GlobalBalanceRateProvider interface {
	GetGlobalBalanceRateMultiplier(ctx context.Context) float64
}

func globalBalanceRateMultiplier(ctx context.Context, provider GlobalBalanceRateProvider) float64 {
	if provider == nil {
		return defaultGlobalBalanceRateMultiplier
	}
	rate := provider.GetGlobalBalanceRateMultiplier(ctx)
	if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return defaultGlobalBalanceRateMultiplier
	}
	return rate
}
