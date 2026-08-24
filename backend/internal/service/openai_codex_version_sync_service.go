package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	openAICodexVersionSyncInterval = 6 * time.Hour
	openAICodexVersionSyncTimeout  = 30 * time.Second
	openAICodexVersionSyncRepo     = "openai/codex"
	openAICodexVersionSyncPerPage  = 30
	openAICodexVersionTagPrefix    = "rust-v"
)

type OpenAICodexVersionSyncService struct {
	settingRepo    SettingRepository
	settingService *SettingService
	githubClient   GitHubReleaseClient
	interval       time.Duration
	stopCh         chan struct{}
	lifecycleMu    sync.Mutex
	started        bool
	stopped        bool
	wg             sync.WaitGroup
}

func NewOpenAICodexVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
	interval time.Duration,
) *OpenAICodexVersionSyncService {
	return &OpenAICodexVersionSyncService{
		settingRepo:    settingRepo,
		settingService: settingService,
		githubClient:   githubClient,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *OpenAICodexVersionSyncService) Start() {
	if s == nil || s.settingRepo == nil || s.githubClient == nil || s.interval <= 0 {
		return
	}
	s.lifecycleMu.Lock()
	if s.started || s.stopped {
		s.lifecycleMu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.lifecycleMu.Unlock()

	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runInitial()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *OpenAICodexVersionSyncService) runInitial() {
	if s.syncedWithinInterval() {
		return
	}
	s.runOnce()
}

func (s *OpenAICodexVersionSyncService) syncedWithinInterval() bool {
	if s == nil || s.settingRepo == nil || s.interval <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	setting, err := s.settingRepo.Get(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			log.Printf("[OpenAICodexVersionSync] Read sync timestamp failed: %v", err)
		}
		return false
	}
	if setting == nil || setting.UpdatedAt.IsZero() {
		return false
	}
	if NormalizeCodexClientVersion(setting.Value) == "" {
		return false
	}
	return time.Since(setting.UpdatedAt) < s.interval
}

func (s *OpenAICodexVersionSyncService) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	s.lifecycleMu.Unlock()
	s.wg.Wait()
}

func (s *OpenAICodexVersionSyncService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	enabled, err := s.autoSyncEnabled(ctx)
	if err != nil {
		log.Printf("[OpenAICodexVersionSync] Read auto-sync setting failed: %v", err)
		return
	}
	if !enabled {
		return
	}
	latest, err := s.fetchLatestStableVersion(ctx)
	if err != nil {
		log.Printf("[OpenAICodexVersionSync] Fetch stable Codex release failed: %v", err)
		return
	}
	if latest == "" {
		log.Printf("[OpenAICodexVersionSync] No stable %s release found", openAICodexVersionTagPrefix)
		return
	}
	currentValue, err := s.currentSyncedVersion(ctx)
	if err != nil {
		log.Printf("[OpenAICodexVersionSync] Read current synced version failed: %v", err)
		return
	}
	current := NormalizeCodexClientVersion(currentValue)
	if current != "" && CompareVersions(latest, current) <= 0 {
		return
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersionSynced, latest); err != nil {
		log.Printf("[OpenAICodexVersionSync] Persist synced version %s failed: %v", latest, err)
		return
	}
	if s.settingService != nil {
		s.settingService.InvalidateOpenAICodexClientVersionCache()
	}
}

func (s *OpenAICodexVersionSyncService) fetchLatestStableVersion(ctx context.Context) (string, error) {
	release, latestErr := s.githubClient.FetchLatestRelease(ctx, openAICodexVersionSyncRepo)
	if latestErr == nil {
		if version := latestCodexStableReleaseVersion([]*GitHubRelease{release}); version != "" {
			return version, nil
		}
	}
	releases, err := s.githubClient.FetchRecentReleases(ctx, openAICodexVersionSyncRepo, openAICodexVersionSyncPerPage)
	if err != nil {
		if latestErr != nil {
			return "", fmt.Errorf("latest release: %v; recent releases: %w", latestErr, err)
		}
		return "", err
	}
	return latestCodexStableReleaseVersion(releases), nil
}

func (s *OpenAICodexVersionSyncService) autoSyncEnabled(ctx context.Context) (bool, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexVersionAutoSyncEnabled)
	if errors.Is(err, ErrSettingNotFound) || strings.TrimSpace(value) == "" && err == nil {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(value), "true"), nil
}

func (s *OpenAICodexVersionSyncService) currentSyncedVersion(ctx context.Context) (string, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if errors.Is(err, ErrSettingNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func latestCodexStableReleaseVersion(releases []*GitHubRelease) string {
	best := ""
	for _, release := range releases {
		if release == nil || release.Draft || release.Prerelease {
			continue
		}
		tag := strings.TrimSpace(release.TagName)
		if !strings.HasPrefix(tag, openAICodexVersionTagPrefix) {
			continue
		}
		version := NormalizeCodexClientVersion(strings.TrimPrefix(tag, openAICodexVersionTagPrefix))
		if version == "" || strings.Contains(version, "-") {
			continue
		}
		if best == "" || CompareVersions(version, best) > 0 {
			best = version
		}
	}
	return best
}
