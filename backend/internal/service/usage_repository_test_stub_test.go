//go:build unit

package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type openAIRecordUsageLogRepoStub struct {
	UsageLogRepository

	inserted   bool
	err        error
	calls      int
	lastLog    *UsageLog
	lastCtxErr error
}

func (s *openAIRecordUsageLogRepoStub) Create(ctx context.Context, log *UsageLog) (bool, error) {
	s.calls++
	s.lastLog = log
	s.lastCtxErr = ctx.Err()
	return s.inserted, s.err
}

type openAIRecordUsageBillingRepoStub struct {
	UsageBillingRepository
	result     *UsageBillingApplyResult
	err        error
	calls      int
	lastCmd    *UsageBillingCommand
	lastCtxErr error
}

func (s *openAIRecordUsageBillingRepoStub) Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.calls++
	s.lastCmd = cmd
	s.lastCtxErr = ctx.Err()
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &UsageBillingApplyResult{Applied: true}, nil
}

type openAIRecordUsageUserRepoStub struct {
	UserRepository
	deductCalls int
	deductErr   error
	lastAmount  float64
	lastCtxErr  error
}

func (s *openAIRecordUsageUserRepoStub) DeductBalance(ctx context.Context, _ int64, amount float64) error {
	s.deductCalls++
	s.lastAmount = amount
	s.lastCtxErr = ctx.Err()
	return s.deductErr
}

type openAIRecordUsageSubRepoStub struct {
	UserSubscriptionRepository
	incrementCalls int
	incrementErr   error
	lastCtxErr     error
}

func (s *openAIRecordUsageSubRepoStub) IncrementUsage(ctx context.Context, _ int64, _ float64) error {
	s.incrementCalls++
	s.lastCtxErr = ctx.Err()
	return s.incrementErr
}

type openAIRecordUsageAPIKeyQuotaStub struct {
	quotaCalls          int
	rateLimitCalls      int
	err                 error
	lastAmount          float64
	lastQuotaCtxErr     error
	lastRateLimitCtxErr error
}

func (s *openAIRecordUsageAPIKeyQuotaStub) UpdateQuotaUsed(ctx context.Context, _ int64, cost float64) error {
	s.quotaCalls++
	s.lastAmount = cost
	s.lastQuotaCtxErr = ctx.Err()
	return s.err
}

func (s *openAIRecordUsageAPIKeyQuotaStub) UpdateRateLimitUsage(ctx context.Context, _ int64, cost float64) error {
	s.rateLimitCalls++
	s.lastAmount = cost
	s.lastRateLimitCtxErr = ctx.Err()
	return s.err
}

func newOpenAIRecordUsageServiceWithBillingRepoForTest(
	usageRepo UsageLogRepository,
	billingRepo UsageBillingRepository,
	userRepo UserRepository,
	subRepo UserSubscriptionRepository,
	_ any,
) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1.1
	return NewOpenAIGatewayService(
		nil, usageRepo, billingRepo, userRepo, subRepo, nil, cfg, nil, nil,
		NewBillingService(cfg, nil), nil, &BillingCacheService{}, nil,
		&DeferredService{}, nil, nil, nil, nil, nil, nil,
	)
}
