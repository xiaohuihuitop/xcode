//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCalculateSubscriptionRemainingUsesInstanceSnapshotLimit(t *testing.T) {
	planLimit := 10.0
	subscription := &service.UserSubscription{
		DailyUsageUSD:         9,
		DailyLimitUSDSnapshot: &planLimit,
	}

	remaining := (&GatewayHandler{}).calculateSubscriptionRemaining(subscription)

	require.Equal(t, 1.0, remaining)
}
