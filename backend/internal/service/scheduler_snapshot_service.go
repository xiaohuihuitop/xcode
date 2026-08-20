package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// SchedulerSnapshotService maintains scheduler snapshots for Platform-owned
// account pools. Billing assets never participate in account selection.
type SchedulerSnapshotService struct {
	cache        SchedulerCache
	outboxRepo   SchedulerOutboxRepository
	accountRepo  AccountRepository
	platformRepo PlatformRepository
	cfg          *config.Config

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	rebuild  sync.Mutex
}

func NewSchedulerSnapshotService(
	cache SchedulerCache,
	outboxRepo SchedulerOutboxRepository,
	accountRepo AccountRepository,
	platformRepo PlatformRepository,
	cfg *config.Config,
) *SchedulerSnapshotService {
	return &SchedulerSnapshotService{
		cache:        cache,
		outboxRepo:   outboxRepo,
		accountRepo:  accountRepo,
		platformRepo: platformRepo,
		cfg:          cfg,
		stopCh:       make(chan struct{}),
	}
}

func (s *SchedulerSnapshotService) Start() {
	if s == nil || s.cache == nil {
		return
	}
	s.startWorker(s.runInitialRebuild)
	if s.outboxRepo != nil {
		s.startWorker(s.runOutboxWorker)
	}
	if s.fullRebuildInterval() > 0 {
		s.startWorker(s.runFullRebuildWorker)
	}
}

func (s *SchedulerSnapshotService) startWorker(run func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		run()
	}()
}

func (s *SchedulerSnapshotService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *SchedulerSnapshotService) ListSchedulableAccounts(
	ctx context.Context,
	platformID *int64,
	accountPlatform string,
	_ bool,
) ([]Account, bool, error) {
	if platformID == nil || *platformID <= 0 {
		return nil, false, ErrPlatformInvalid
	}
	bucket := schedulerPlatformBucket(*platformID, accountPlatform)
	if s.cache != nil {
		cached, hit, err := s.cache.GetSnapshot(ctx, bucket)
		if err == nil && hit {
			return derefSchedulerAccounts(cached), false, nil
		}
		if err != nil {
			slog.Warn("scheduler snapshot read failed", "bucket", bucket.String(), "error", err)
		}
	}

	accounts, err := s.accountRepo.ListSchedulableByPlatformPool(ctx, *platformID, accountPlatform)
	if err != nil {
		return nil, false, err
	}
	s.publishSnapshot(ctx, bucket, accounts)
	return accounts, false, nil
}

func (s *SchedulerSnapshotService) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if accountID <= 0 {
		return nil, nil
	}
	if s.cache != nil {
		account, err := s.cache.GetAccount(ctx, accountID)
		if err == nil && account != nil {
			return account, nil
		}
	}
	return s.accountRepo.GetByID(ctx, accountID)
}

func (s *SchedulerSnapshotService) UpdateAccountInCache(ctx context.Context, account *Account) error {
	if s == nil || s.cache == nil || account == nil {
		return nil
	}
	return s.cache.SetAccount(ctx, account)
}

func (s *SchedulerSnapshotService) runInitialRebuild() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.rebuildAll(ctx); err != nil {
		slog.Error("initial scheduler snapshot rebuild failed", "error", err)
	}
}

