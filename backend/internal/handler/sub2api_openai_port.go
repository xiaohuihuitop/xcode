package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	sub2api "github.com/Wei-Shaw/sub2api/internal/runtime/sub2api"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

// sub2APIOpenAIPort is the composition-root adapter for the pure OpenAI
// Driver. It translates scalar contract values into the existing Sub2API
// scheduling/OAuth service without exposing Handler or Gin to the Driver.
type sub2APIOpenAIPort struct {
	service       *service.OpenAIGatewayService
	legacyGateway *service.GatewayService
	maxSwitches   int
}

func (p *sub2APIOpenAIPort) Select(ctx context.Context, request v1.Request, excluded map[int64]struct{}, capability string) (sub2api.AccountSelection, error) {
	if p == nil || p.service == nil {
		return sub2api.AccountSelection{}, ErrSub2APIRuntimeUnavailable
	}
	pricingModel := firstNonEmptyHandler(request.Platform.UpstreamModel, request.Platform.RequestedModel)
	if err := p.service.ValidateRequestPricing(ctx, pricingModel); err != nil {
		return sub2api.AccountSelection{}, err
	}
	exchange, _ := runtimebridge.LocalExchangeFromContext(ctx)
	legacyRequest := gatewayRequestFromContract(request, exchange)
	sessionHash := strings.TrimSpace(request.Session.SessionID)
	if sessionHash == "" && p.legacyGateway != nil {
		sessionHash = runtimeOpenAISessionHash(legacyRequest, p.legacyGateway)
	}
	selection, _, err := p.service.SelectAccountWithSchedulerForCapability(
		ctx,
		service.PlatformSchedulingID(ctx),
		"",
		sessionHash,
		strings.TrimSpace(request.Platform.RequestedModel),
		excluded,
		service.OpenAIUpstreamTransportAny,
		openAIEndpointCapability(capability),
		false,
		true,
		true,
		request.Platform.RuntimeAdapter,
	)
	if err != nil {
		return sub2api.AccountSelection{}, err
	}
	if selection == nil || selection.Account == nil {
		return sub2api.AccountSelection{}, service.ErrNoAvailableAccounts
	}
	account := selection.Account
	return sub2api.AccountSelection{
		ID:          account.ID,
		Platform:    account.Platform,
		AccountType: account.Type,
		Release:     selection.ReleaseFunc,
		Forward: func(forwardCtx context.Context, forwardRequest v1.Request) (sub2api.ForwardResult, error) {
			return p.forward(forwardCtx, forwardRequest, account)
		},
	}, nil
}

func (p *sub2APIOpenAIPort) MaxSwitches() int {
	if p == nil || p.maxSwitches <= 0 {
		return 3
	}
	return p.maxSwitches
}

func (p *sub2APIOpenAIPort) ReportScheduleResult(_ context.Context, accountID int64, model string, success bool, firstTokenMs *int) {
	if p != nil && p.service != nil {
		p.service.ReportOpenAIAccountScheduleResult(accountID, model, success, firstTokenMs)
	}
}

func (p *sub2APIOpenAIPort) RecordAccountSwitch(_ context.Context) {
	if p != nil && p.service != nil {
		p.service.RecordOpenAIAccountSwitch()
	}
}

func (p *sub2APIOpenAIPort) ShouldStopOAuth429Failover(_ context.Context, selection sub2api.AccountSelection, statusCode, failedSwitches int, state *sub2api.FailoverState) bool {
	if p == nil || p.service == nil {
		return false
	}
	account := &service.Account{ID: selection.ID, Platform: selection.Platform, Type: selection.AccountType}
	var followupPending *bool
	if state != nil {
		followupPending = &state.OAuth429FollowupPending
	}
	return p.service.ShouldStopOpenAIOAuth429FailoverRuntime(account, statusCode, failedSwitches, followupPending)
}

func (p *sub2APIOpenAIPort) forward(ctx context.Context, request v1.Request, account *service.Account) (sub2api.ForwardResult, error) {
	if p == nil || p.service == nil || account == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeUnavailable
	}
	exchange, ok := runtimebridge.LocalExchangeFromContext(ctx)
	if !ok || exchange == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	legacyRequest := gatewayRequestFromContract(request, exchange)
	service.ClearActualOpenAIUpstreamEndpointExchange(exchange)
	var result *service.OpenAIForwardResult
	var err error
	switch gatewayruntime.Endpoint(request.Endpoint) {
	case gatewayruntime.EndpointChatCompletions:
		result, err = p.service.ForwardAsChatCompletionsRuntime(ctx, exchange, account, request.Payload, request.Session.SessionID, request.Platform.UpstreamModel, request.Owner.APIKeyID)
	case gatewayruntime.EndpointResponses:
		result, err = p.service.ForwardRuntime(ctx, exchange, account, request.Payload, request.Owner.APIKeyID)
	case gatewayruntime.EndpointMessages:
		result, err = p.service.ForwardAsAnthropicRuntime(ctx, exchange, account, request.Payload, request.Session.SessionID, request.Platform.UpstreamModel, request.Owner.APIKeyID)
	default:
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	if err != nil {
		return sub2api.ForwardResult{}, sub2APIRetryErrorOr(err)
	}
	if result == nil {
		return sub2api.ForwardResult{}, errors.New("openai runtime returned an empty result")
	}
	status := runtimeExchangeStatus(exchange)
	if status == 0 {
		status = http.StatusOK
	}
	legacyFacts := openAIForwardUsageFacts(legacyRequest, account, result)
	return sub2api.ForwardResult{
		StatusCode:       status,
		Streamed:         result.Stream,
		AccountID:        account.ID,
		ResponseHeaders:  cloneResponseHeaders(result.ResponseHeaders),
		UpstreamEndpoint: firstNonEmptyHandler(result.UpstreamEndpoint, service.ActualOpenAIUpstreamEndpointFromExchange(exchange)),
		UpstreamModel:    result.UpstreamModel,
		Usage:            contractUsageFacts(request, legacyFacts, exchange),
	}, nil
}

