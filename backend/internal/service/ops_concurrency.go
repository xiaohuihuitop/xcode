package service

import (
	"context"
	"log"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	opsAccountsPageSize          = 100
	opsConcurrencyBatchChunkSize = 200
)

type opsAccountStatsRepository interface {
	ListOpsAccountsForStats(ctx context.Context, platformFilter string, platformIDFilter *int64) ([]Account, error)
}

func (s *OpsService) listAllAccountsForOps(ctx context.Context, platformFilter string, platformIDFilter *int64) ([]Account, error) {
	if s == nil || s.accountRepo == nil {
		return []Account{}, nil
	}
	if repo, ok := s.accountRepo.(opsAccountStatsRepository); ok {
		return repo.ListOpsAccountsForStats(ctx, platformFilter, platformIDFilter)
	}

	out := make([]Account, 0, 128)
	page := 1
	platformID := int64(0)
	if platformIDFilter != nil {
		platformID = *platformIDFilter
	}
	for {
		accounts, pageInfo, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{
			Page:     page,
			PageSize: opsAccountsPageSize,
		}, platformFilter, "", "", "", platformID, "")
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			break
		}

		out = append(out, accounts...)
		if pageInfo != nil && int64(len(out)) >= pageInfo.Total {
			break
		}
		if len(accounts) < opsAccountsPageSize {
			break
		}

		page++
		if page > 10_000 {
			log.Printf("[Ops] listAllAccountsForOps: aborting after too many pages (platform=%q)", platformFilter)
			break
		}
	}

	return out, nil
}

func (s *OpsService) getAccountsLoadMapBestEffort(ctx context.Context, accounts []Account) map[int64]*AccountLoadInfo {
	if s == nil || s.concurrencyService == nil {
		return map[int64]*AccountLoadInfo{}
	}
	if len(accounts) == 0 {
		return map[int64]*AccountLoadInfo{}
	}

	// De-duplicate IDs (and keep the max concurrency to avoid under-reporting).
	unique := make(map[int64]int, len(accounts))
	for _, acc := range accounts {
		if acc.ID <= 0 {
			continue
		}
		lf := acc.EffectiveLoadFactor()
		if prev, ok := unique[acc.ID]; !ok || lf > prev {
			unique[acc.ID] = lf
		}
	}

	batch := make([]AccountWithConcurrency, 0, len(unique))
	for id, maxConc := range unique {
		batch = append(batch, AccountWithConcurrency{
			ID:             id,
			MaxConcurrency: maxConc,
		})
	}

	out := make(map[int64]*AccountLoadInfo, len(batch))
	for i := 0; i < len(batch); i += opsConcurrencyBatchChunkSize {
		end := i + opsConcurrencyBatchChunkSize
		if end > len(batch) {
			end = len(batch)
		}
		part, err := s.concurrencyService.GetAccountsLoadBatch(ctx, batch[i:end])
		if err != nil {
			// Best-effort: return zeros rather than failing the ops UI.
			log.Printf("[Ops] GetAccountsLoadBatch failed: %v", err)
			continue
		}
		for k, v := range part {
			out[k] = v
		}
	}

	return out
}

// GetConcurrencyStats returns real-time concurrency usage aggregated by protocol platform, platform pool, and account.
//
// Optional filters:
// - platformFilter: only include accounts in that platform (best-effort reduces DB load)
// - platformIDFilter: only include accounts that belong to that platform pool
func (s *OpsService) GetConcurrencyStats(
	ctx context.Context,
	platformFilter string,
	platformIDFilter *int64,
) (map[string]*PlatformConcurrencyInfo, map[int64]*PlatformIDConcurrencyInfo, map[int64]*AccountConcurrencyInfo, *time.Time, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, nil, nil, nil, err
	}

	accounts, err := s.listAllAccountsForOps(ctx, platformFilter, platformIDFilter)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	collectedAt := time.Now()
	loadMap := s.getAccountsLoadMapBestEffort(ctx, accounts)

	platform := make(map[string]*PlatformConcurrencyInfo)
	platformPools := make(map[int64]*PlatformIDConcurrencyInfo)
	account := make(map[int64]*AccountConcurrencyInfo)

	for _, acc := range accounts {
		if acc.ID <= 0 {
			continue
		}

		load := loadMap[acc.ID]
		currentInUse := int64(0)
		waiting := int64(0)
		if load != nil {
			currentInUse = int64(load.CurrentConcurrency)
			waiting = int64(load.WaitingCount)
		}

		if _, ok := account[acc.ID]; !ok {
			info := &AccountConcurrencyInfo{
				AccountID:      acc.ID,
				AccountName:    acc.Name,
				Platform:       acc.Platform,
				CurrentInUse:   currentInUse,
				MaxCapacity:    int64(acc.Concurrency),
				WaitingInQueue: waiting,
			}
			if acc.PlatformID != nil {
				info.PlatformID = *acc.PlatformID
				info.PlatformName = acc.PlatformName
			}
			if info.MaxCapacity > 0 {
				info.LoadPercentage = float64(info.CurrentInUse) / float64(info.MaxCapacity) * 100
			}
			account[acc.ID] = info
		}

		if acc.PlatformID != nil && *acc.PlatformID > 0 {
			poolID := *acc.PlatformID
			if _, ok := platformPools[poolID]; !ok {
				platformPools[poolID] = &PlatformIDConcurrencyInfo{
					PlatformID:   poolID,
					PlatformName: acc.PlatformName,
					Platform:     acc.Platform,
				}
			}
			pool := platformPools[poolID]
			pool.MaxCapacity += int64(acc.Concurrency)
			pool.CurrentInUse += currentInUse
			pool.WaitingInQueue += waiting
		}

		// Platform aggregation.
		if acc.Platform != "" {
			if _, ok := platform[acc.Platform]; !ok {
				platform[acc.Platform] = &PlatformConcurrencyInfo{
					Platform: acc.Platform,
				}
			}
			p := platform[acc.Platform]
			p.MaxCapacity += int64(acc.Concurrency)
			p.CurrentInUse += currentInUse
			p.WaitingInQueue += waiting
		}

	}

	for _, info := range platform {
		if info.MaxCapacity > 0 {
			info.LoadPercentage = float64(info.CurrentInUse) / float64(info.MaxCapacity) * 100
		}
	}
	for _, info := range platformPools {
		if info.MaxCapacity > 0 {
			info.LoadPercentage = float64(info.CurrentInUse) / float64(info.MaxCapacity) * 100
		}
	}

	return platform, platformPools, account, &collectedAt, nil
}