func (s *SchedulerSnapshotService) runOutboxWorker() {
	ticker := time.NewTicker(s.outboxPollInterval())
	defer ticker.Stop()
	s.pollOutbox()
	for {
		select {
		case <-ticker.C:
			s.pollOutbox()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SchedulerSnapshotService) runFullRebuildWorker() {
	ticker := time.NewTicker(s.fullRebuildInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := s.rebuildAll(ctx); err != nil {
				slog.Error("periodic scheduler snapshot rebuild failed", "error", err)
			}
			cancel()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SchedulerSnapshotService) pollOutbox() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	watermark, err := s.cache.GetOutboxWatermark(ctx)
	if err != nil {
		slog.Warn("read scheduler outbox watermark failed", "error", err)
		return
	}
	events, err := s.outboxRepo.ListAfterAndReleaseDedup(ctx, watermark, 500)
	if err != nil {
		slog.Warn("read scheduler outbox failed", "error", err)
		return
	}
	if len(events) == 0 {
		return
	}

	rebuildAll := false
	lastUsed := make(map[int64]time.Time)
	for _, event := range events {
		watermark = event.ID
		switch event.EventType {
		case SchedulerOutboxEventAccountLastUsed:
			mergeSchedulerLastUsed(lastUsed, event.Payload)
		case SchedulerOutboxEventAccountChanged,
			SchedulerOutboxEventAccountPlatformChanged,
			SchedulerOutboxEventAccountBulkChanged,
			SchedulerOutboxEventPlatformChanged,
			SchedulerOutboxEventFullRebuild:
			rebuildAll = true
		}
	}
	if len(lastUsed) > 0 {
		if err := s.cache.UpdateLastUsed(ctx, lastUsed); err != nil {
			slog.Warn("update scheduler last-used cache failed", "error", err)
			return
		}
	}
	if rebuildAll {
		if err := s.rebuildAll(ctx); err != nil {
			slog.Warn("scheduler outbox rebuild failed", "error", err)
			return
		}
	}
	if err := s.cache.SetOutboxWatermark(ctx, watermark); err != nil {
		slog.Warn("save scheduler outbox watermark failed", "error", err)
	}
}

func (s *SchedulerSnapshotService) rebuildAll(ctx context.Context) error {
	if s.platformRepo == nil || s.accountRepo == nil || s.cache == nil {
		return nil
	}
	s.rebuild.Lock()
	defer s.rebuild.Unlock()

	platforms, err := s.platformRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, platform := range platforms {
		if platform.ID <= 0 || !platform.IsActive() {
			continue
		}
		accounts, err := s.accountRepo.ListSchedulableByPlatformPool(ctx, platform.ID, platform.AccountPlatform)
		if err != nil {
			return err
		}
		s.publishSnapshot(ctx, schedulerPlatformBucket(platform.ID, platform.AccountPlatform), accounts)
	}
	return nil
}

func (s *SchedulerSnapshotService) publishSnapshot(ctx context.Context, bucket SchedulerBucket, accounts []Account) {
	if s.cache == nil {
		return
	}
	token, err := s.cache.CaptureBucketWriteToken(ctx, bucket)
	if err != nil {
		if !errors.Is(err, ErrSchedulerBucketRetired) && !errors.Is(err, ErrSchedulerBucketWriteFenced) {
			slog.Warn("capture scheduler snapshot token failed", "bucket", bucket.String(), "error", err)
		}
		return
	}
	if err := s.cache.SetSnapshot(ctx, bucket, token, accounts); err != nil &&
		!errors.Is(err, ErrSchedulerBucketRetired) && !errors.Is(err, ErrSchedulerBucketWriteFenced) {
		slog.Warn("publish scheduler snapshot failed", "bucket", bucket.String(), "error", err)
	}
}

func schedulerPlatformBucket(platformID int64, accountPlatform string) SchedulerBucket {
	return SchedulerBucket{PlatformID: platformID, Platform: accountPlatform, Mode: SchedulerModeSingle}
}

func derefSchedulerAccounts(accounts []*Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil {
			out = append(out, *account)
		}
	}
	return out
}

func mergeSchedulerLastUsed(dst map[int64]time.Time, payload map[string]any) {
	raw, ok := payload["last_used"].(map[string]any)
	if !ok {
		return
	}
	for key, value := range raw {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		seconds, ok := schedulerInt64(value)
		if ok && seconds > 0 {
			dst[id] = time.Unix(seconds, 0)
		}
	}
}

func schedulerInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		parsed, err := strconv.ParseInt(typed.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (s *SchedulerSnapshotService) outboxPollInterval() time.Duration {
	if s.cfg == nil || s.cfg.Gateway.Scheduling.OutboxPollIntervalSeconds <= 0 {
		return time.Second
	}
	return time.Duration(s.cfg.Gateway.Scheduling.OutboxPollIntervalSeconds) * time.Second
}

func (s *SchedulerSnapshotService) fullRebuildInterval() time.Duration {
	if s.cfg == nil || s.cfg.Gateway.Scheduling.FullRebuildIntervalSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.Scheduling.FullRebuildIntervalSeconds) * time.Second
}
