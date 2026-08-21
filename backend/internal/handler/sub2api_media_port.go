package handler

import (
	"context"
	"errors"
	"net/http"

	sub2api "github.com/Wei-Shaw/sub2api/internal/runtime/sub2api"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

type sub2APIMediaPort struct {
	service     *service.OpenAIGatewayService
	maxSwitches int
}

func (p *sub2APIMediaPort) Select(ctx context.Context, request v1.Request, excluded map[int64]struct{}, _ string) (sub2api.AccountSelection, error) {
	if p == nil || p.service == nil {
		return sub2api.AccountSelection{}, ErrSub2APIRuntimeUnavailable
	}
	exchange, ok := runtimebridge.LocalExchangeFromContext(ctx)
	if !ok || exchange == nil {
		return sub2api.AccountSelection{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	parsed, err := p.service.ParseOpenAIImagesRequestRuntime(ctx, exchange, request.Payload)
	if err != nil || parsed == nil {
		if err == nil {
			err = errors.New("images request is invalid")
		}
		return sub2api.AccountSelection{}, err
	}
	model := firstNonEmptyHandler(request.Platform.RequestedModel, parsed.Model)
	selection, _, err := p.service.SelectAccountWithSchedulerForImages(
		ctx,
		service.PlatformSchedulingID(ctx),
		request.Session.SessionID,
		model,
		excluded,
		parsed.RequiredCapability,
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
			return p.forward(forwardCtx, forwardRequest, account, parsed)
		},
	}, nil
}

func (p *sub2APIMediaPort) MaxSwitches() int {
	if p == nil || p.maxSwitches <= 0 {
		return 3
	}
	return p.maxSwitches
}

func (p *sub2APIMediaPort) ReportScheduleResult(_ context.Context, accountID int64, model string, success bool, firstTokenMs *int) {
	if p != nil && p.service != nil {
		p.service.ReportOpenAIAccountScheduleResult(accountID, model, success, firstTokenMs)
	}
}

func (p *sub2APIMediaPort) RecordAccountSwitch(_ context.Context) {
	if p != nil && p.service != nil {
		p.service.RecordOpenAIAccountSwitch()
	}
}

func (p *sub2APIMediaPort) forward(ctx context.Context, request v1.Request, account *service.Account, parsed *service.OpenAIImagesRequest) (sub2api.ForwardResult, error) {
	if p == nil || p.service == nil || account == nil || parsed == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeUnavailable
	}
	exchange, ok := runtimebridge.LocalExchangeFromContext(ctx)
	if !ok || exchange == nil {
		return sub2api.ForwardResult{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	legacyRequest := gatewayRequestFromContract(request, exchange)
	model := firstNonEmptyHandler(request.Platform.RequestedModel, parsed.Model)
	result, err := p.service.ForwardImagesRuntime(ctx, exchange, account, request.Payload, parsed, request.Platform.UpstreamModel)
	if err != nil {
		return sub2api.ForwardResult{}, sub2APIRetryErrorOr(err)
	}
	if result == nil {
		return sub2api.ForwardResult{}, errors.New("images runtime returned an empty result")
	}
	status := runtimeExchangeStatus(exchange)
	if status == 0 {
		status = http.StatusOK
	}
	facts := openAIMediaUsageFacts(legacyRequest, account, result)
	facts.Model = model
	return sub2api.ForwardResult{
		StatusCode:       status,
		Streamed:         result.Stream,
		AccountID:        account.ID,
		ResponseHeaders:  cloneResponseHeaders(result.ResponseHeaders),
		UpstreamEndpoint: firstNonEmptyHandler(result.UpstreamEndpoint, facts.UpstreamEndpoint),
		UpstreamModel:    result.UpstreamModel,
		Usage:            contractUsageFacts(request, facts, exchange),
	}, nil
}

var _ sub2api.OpenAIRuntimePort = (*sub2APIMediaPort)(nil)