// listAllActiveUsersForOps returns all active users with their concurrency settings.
func (s *OpsService) listAllActiveUsersForOps(ctx context.Context) ([]User, error) {
	if s == nil || s.userRepo == nil {
		return []User{}, nil
	}

	out := make([]User, 0, 128)
	page := 1
	for {
		users, pageInfo, err := s.userRepo.ListWithFilters(ctx, pagination.PaginationParams{
			Page:     page,
			PageSize: opsAccountsPageSize,
		}, UserListFilters{
			Status: StatusActive,
		})
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			break
		}

		out = append(out, users...)
		if pageInfo != nil && int64(len(out)) >= pageInfo.Total {
			break
		}
		if len(users) < opsAccountsPageSize {
			break
		}

		page++
		if page > 10_000 {
			log.Printf("[Ops] listAllActiveUsersForOps: aborting after too many pages")
			break
		}
	}

	return out, nil
}

// getUsersLoadMapBestEffort returns user load info for the given users.
func (s *OpsService) getUsersLoadMapBestEffort(ctx context.Context, users []User) map[int64]*UserLoadInfo {
	if s == nil || s.concurrencyService == nil {
		return map[int64]*UserLoadInfo{}
	}
	if len(users) == 0 {
		return map[int64]*UserLoadInfo{}
	}

	// De-duplicate IDs (and keep the max concurrency to avoid under-reporting).
	unique := make(map[int64]int, len(users))
	for _, u := range users {
		if u.ID <= 0 {
			continue
		}
		if prev, ok := unique[u.ID]; !ok || u.Concurrency > prev {
			unique[u.ID] = u.Concurrency
		}
	}

	batch := make([]UserWithConcurrency, 0, len(unique))
	for id, maxConc := range unique {
		batch = append(batch, UserWithConcurrency{
			ID:             id,
			MaxConcurrency: maxConc,
		})
	}

	out := make(map[int64]*UserLoadInfo, len(batch))
	for i := 0; i < len(batch); i += opsConcurrencyBatchChunkSize {
		end := i + opsConcurrencyBatchChunkSize
		if end > len(batch) {
			end = len(batch)
		}
		part, err := s.concurrencyService.GetUsersLoadBatch(ctx, batch[i:end])
		if err != nil {
			// Best-effort: return zeros rather than failing the ops UI.
			log.Printf("[Ops] GetUsersLoadBatch failed: %v", err)
			continue
		}
		for k, v := range part {
			out[k] = v
		}
	}

	return out
}

// GetUserConcurrencyStats returns real-time concurrency usage for all active users.
func (s *OpsService) GetUserConcurrencyStats(ctx context.Context) (map[int64]*UserConcurrencyInfo, *time.Time, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, nil, err
	}

	users, err := s.listAllActiveUsersForOps(ctx)
	if err != nil {
		return nil, nil, err
	}

	collectedAt := time.Now()
	loadMap := s.getUsersLoadMapBestEffort(ctx, users)

	result := make(map[int64]*UserConcurrencyInfo)

	for _, u := range users {
		if u.ID <= 0 {
			continue
		}

		load := loadMap[u.ID]
		currentInUse := int64(0)
		waiting := int64(0)
		if load != nil {
			currentInUse = int64(load.CurrentConcurrency)
			waiting = int64(load.WaitingCount)
		}

		// Skip users with no concurrency activity
		if currentInUse == 0 && waiting == 0 {
			continue
		}

		info := &UserConcurrencyInfo{
			UserID:         u.ID,
			UserEmail:      u.Email,
			Username:       u.Username,
			CurrentInUse:   currentInUse,
			MaxCapacity:    int64(u.Concurrency),
			WaitingInQueue: waiting,
		}
		if info.MaxCapacity > 0 {
			info.LoadPercentage = float64(info.CurrentInUse) / float64(info.MaxCapacity) * 100
		}
		result[u.ID] = info
	}

	return result, &collectedAt, nil
}
