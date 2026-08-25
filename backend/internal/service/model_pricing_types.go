package service

import (
	"fmt"
	"sort"
	"time"
)

type BillingMode string

const (
	BillingModeToken      BillingMode = "token"
	BillingModePerRequest BillingMode = "per_request"
	BillingModeImage      BillingMode = "image"
	BillingModeVideo      BillingMode = "video"
)

func (m BillingMode) IsValid() bool {
	switch m {
	case BillingModeToken, BillingModePerRequest, BillingModeImage, "":
		return true
	default:
		return false
	}
}

func (m BillingMode) IsValidUsageFilter() bool {
	switch m {
	case BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo, "":
		return true
	default:
		return false
	}
}

const (
	BillingModelSourceRequested = "requested"
	BillingModelSourceUpstream  = "upstream"
	BillingModelSourceMapped    = "mapped"
)

type ModelPricingOverrideInput struct {
	InputPrice       *float64
	OutputPrice      *float64
	CacheWritePrice  *float64
	CacheReadPrice   *float64
	ImageInputPrice  *float64
	ImageOutputPrice *float64
	PerRequestPrice  *float64
	Intervals        []PricingInterval
}

type PricingInterval struct {
	MinTokens       int
	MaxTokens       *int
	TierLabel       string
	InputPrice      *float64
	OutputPrice     *float64
	CacheWritePrice *float64
	CacheReadPrice  *float64
	PerRequestPrice *float64
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func FindMatchingInterval(intervals []PricingInterval, totalTokens int) *PricingInterval {
	for i := range intervals {
		interval := &intervals[i]
		if totalTokens > interval.MinTokens && (interval.MaxTokens == nil || totalTokens <= *interval.MaxTokens) {
			return interval
		}
	}
	return nil
}

func ValidateIntervals(intervals []PricingInterval, _ BillingMode) error {
	if len(intervals) == 0 {
		return nil
	}
	sorted := append([]PricingInterval(nil), intervals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MinTokens < sorted[j].MinTokens })
	for i := range sorted {
		if err := validateSingleInterval(&sorted[i], i); err != nil {
			return err
		}
	}
	return validateIntervalOverlap(sorted)
}

func validateSingleInterval(interval *PricingInterval, index int) error {
	if interval.MinTokens < 0 {
		return fmt.Errorf("interval #%d: min_tokens (%d) must be >= 0", index+1, interval.MinTokens)
	}
	if interval.MaxTokens != nil {
		if *interval.MaxTokens <= 0 {
			return fmt.Errorf("interval #%d: max_tokens (%d) must be > 0", index+1, *interval.MaxTokens)
		}
		if *interval.MaxTokens <= interval.MinTokens {
			return fmt.Errorf("interval #%d: max_tokens (%d) must be > min_tokens (%d)", index+1, *interval.MaxTokens, interval.MinTokens)
		}
	}
	return validateIntervalPrices(interval, index)
}

func validateIntervalPrices(interval *PricingInterval, index int) error {
	prices := []struct {
		name  string
		value *float64
	}{
		{"input_price", interval.InputPrice},
		{"output_price", interval.OutputPrice},
		{"cache_write_price", interval.CacheWritePrice},
		{"cache_read_price", interval.CacheReadPrice},
		{"per_request_price", interval.PerRequestPrice},
	}
	for _, price := range prices {
		if price.value != nil && *price.value < 0 {
			return fmt.Errorf("interval #%d: %s must be >= 0", index+1, price.name)
		}
	}
	return nil
}

func validateIntervalOverlap(intervals []PricingInterval) error {
	for i, interval := range intervals {
		if interval.MaxTokens == nil && i < len(intervals)-1 {
			return fmt.Errorf("interval #%d: unbounded interval (max_tokens=null) must be the last one", i+1)
		}
		if i == 0 {
			continue
		}
		previous := intervals[i-1]
		if previous.MaxTokens == nil || *previous.MaxTokens > interval.MinTokens {
			return fmt.Errorf("interval #%d and #%d overlap: prev max=%s > cur min=%d", i, i+1, formatMaxTokensLabel(previous.MaxTokens), interval.MinTokens)
		}
	}
	return nil
}

func formatMaxTokensLabel(max *int) string {
	if max == nil {
		return "unbounded"
	}
	return fmt.Sprintf("%d", *max)
}

type ModelRoutingUsageFields struct {
	OriginalModel      string
	MappedModel        string
	BillingModelSource string
	ModelMappingChain  string
}
