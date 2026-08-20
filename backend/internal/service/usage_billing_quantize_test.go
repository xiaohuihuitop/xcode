package service

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingCommandQuantizesAllAmounts(t *testing.T) {
	const raw = 0.000078125
	cmd := &UsageBillingCommand{
		RequestID:           "req-quantize",
		BalanceCost:         raw,
		SubscriptionCost:    raw,
		APIKeyQuotaCost:     raw,
		APIKeyRateLimitCost: raw,
		AccountQuotaCost:    raw,
	}
	expectedFingerprint := buildUsageBillingFingerprint(cmd)

	cmd.Normalize()

	require.Equal(t, expectedFingerprint, cmd.RequestFingerprint)
	for name, value := range map[string]float64{
		"balance":       cmd.BalanceCost,
		"subscription":  cmd.SubscriptionCost,
		"key quota":     cmd.APIKeyQuotaCost,
		"key rate":      cmd.APIKeyRateLimitCost,
		"account quota": cmd.AccountQuotaCost,
	} {
		require.Equal(t, 0.00007813, value, name)
	}
}

func TestQuantizeUsageBillingAmountUsesDecimalHalfAwayFromZero(t *testing.T) {
	for _, value := range []float64{0.000078124, 0.000078125, 0.000078126, -0.000078125} {
		want, _ := decimal.NewFromFloat(value).Round(UsageBillingMonetaryScale).Float64()
		require.Equal(t, want, QuantizeUsageBillingAmount(value), "value=%v", value)
	}
	require.True(t, math.IsNaN(QuantizeUsageBillingAmount(math.NaN())))
	require.True(t, math.IsInf(QuantizeUsageBillingAmount(math.Inf(1)), 1))
}