func cloneResponseHeaders(headers http.Header) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func contractUsageFacts(request v1.Request, facts gatewayruntime.UsageFacts, exchanges ...gatewayruntime.HTTPExchange) v1.UsageFacts {
	partiallySent := facts.ResponseWasPartiallySent
	if len(exchanges) > 0 && exchanges[0] != nil {
		partiallySent = partiallySent || exchanges[0].Size() > 0
	}
	return v1.UsageFacts{
		Adapter:                  firstNonEmptyHandler(facts.Adapter, request.Platform.RuntimeAdapter),
		AccountID:                facts.AccountID,
		PlatformID:               request.Platform.ID,
		Endpoint:                 string(request.Endpoint),
		RequestedModel:           firstNonEmptyHandler(facts.RequestedModel, request.Platform.RequestedModel),
		UpstreamModel:            firstNonEmptyHandler(facts.UpstreamModel, request.Platform.UpstreamModel),
		UpstreamEndpoint:         facts.UpstreamEndpoint,
		Model:                    facts.Model,
		ServiceTier:              facts.ServiceTier,
		ReasoningEffort:          facts.ReasoningEffort,
		BillingModel:             facts.BillingModel,
		OriginalModel:            facts.OriginalModel,
		MappedModel:              facts.MappedModel,
		BillingModelSource:       facts.BillingModelSource,
		ModelMappingChain:        facts.ModelMappingChain,
		InputTokens:              facts.InputTokens,
		OutputTokens:             facts.OutputTokens,
		CacheCreationTokens:      facts.CacheCreationTokens,
		CacheReadTokens:          facts.CacheReadTokens,
		ImageInputTokens:         facts.ImageInputTokens,
		ImageOutputTokens:        facts.ImageOutputTokens,
		ImageCount:               facts.ImageCount,
		VideoCount:               facts.VideoCount,
		ForceCacheBilling:        facts.ForceCacheBilling,
		CyberBlocked:             facts.CyberBlocked,
		LongContextThreshold:     facts.LongContextThreshold,
		LongContextMultiplier:    facts.LongContextMultiplier,
		FirstTokenMilliseconds:   facts.FirstTokenMilliseconds,
		DurationMilliseconds:     facts.DurationMilliseconds,
		TerminalStatus:           facts.TerminalStatus,
		RequestWasClientStream:   facts.RequestWasClientStream || request.Stream,
		ResponseWasPartiallySent: partiallySent,
		InboundEndpoint:          facts.InboundEndpoint,
		UserAgent:                facts.UserAgent,
		ClientIP:                 facts.ClientIP,
		SessionID:                facts.SessionID,
		RequestPayloadHash:       facts.RequestPayloadHash,
	}
}

func sub2APIRetryErrorOr(err error) error {
	var failover *service.UpstreamFailoverError
	if !errors.As(err, &failover) || failover == nil {
		return err
	}
	return sub2APIRetryError(failover)
}

func sub2APIRetryError(failover *service.UpstreamFailoverError) *sub2api.RetryError {
	if failover == nil {
		return nil
	}
	return &sub2api.RetryError{
		StatusCode:               failover.StatusCode,
		RetryNextAccount:         failover.ShouldRetryNextAccount(),
		RetrySameAccount:         failover.RetryableOnSameAccount,
		ReportSchedule:           failover.ShouldReportAccountScheduleFailure(),
		SafeToFailoverAfterWrite: failover.SafeToFailoverAfterWrite,
		Cause:                    failover,
	}
}

func openAIEndpointCapability(capability string) service.OpenAIEndpointCapability {
	switch strings.ToLower(strings.TrimSpace(capability)) {
	case "responses":
		return service.OpenAIEndpointCapabilityResponses
	case "embeddings":
		return service.OpenAIEndpointCapabilityEmbeddings
	case "alpha_search":
		return service.OpenAIEndpointCapabilityAlphaSearch
	default:
		return service.OpenAIEndpointCapabilityChatCompletions
	}
}

var _ sub2api.OpenAIRuntimePort = (*sub2APIOpenAIPort)(nil)
