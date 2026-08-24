//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexVersionSettingRepoStub struct {
	SettingRepository
	values    map[string]string
	writes    []string
	updatedAt time.Time
	valueErr  error
}

func (r *codexVersionSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, nil
}

func (r *codexVersionSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.valueErr != nil {
		return "", r.valueErr
	}
	return r.values[key], nil
}

func (r *codexVersionSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	r.writes = append(r.writes, key+"="+value)
	return nil
}

func (r *codexVersionSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value, UpdatedAt: r.updatedAt}, nil
}

type codexVersionGitHubStub struct {
	GitHubReleaseClient
	latest      *GitHubRelease
	latestErr   error
	releases    []*GitHubRelease
	releasesErr error
	latestCalls int
	recentCalls int
}

type codexVersionLifecycleRepoStub struct {
	SettingRepository
	getCalls atomic.Int32
	started  chan struct{}
	release  chan struct{}
}

func (r *codexVersionLifecycleRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	if key != SettingKeyOpenAICodexClientVersionSynced {
		return nil, ErrSettingNotFound
	}
	r.getCalls.Add(1)
	if r.started != nil {
		r.started <- struct{}{}
	}
	if r.release != nil {
		<-r.release
	}
	return &Setting{Key: key, Value: codexCLIVersion, UpdatedAt: time.Now()}, nil
}

func (s *codexVersionGitHubStub) FetchLatestRelease(_ context.Context, _ string) (*GitHubRelease, error) {
	s.latestCalls++
	return s.latest, s.latestErr
}

func (s *codexVersionGitHubStub) FetchRecentReleases(_ context.Context, _ string, _ int) ([]*GitHubRelease, error) {
	s.recentCalls++
	return s.releases, s.releasesErr
}

func TestGetOpenAICodexClientVersionPriority(t *testing.T) {
	tests := []struct {
		name     string
		override string
		synced   string
		want     string
	}{
		{name: "override", override: "0.150.0", synced: "0.146.0", want: "0.150.0"},
		{name: "synced", synced: "0.146.0", want: "0.146.0"},
		{name: "fallback", want: codexCLIVersion},
		{name: "invalid override", override: "latest", synced: "0.146.0", want: "0.146.0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion:       test.override,
				SettingKeyOpenAICodexClientVersionSynced: test.synced,
			}}, nil)
			require.Equal(t, test.want, svc.GetOpenAICodexClientVersion(context.Background()))
		})
	}
}

func TestOpenAICodexVersionSyncWritesLatestStableVersion(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{}}
	github := &codexVersionGitHubStub{releases: []*GitHubRelease{
		{TagName: "rust-v0.147.0-alpha.4", Prerelease: true},
		{TagName: "rust-v0.146.0"},
		{TagName: "rusty-v8-v150.4.0"},
	}}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Equal(t, []string{SettingKeyOpenAICodexClientVersionSynced + "=0.146.0"}, repo.writes)
}

func TestOpenAICodexVersionSyncDisabledDoesNotFetchGitHub(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexVersionAutoSyncEnabled: "false",
	}}
	github := &codexVersionGitHubStub{}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Zero(t, github.latestCalls)
	require.Zero(t, github.recentCalls)
	require.Empty(t, repo.writes)
}

func TestOpenAICodexVersionSyncSettingReadErrorDoesNotFetchGitHub(t *testing.T) {
	repo := &codexVersionSettingRepoStub{
		values:   map[string]string{},
		valueErr: errors.New("settings unavailable"),
	}
	github := &codexVersionGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.146.0"}}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Zero(t, github.latestCalls)
	require.Zero(t, github.recentCalls)
	require.Empty(t, repo.writes)
}

func TestOpenAICodexVersionSyncDoesNotDowngrade(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.147.0",
	}}
	github := &codexVersionGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.146.0"}}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Empty(t, repo.writes)
}

func TestOpenAICodexVersionSyncFallsBackWhenLatestIsNotCodex(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{}}
	github := &codexVersionGitHubStub{
		latest:   &GitHubRelease{TagName: "rusty-v8-v150.4.0"},
		releases: []*GitHubRelease{{TagName: "rust-v0.146.0"}},
	}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, 1, github.recentCalls)
	require.Equal(t, []string{SettingKeyOpenAICodexClientVersionSynced + "=0.146.0"}, repo.writes)
}

