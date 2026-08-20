package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

type ProductUsageAPIKeyLoader interface {
	GetByID(context.Context, int64) (*APIKey, error)
	UpdateQuotaUsed(context.Context, int64, float64) error
	UpdateRateLimitUsage(context.Context, int64, float64) error
}

// Sub2APIProductUsageFinalizer is the compatibility implementation of the
// product-side usage port. It reconstructs the existing service input from
// immutable IDs and facts, then delegates to the existing atomic billing
// transaction without moving pricing or subscription rules into Runtime.
type Sub2APIProductUsageFinalizer struct {
	gateway *GatewayService
	openai  *OpenAIGatewayService
	apiKeys ProductUsageAPIKeyLoader
	account AccountRepository
	users   UserRepository
	plans   UserSubscriptionRepository
}

func NewSub2APIProductUsageFinalizer(
	gateway *GatewayService,
	openai *OpenAIGatewayService,
	apiKeys ProductUsageAPIKeyLoader,
) *Sub2APIProductUsageFinalizer {
	f := &Sub2APIProductUsageFinalizer{gateway: gateway, openai: openai, apiKeys: apiKeys}
	if gateway != nil {
		f.account = gateway.accountRepo
		f.users = gateway.userRepo
		f.plans = gateway.userSubRepo
	} else if openai != nil {
		f.account = openai.accountRepo
		f.users = openai.userRepo
		f.plans = openai.userSubRepo
	}
	return f
}

func NewSub2APIProductUsageSinkFactory(
	gateway *GatewayService,
	openai *OpenAIGatewayService,
	apiKeys ProductUsageAPIKeyLoader,
) *ProductUsageSinkFactory {
	return NewProductUsageSinkFactory(NewSub2APIProductUsageFinalizer(gateway, openai, apiKeys))
}

func (f *Sub2APIProductUsageFinalizer) Finalize(ctx context.Context, record ProductUsageRecord) error {
	if !record.Event.Success {
		return nil
	}
	if f == nil || f.apiKeys == nil || f.account == nil || f.users == nil || f.plans == nil {
		return ErrProductUsageFinalizerUnavailable
	}
	grant := record.Snapshot.Grant
	if grant.KeyID <= 0 || grant.UserID <= 0 || record.Event.Facts.AccountID <= 0 {
		return errors.New("product usage record identifiers are incomplete")
	}
	apiKey, err := f.apiKeys.GetByID(ctx, grant.KeyID)
	if err != nil {
		return err
	}
	if apiKey == nil {
		return errors.New("api key not found for product usage")
	}
	user, err := f.users.GetByID(ctx, grant.UserID)
	if err != nil {
		return err
	}
	account, err := f.account.GetByID(ctx, record.Event.Facts.AccountID)
	if err != nil {
		return err
	}
	if user == nil || account == nil {
		return errors.New("product usage owner or account not found")
	}
	apiKey.User = user

	var subscription *UserSubscription
	if asset := record.Snapshot.Decision.BillingAsset; asset != nil && asset.SubscriptionID != nil {
		subscription, err = f.plans.GetByID(ctx, *asset.SubscriptionID)
		if err != nil {
			return err
		}
	}
	ctx = attachProductDecision(ctx, &record.Snapshot.Decision, subscription)
	facts := record.Event.Facts
	if isOpenAIUsageAdapter(facts.Adapter, record.Snapshot.Decision.Platform.AccountPlatform) {
		if f.openai == nil {
			return ErrProductUsageFinalizerUnavailable
		}
		return f.openai.RecordUsage(ctx, &OpenAIRecordUsageInput{
			Result: openAIForwardResultFromProductUsage(record.Event),
			APIKey: apiKey, User: user, Account: account, Subscription: subscription,
			InboundEndpoint: facts.InboundEndpoint, UpstreamEndpoint: facts.UpstreamEndpoint,
			UserAgent: facts.UserAgent, IPAddress: facts.ClientIP, SessionID: facts.SessionID,
			RequestPayloadHash: facts.RequestPayloadHash, APIKeyService: f.apiKeys,
			ModelRoutingUsageFields: ModelRoutingUsageFields{
				OriginalModel: facts.OriginalModel, MappedModel: facts.MappedModel,
				BillingModelSource: facts.BillingModelSource, ModelMappingChain: facts.ModelMappingChain,
			},
			QuotaPlatform: account.Platform,
			CyberBlocked:  facts.CyberBlocked,
		})
	}
	if f.gateway == nil {
		return ErrProductUsageFinalizerUnavailable
	}
	return f.gateway.RecordUsage(ctx, &RecordUsageInput{
		Result: gatewayForwardResultFromProductUsage(record.Event),
		APIKey: apiKey, User: user, Account: account, Subscription: subscription,
		InboundEndpoint: facts.InboundEndpoint, UpstreamEndpoint: facts.UpstreamEndpoint,
		UserAgent: facts.UserAgent, IPAddress: facts.ClientIP, SessionID: facts.SessionID,
		RequestPayloadHash: facts.RequestPayloadHash, ForceCacheBilling: facts.ForceCacheBilling,
		APIKeyService: f.apiKeys, QuotaPlatform: account.Platform,
		ModelRoutingUsageFields: ModelRoutingUsageFields{
			OriginalModel: facts.OriginalModel, MappedModel: facts.MappedModel,
			BillingModelSource: facts.BillingModelSource, ModelMappingChain: facts.ModelMappingChain,
		},
	})
}

