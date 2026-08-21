package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	sub2api "github.com/Wei-Shaw/sub2api/internal/runtime/sub2api"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

// sub2APISyncPort exposes the existing OpenAI account scheduler and exchange
// forwarding methods to the pure Driver for embeddings, alpha search and
// OpenAI count-tokens requests.
type sub2APISyncPort struct {
	service     *service.OpenAIGatewayService
	maxSwitches int
}

func (p *sub2APISyncPort) Select(ctx context.Context, request v1.Request, excluded map[int64]struct{}, capability string) (sub2api.AccountSelection, error) {
	if p == nil || p.service == nil {
		return sub2api.AccountSelection{}, ErrSub2APIRuntimeUnavailable
	}
	useUpstreamTokenCost := capability == "embeddings"
	selection, _, err := p.service.SelectAccountWithSchedulerForCapability(
		ctx,
		service.PlatformSchedulingID(ctx),
		"",
		firstNonEmptyHandler(request.Session.SessionID, request.RequestID),
		strings.TrimSpace(request.Platform.RequestedModel),
		excluded,
		service.OpenAIUpstreamTransportHTTPSSE,
		openAIEndpointCapability(capability),
		false,
		false,
		useUpstreamTokenCost,
		request.Platform.RuntimeAdapter,
	)
	if err != nil {
		if request.Endpoint == v1.EndpointCountTokens {
			return sub2api.AccountSelection{}, &sub2api.RetryError{
				StatusCode:         http.StatusBadGateway,
				ClientErrorType:    "api_error",
				ClientErrorMessage: "No available accounts",
				Cause:              err,
			}
		}
		return sub2api.AccountSelection{}, err
	}
	if selection == nil || selection.Account == nil {
		if request.Endpoint == v1.EndpointCountTokens {
			return sub2api.AccountSelection{}, &sub2api.RetryError{
				StatusCode:         http.StatusBadGateway,
				ClientErrorType:    "api_error",
				ClientErrorMessage: "No available accounts",
				Cause:              service.ErrNoAvailableAccounts,
			}
		}
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

func (p *sub2APISyncPort) MaxSwitches() int {
	if p == nil || p.maxSwitches <= 0 {
		return 3
	}
	return p.maxSwitches
}

func (p *sub2APISyncPort) ReportScheduleResult(_ context.Context, accountID int64, model string, success bool, firstTokenMs *int) {
	if p != nil && p.service != nil {
		p.service.ReportOpenAIAccountScheduleResult(accountID, model, success, firstTokenMs)
	}
}

func (p *sub2APISyncPort) RecordAccountSwitch(_ context.Context) {
	if p != nil && p.service != nil {
		p.service.RecordOpenAIAccountSwitch()
	}
}

func (p *sub2APISyncPort) forward(ctx context.Context, request v1.Request, account *service.Account) (sub2api.ForwardResult, error) {
	if p == nil || p.service == nil || account == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeUnavailable
	}
	exchange, ok := runtimebridge.LocalExchangeFromContext(ctx)
	if !ok || exchange == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	legacyRequest := gatewayRequestFromContract(request, exchange)
	var result *service.OpenAIForwardResult
	var err error
	switch gatewayruntime.Endpoint(request.Endpoint) {
	case gatewayruntime.EndpointEmbeddings:
		result, err = p.service.ForwardEmbeddingsRuntime(ctx, exchange, account, request.Payload, request.Platform.UpstreamModel)
	case gatewayruntime.EndpointAlphaSearch:
		result, err = p.service.ForwardAlphaSearchRuntime(ctx, exchange, account, request.Payload, request.Owner.APIKeyID)
	case gatewayruntime.EndpointCountTokens:
		parsed, parseErr := service.ParseGatewayRequest(service.NewRequestBodyRef(request.Payload), domain.PlatformAnthropic)
		if parseErr != nil || parsed == nil {
			if parseErr == nil {
				parseErr = errors.New("count_tokens request is invalid")
			}
			return sub2api.ForwardResult{}, parseErr
		}
		err = p.service.ForwardCountTokensAsAnthropicRuntime(ctx, exchange, account, parsed.Body.Bytes(), request.Platform.UpstreamModel, request.Owner.APIKeyID)
		status := runtimeExchangeStatus(exchange)
		if status == 0 {
			status = http.StatusOK
		}
		if err == nil && (status < http.StatusOK || status >= http.StatusMultipleChoices) {
			err = errors.New("count_tokens upstream returned a non-success status")
		}
		if err != nil {
			return sub2api.ForwardResult{StatusCode: status, AccountID: account.ID}, sub2APIRetryErrorOr(err)
		}
		return sub2api.ForwardResult{StatusCode: status, AccountID: account.ID, UpstreamModel: request.Platform.UpstreamModel, Usage: contractUsageFacts(request, gatewayruntime.UsageFacts{AccountID: account.ID, Model: parsed.Model, RequestedModel: request.Platform.RequestedModel, UpstreamModel: request.Platform.UpstreamModel, InboundEndpoint: request.InboundEndpoint, TerminalStatus: http.StatusText(status)}, exchange)}, nil
	default:
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	if err != nil {
		return sub2api.ForwardResult{}, sub2APIRetryErrorOr(err)
	}
	if result == nil {
		return sub2api.ForwardResult{}, errors.New("sync runtime returned an empty result")
	}
	status := runtimeExchangeStatus(exchange)
	if status == 0 {
		status = http.StatusOK
	}
	return sub2api.ForwardResult{
		StatusCode:       status,
		Streamed:         result.Stream,
		AccountID:        account.ID,
		ResponseHeaders:  cloneResponseHeaders(result.ResponseHeaders),
		UpstreamEndpoint: firstNonEmptyHandler(result.UpstreamEndpoint, defaultSyncUpstreamEndpoint(gatewayruntime.Endpoint(request.Endpoint))),
		UpstreamModel:    result.UpstreamModel,
		Usage:            contractUsageFacts(request, openAIForwardUsageFacts(legacyRequest, account, result), exchange),
	}, nil
}

var _ sub2api.OpenAIRuntimePort = (*sub2APISyncPort)(nil)
