package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	SubscriptionID  int64
	Status          string
	ExpiresAt       time.Time
	DailyUsage      float64
	WeeklyUsage     float64
	MonthlyUsage    float64
	DailyLimitUSD   *float64
	WeeklyLimitUSD  *float64
	MonthlyLimitUSD *float64
	RateMultiplier  float64
	Version         int64
}
