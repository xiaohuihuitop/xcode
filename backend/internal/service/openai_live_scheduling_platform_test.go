package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type liveSchedulingCapture struct {
	request   OpenAIAccountScheduleRequest
	selection *AccountSelectionResult
	err       error
}

func (s *liveSchedulingCapture) Select(_ context.Context, request OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	s.request = request
	return s.selection, OpenAIAccountScheduleDecision{}, s.err
}

func (s *liveSchedulingCapture) ReportResult(int64, bool, *int) {}
func (s *liveSchedulingCapture) ReportSwitch()                  {}
func (s *liveSchedulingCapture) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	return OpenAIAccountSchedulerMetricsSnapshot{}
}

type liveSchedulingStore struct {
	*liveTestStore
}

func (s *liveSchedulingStore) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (s *liveSchedulingStore) ClaimLiveController(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func TestCreateLiveCallUsesSchedulingNamespaceAndKeepsBusinessIdentity(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Minute).UnixNano(),
	})

	sentinel := errors.New("stop after scheduling capture")
	scheduler := &liveSchedulingCapture{err: sentinel}
	store := &liveSchedulingStore{liveTestStore: &liveTestStore{}}
	svc := &OpenAIGatewayService{
		cache:                 store,
		concurrencyService:    NewConcurrencyService(&liveTestConcurrencyCache{}),
		liveAttestation:       liveAttestationStub{header: `{"v":1,"s":0,"t":"v1.test"}`},
		liveAttestationCipher: newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "live-scheduling-test"}}),
		openaiScheduler:       scheduler,
	}
	platformID := int64(42)
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID:      platformID,
		PlatformCode:    "openai",
		AccountPlatform: PlatformOpenAI,
	})

	_, err := svc.CreateLiveCall(ctx, &LiveCallRequest{
		SDP:     "v=0\r\n",
		Session: []byte(`{"model":"gpt-live"}`),
	}, LiveCallIdentity{APIKeyID: 7, UserID: 9, PlatformID: &platformID}, 1)

	require.ErrorIs(t, err, sentinel)
	require.NotNil(t, scheduler.request.PlatformID)
	require.Equal(t, int64(-43), *scheduler.request.PlatformID)
	require.Equal(t, int64(42), platformID, "business identity must remain unchanged")
}

func TestCreateLiveCallPersistsBusinessPlatformIDAfterScheduling(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled:   true,
		expiresAt: time.Now().Add(time.Minute).UnixNano(),
	})

	platformID := int64(42)
	account := &Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		PlatformID:  &platformID,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acct_test",
		},
	}
	scheduler := &liveSchedulingCapture{selection: &AccountSelectionResult{
		Account:     account,
		Acquired:    true,
		ReleaseFunc: func() {},
	}}
	store := &liveSchedulingStore{liveTestStore: &liveTestStore{}}
	svc := &OpenAIGatewayService{
		cache:                 store,
		concurrencyService:    NewConcurrencyService(&liveTestConcurrencyCache{}),
		httpUpstream:          &liveHTTPUpstreamStub{},
		liveAttestation:       liveAttestationStub{header: `{"v":1,"s":0,"t":"v1.test"}`},
		liveAttestationCipher: newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "live-persistence-test"}}),
		openaiScheduler:       scheduler,
	}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID:      platformID,
		PlatformCode:    "openai",
		AccountPlatform: PlatformOpenAI,
	})

	created, err := svc.CreateLiveCall(ctx, &LiveCallRequest{
		SDP:     "v=0\r\n",
		Session: []byte(`{"model":"gpt-live"}`),
	}, LiveCallIdentity{APIKeyID: 7, UserID: 9, PlatformID: &platformID}, 1)

	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotNil(t, scheduler.request.PlatformID)
	require.Equal(t, int64(-43), *scheduler.request.PlatformID)
	require.NotNil(t, store.record)
	require.Equal(t, int64(42), store.record.PlatformID)
}