func openAIForwardResultFromProductUsage(event gatewayruntime.UsageEvent) *OpenAIForwardResult {
	facts := event.Facts
	return &OpenAIForwardResult{
		RequestID:        event.RequestID,
		Model:            firstNonEmptyProduct(facts.Model, facts.RequestedModel),
		BillingModel:     facts.BillingModel,
		UpstreamModel:    facts.UpstreamModel,
		UpstreamEndpoint: facts.UpstreamEndpoint,
		ServiceTier:      stringPointerProduct(facts.ServiceTier),
		ReasoningEffort:  stringPointerProduct(facts.ReasoningEffort),
		Usage: OpenAIUsage{
			InputTokens:              facts.InputTokens,
			OutputTokens:             facts.OutputTokens,
			CacheCreationInputTokens: facts.CacheCreationTokens,
			CacheReadInputTokens:     facts.CacheReadTokens,
			ImageInputTokens:         facts.ImageInputTokens,
			ImageOutputTokens:        facts.ImageOutputTokens,
		},
		ImageCount:   facts.ImageCount,
		VideoCount:   facts.VideoCount,
		Stream:       facts.RequestWasClientStream,
		Duration:     productUsageDuration(facts.DurationMilliseconds),
		FirstTokenMs: productUsageFirstToken(facts.FirstTokenMilliseconds),
	}
}

func gatewayForwardResultFromProductUsage(event gatewayruntime.UsageEvent) *ForwardResult {
	facts := event.Facts
	return &ForwardResult{
		RequestID:     event.RequestID,
		Model:         firstNonEmptyProduct(facts.Model, facts.RequestedModel),
		UpstreamModel: facts.UpstreamModel,
		Usage: ClaudeUsage{
			InputTokens:              facts.InputTokens,
			OutputTokens:             facts.OutputTokens,
			CacheCreationInputTokens: facts.CacheCreationTokens,
			CacheReadInputTokens:     facts.CacheReadTokens,
			ImageOutputTokens:        facts.ImageOutputTokens,
		},
		Stream:       facts.RequestWasClientStream,
		Duration:     productUsageDuration(facts.DurationMilliseconds),
		FirstTokenMs: productUsageFirstToken(facts.FirstTokenMilliseconds),
	}
}

func productUsageDuration(milliseconds int64) time.Duration {
	if milliseconds <= 0 {
		return 0
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func productUsageFirstToken(milliseconds int64) *int {
	if milliseconds <= 0 {
		return nil
	}
	value := int(milliseconds)
	return &value
}

func isOpenAIUsageAdapter(adapter, fallback string) bool {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		adapter = strings.TrimSpace(fallback)
	}
	return adapter == PlatformOpenAI || adapter == PlatformGrok
}

func firstNonEmptyProduct(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stringPointerProduct(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

var _ ProductUsageFinalizer = (*Sub2APIProductUsageFinalizer)(nil)
var _ gatewayruntime.UsageSink = (*productUsageSink)(nil)
