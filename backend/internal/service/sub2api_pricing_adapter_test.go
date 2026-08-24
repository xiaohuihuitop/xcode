//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
)

func TestSub2APIPricingAdapterQuotesBasePriceWithoutProductMultiplier(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	adapter := NewSub2APIPricingAdapter(billing, nil)

	quote, err := adapter.Quote(context.Background(), gatewayruntime.PricingRequest{
		Model: "claude-sonnet-4",
		Tokens: gatewayruntime.UsageFacts{
			InputTokens:  1000,
			OutputTokens: 200,
		},
	})

	require.NoError(t, err)
	require.Greater(t, quote.TotalCost, 0.0)
	require.Equal(t, quote.TotalCost, quote.InputCost+quote.OutputCost)
}

func TestSub2APIPricingAdapterPropagatesPricingRepositoryError(t *testing.T) {
	repoErr := errors.New("database unavailable")
	billing := NewBillingService(&config.Config{}, nil)
	resolver := NewModelPricingResolverWithCatalog(
		billing,
		NewModelPricingCatalog(&modelPricingOverrideRepoStub{err: repoErr}),
	)
	adapter := NewSub2APIPricingAdapter(billing, resolver)

	quote, err := adapter.Quote(context.Background(), gatewayruntime.PricingRequest{
		Model:   "gpt-5.6-sol",
		Adapter: "openai",
		Tokens:  gatewayruntime.UsageFacts{InputTokens: 100},
	})

	require.Empty(t, quote)
	require.ErrorContains(t, err, "database unavailable")
}
