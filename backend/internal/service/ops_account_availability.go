package service

import (
	"context"
	"errors"
	"time"
)

// GetAccountAvailabilityStats returns current account availability stats.
//
// Query-level filtering is intentionally limited to protocol platform and platform pool.
func (s *OpsService) GetAccountAvailabilityStats(ctx context.Context, platformFilter string, platformIDFilter *int64) (
	map[string]*PlatformAvailability,
	map[int64]*PlatformIDAvailability,
	map[int64]*AccountAvailability,
	*time.Time,
	error,
) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, nil, nil, nil, err
	}

	accounts, err := s.listAllAccountsForOps(ctx, platformFilter, platformIDFilter)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	now := time.Now()
	collectedAt := now

	platform := make(map[string]*PlatformAvailability)
	platformPools := make(map[int64]*PlatformIDAvailability)
	account := make(map[int64]*AccountAvailability)

	for _, acc := range accounts {
		if acc.ID <= 0 {
			continue
		}

		isTempUnsched := false
		if acc.TempUnschedulableUntil != nil && now.Before(*acc.TempUnschedulableUntil) {
			isTempUnsched = true
		}

		isRateLimited := acc.RateLimitResetAt != nil && now.Before(*acc.RateLimitResetAt)
		isOverloaded := acc.OverloadUntil != nil && now.Before(*acc.OverloadUntil)
		hasError := acc.Status == StatusError

		// Normalize exclusive status flags so the UI doesn't show conflicting badges.
		if hasError {
			isRateLimited = false
			isOverloaded = false
		}

		isAvailable := acc.Status == StatusActive && acc.Schedulable && !isRateLimited && !isOverloaded && !isTempUnsched

		if acc.Platform != "" {
			if _, ok := platform[acc.Platform]; !ok {
				platform[acc.Platform] = &PlatformAvailability{
					Platform: acc.Platform,
				}
			}
			p := platform[acc.Platform]
			p.TotalAccounts++
			if isAvailable {
				p.AvailableCount++
			}
			if isRateLimited {
				p.RateLimitCount++
			}
			if hasError {
				p.ErrorCount++
			}
		}

		item := &AccountAvailability{
			AccountID:   acc.ID,
			AccountName: acc.Name,
			Platform:    acc.Platform,
			Status:      acc.Status,

			IsAvailable:   isAvailable,
			IsRateLimited: isRateLimited,
			IsOverloaded:  isOverloaded,
			HasError:      hasError,

			ErrorMessage: acc.ErrorMessage,
		}
		if acc.PlatformID != nil {
			item.PlatformID = *acc.PlatformID
			item.PlatformName = acc.PlatformName
		}

		if acc.PlatformID != nil && *acc.PlatformID > 0 {
			poolID := *acc.PlatformID
			if _, ok := platformPools[poolID]; !ok {
				platformPools[poolID] = &PlatformIDAvailability{
					PlatformID:   poolID,
					PlatformName: acc.PlatformName,
					Platform:     acc.Platform,
				}
			}
			pool := platformPools[poolID]
			pool.TotalAccounts++
			if isAvailable {
				pool.AvailableCount++
			}
			if isRateLimited {
				pool.RateLimitCount++
			}
			if hasError {
				pool.ErrorCount++
			}
		}

		if isRateLimited && acc.RateLimitResetAt != nil {
			item.RateLimitResetAt = acc.RateLimitResetAt
			remainingSec := int64(time.Until(*acc.RateLimitResetAt).Seconds())
			if remainingSec > 0 {
				item.RateLimitRemainingSec = &remainingSec
			}
		}
		if isOverloaded && acc.OverloadUntil != nil {
			item.OverloadUntil = acc.OverloadUntil
			remainingSec := int64(time.Until(*acc.OverloadUntil).Seconds())
			if remainingSec > 0 {
				item.OverloadRemainingSec = &remainingSec
			}
		}
		if isTempUnsched && acc.TempUnschedulableUntil != nil {
			item.TempUnschedulableUntil = acc.TempUnschedulableUntil
		}

		account[acc.ID] = item
	}

	return platform, platformPools, account, &collectedAt, nil
}

type OpsAccountAvailability struct {
	PlatformPool *PlatformIDAvailability
	Accounts     map[int64]*AccountAvailability
	CollectedAt  *time.Time
}

func (s *OpsService) GetAccountAvailability(ctx context.Context, platformFilter string, platformIDFilter *int64) (*OpsAccountAvailability, error) {
	if s == nil {
		return nil, errors.New("ops service is nil")
	}

	if s.getAccountAvailability != nil {
		return s.getAccountAvailability(ctx, platformFilter, platformIDFilter)
	}

	_, platformIDStats, accountStats, collectedAt, err := s.GetAccountAvailabilityStats(ctx, platformFilter, platformIDFilter)
	if err != nil {
		return nil, err
	}

	var platformPool *PlatformIDAvailability
	if platformIDFilter != nil && *platformIDFilter > 0 {
		platformPool = platformIDStats[*platformIDFilter]
	}

	if accountStats == nil {
		accountStats = map[int64]*AccountAvailability{}
	}

	return &OpsAccountAvailability{
		PlatformPool: platformPool,
		Accounts:     accountStats,
		CollectedAt:  collectedAt,
	}, nil
}