func TestOpenAICodexVersionSyncFallsBackWhenLatestFetchFails(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{}}
	github := &codexVersionGitHubStub{
		latestErr: errors.New("latest unavailable"),
		releases:  []*GitHubRelease{{TagName: "rust-v0.146.0"}},
	}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runOnce()

	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, 1, github.recentCalls)
	require.Equal(t, []string{SettingKeyOpenAICodexClientVersionSynced + "=0.146.0"}, repo.writes)
}

func TestOpenAICodexVersionSyncLifecycleToleratesMissingDependencies(t *testing.T) {
	var nilService *OpenAICodexVersionSyncService
	require.NotPanics(t, func() {
		nilService.Start()
		nilService.Stop()
	})

	svc := NewOpenAICodexVersionSyncService(nil, nil, nil, openAICodexVersionSyncInterval)
	require.NotPanics(t, func() {
		svc.Start()
		svc.Stop()
	})
}

func TestOpenAICodexVersionSyncStartIsIdempotent(t *testing.T) {
	repo := &codexVersionLifecycleRepoStub{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	svc := NewOpenAICodexVersionSyncService(repo, nil, &codexVersionGitHubStub{}, time.Hour)
	svc.Start()
	svc.Start()

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		close(repo.release)
		svc.Stop()
		t.Fatal("version sync worker did not start")
	}

	select {
	case <-repo.started:
		close(repo.release)
		svc.Stop()
		t.Fatal("repeated Start launched a second version sync worker")
	case <-time.After(50 * time.Millisecond):
	}

	close(repo.release)
	svc.Stop()
	require.Equal(t, int32(1), repo.getCalls.Load())
}

func TestOpenAICodexVersionSyncDoesNotStartAfterStop(t *testing.T) {
	repo := &codexVersionLifecycleRepoStub{}
	svc := NewOpenAICodexVersionSyncService(repo, nil, &codexVersionGitHubStub{}, time.Hour)

	svc.Stop()
	svc.Start()
	time.Sleep(50 * time.Millisecond)

	require.Zero(t, repo.getCalls.Load())
}

func TestOpenAICodexVersionSyncInitialSkipsRecentlySyncedValue(t *testing.T) {
	repo := &codexVersionSettingRepoStub{
		values:    map[string]string{SettingKeyOpenAICodexClientVersionSynced: "0.146.0"},
		updatedAt: time.Now().Add(-time.Hour),
	}
	github := &codexVersionGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.147.0"}}}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runInitial()

	require.Zero(t, github.latestCalls)
	require.Zero(t, github.recentCalls)
	require.Empty(t, repo.writes)
}

func TestOpenAICodexVersionSyncInitialRunsForStaleValue(t *testing.T) {
	repo := &codexVersionSettingRepoStub{
		values:    map[string]string{SettingKeyOpenAICodexClientVersionSynced: "0.146.0"},
		updatedAt: time.Now().Add(-7 * time.Hour),
	}
	github := &codexVersionGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.147.0"}}
	svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, openAICodexVersionSyncInterval)

	svc.runInitial()

	require.Equal(t, []string{SettingKeyOpenAICodexClientVersionSynced + "=0.147.0"}, repo.writes)
}

func TestProvideSettingServicePublishesCodexCanonicalUserAgentResolver(t *testing.T) {
	repo := &codexVersionSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexClientVersionSynced:       "0.200.1",
		SettingKeyCodexCLIOnlyEngineFingerprintSignals: "configured",
	}}
	SetCodexCanonicalUserAgentResolver(nil)
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })

	ProvideSettingService(repo, nil, nil, nil)

	require.Equal(t, "codex-tui/0.200.1"+codexCLIUserAgentSuffix, CodexCanonicalUserAgent())
}

func TestNewOpenAIGatewayServicePublishesCodexIdentityEnforcementConfig(t *testing.T) {
	SetCodexIdentityEnforcementEnabled(true)
	t.Cleanup(func() { SetCodexIdentityEnforcementEnabled(true) })

	cfg := &config.Config{}
	cfg.Gateway.DisableCodexIdentityEnforcement = true
	NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	h := make(http.Header)
	h.Set("originator", "codex-tui")
	h.Set("user-agent", "codex-tui/0.145.0")
	h.Set("version", "0.145.0")
	enforceCodexIdentityHeaders(h)
	require.Equal(t, "codex-tui/0.145.0", h.Get("user-agent"))
}

func TestProvideOpenAICodexVersionSyncServiceToleratesMissingDependencies(t *testing.T) {
	var svc *OpenAICodexVersionSyncService
	require.NotPanics(t, func() {
		svc = ProvideOpenAICodexVersionSyncService(nil, nil, nil)
	})
	require.NotNil(t, svc)
	svc.Stop()
}
