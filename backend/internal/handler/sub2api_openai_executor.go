package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// sub2APIOpenAIExecutor owns the OpenAI-compatible protocol family at the
// runtime boundary. Account selection, failover and usage terminal ownership
// stay here; the service runtime methods retain the protocol conversion and
// OAuth transport details behind an exchange-only contract.
type sub2APIOpenAIExecutor struct {
	handler              *OpenAIGatewayHandler
	gatewayHandler       *GatewayHandler
	endpoint             gatewayruntime.Endpoint
	execute              func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error)
	selectAccountRuntime func(context.Context, gatewayruntime.Request, map[int64]struct{}, service.OpenAIEndpointCapability) (*service.AccountSelectionResult, error)
	forwardRuntime       func(context.Context, gatewayruntime.Request, *service.Account) (*service.OpenAIForwardResult, error)
}

func (e sub2APIOpenAIExecutor) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	terminal := &openAIExecutorTerminalSink{sink: sink}
	if e.execute != nil {
		result, err := e.execute(ctx, request, terminal)
		return e.ensureTerminal(ctx, request, result, err, terminal)
	}
	if e.forwardRuntime != nil || e.selectAccountRuntime != nil || e.handler != nil {
		result, err := e.executeRuntime(ctx, request, terminal)
		return e.ensureTerminal(ctx, request, result, err, terminal)
	}
	return gatewayruntime.Result{}, ErrSub2APIRuntimeUnavailable
}

func (e sub2APIOpenAIExecutor) ensureTerminal(
	ctx context.Context,
	request gatewayruntime.Request,
	result gatewayruntime.Result,
	err error,
	terminal *openAIExecutorTerminalSink,
) (gatewayruntime.Result, error) {
	if terminal.recorded() {
		return result, err
	}
	status := result.StatusCode
	if status == 0 {
		status = http.StatusBadGateway
	}
	event := gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   err == nil && status >= http.StatusOK && status < http.StatusBadRequest,
		Facts: gatewayruntime.UsageFacts{
			Adapter:                  request.Adapter,
			RequestedModel:           request.RequestedModel,
			UpstreamModel:            request.UpstreamModel,
			InboundEndpoint:          request.InboundEndpoint,
			RequestWasClientStream:   request.Stream,
			ResponseWasPartiallySent: result.Response.Streamed,
		},
	}
	if !event.Success {
		if err != nil {
			event.Error = gatewayruntime.RuntimeErrorFromContext(err)
		}
		if event.Error == nil {
			event.Error = gatewayruntime.RuntimeErrorFromStatus(status, http.StatusText(status))
		}
	}
	if recordErr := terminal.RecordFinal(ctx, event); recordErr != nil {
		if err != nil {
			return result, errors.Join(err, recordErr)
		}
		return result, recordErr
	}
	return result, err
}

func (e sub2APIOpenAIExecutor) executeRuntime(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
	if request.Exchange == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	execution, err := newSub2APIExecution(ctx, request, sink)
	if err != nil {
		return gatewayruntime.Result{}, err
	}
	ctx = execution.Context()
	request = execution.Request()

	model := runtimeOpenAIRequestModel(request)
	if model == "" {
		_ = writeOpenAIRuntimeError(request.Exchange, request.Endpoint, http.StatusBadRequest, "invalid_request_error", "model is required")
		return gatewayruntime.Result{StatusCode: http.StatusBadRequest}, errors.New("model is required")
	}

	selector := e.selectAccountRuntime
	forwarder := e.forwardRuntime
	sessionHash := strings.TrimSpace(request.Metadata.SessionID)
	if e.handler != nil && e.handler.gatewayService != nil {
		if sessionHash == "" {
			if e.gatewayHandler != nil && e.gatewayHandler.gatewayService != nil {
				sessionHash = runtimeOpenAISessionHash(request, e.gatewayHandler.gatewayService)
			}
		}
		if selector == nil {
			selector = func(ctx context.Context, request gatewayruntime.Request, excluded map[int64]struct{}, capability service.OpenAIEndpointCapability) (*service.AccountSelectionResult, error) {
				selection, _, err := e.handler.gatewayService.SelectAccountWithSchedulerForCapability(
					ctx,
					service.PlatformSchedulingID(ctx),
					"",
					sessionHash,
					model,
					excluded,
					service.OpenAIUpstreamTransportAny,
					capability,
					false,
					false,
					true,
					request.Adapter,
				)
				return selection, err
			}
		}
		if forwarder == nil {
			forwarder = func(ctx context.Context, request gatewayruntime.Request, account *service.Account) (*service.OpenAIForwardResult, error) {
				promptCacheKey := strings.TrimSpace(request.Metadata.SessionID)
				defaultMappedModel := strings.TrimSpace(request.UpstreamModel)
				svc := e.handler.gatewayService
				service.ClearActualOpenAIUpstreamEndpointExchange(request.Exchange)
				switch request.Endpoint {
				case gatewayruntime.EndpointChatCompletions:
					return svc.ForwardAsChatCompletionsRuntime(ctx, request.Exchange, account, request.Payload, promptCacheKey, defaultMappedModel, request.Metadata.APIKeyID)
				case gatewayruntime.EndpointResponses:
					return svc.ForwardRuntime(ctx, request.Exchange, account, request.Payload, request.Metadata.APIKeyID)
				case gatewayruntime.EndpointMessages:
					return svc.ForwardAsAnthropicRuntime(ctx, request.Exchange, account, request.Payload, promptCacheKey, defaultMappedModel, request.Metadata.APIKeyID)
				default:
					return nil, ErrSub2APIRuntimeEndpointUnavailable
				}
			}
		}
	}
	if selector == nil || forwarder == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}

	capability := runtimeOpenAIRequiredCapability(request, model)
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var oauth429State service.OpenAIOAuth429FailoverState
	firstOutputTimeoutSwitchCount := 0
	maxSwitches := 3
	if e.handler != nil && e.handler.maxAccountSwitches > 0 {
		maxSwitches = e.handler.maxAccountSwitches
	}
	var lastErr error
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		if ctx.Err() != nil {
			return gatewayruntime.Result{StatusCode: statusClientClosedRequest}, ctx.Err()
		}
		selection, selectErr := selector(ctx, request, failedAccountIDs, capability)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if selectErr != nil {
				lastErr = selectErr
			}
			break
		}
		account := selection.Account
		request.Exchange.SetState(opsAccountIDKey, account.ID)
		request.Exchange.SetState("ops_account_platform", account.Platform)
		writerSizeBeforeForward := request.Exchange.Size()
		service.ClearActualOpenAIUpstreamEndpointExchange(request.Exchange)
		result, forwardErr := forwarder(ctx, request, account)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if result != nil && strings.TrimSpace(result.UpstreamEndpoint) == "" {
			result.UpstreamEndpoint = service.ActualOpenAIUpstreamEndpointFromExchange(request.Exchange)
		}
		if forwardErr == nil && result != nil {
			e.reportScheduleResult(account, model, result, true)
			facts := openAIForwardUsageFacts(request, account, result)
			if err := sink.RecordFinal(ctx, gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true, Facts: facts}); err != nil {
				return gatewayruntime.Result{}, err
			}
			status := runtimeExchangeStatus(request.Exchange)
			if status == 0 {
				status = http.StatusOK
			}
			return gatewayruntime.Result{
				StatusCode:       status,
				AccountID:        account.ID,
				UpstreamEndpoint: facts.UpstreamEndpoint,
				UpstreamModel:    facts.UpstreamModel,
				Response:         gatewayruntime.Response{Streamed: request.Stream && request.Exchange.Size() > 0},
				Usage:            facts,
			}, nil
		}

		lastErr = forwardErr
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) || request.Exchange.Size() != writerSizeBeforeForward || !failoverErr.ShouldRetryNextAccount() {
			e.reportScheduleResult(account, model, result, false)
			break
		}
		if failoverErr.ShouldReportAccountScheduleFailure() {
			e.reportScheduleResult(account, model, result, false)
		}
		if failoverErr.RetryableOnSameAccount {
			retryLimit := account.GetPoolModeRetryCount()
			if sameAccountRetryCount[account.ID] < retryLimit {
				sameAccountRetryCount[account.ID]++
				select {
				case <-ctx.Done():
					return gatewayruntime.Result{StatusCode: statusClientClosedRequest}, ctx.Err()
				case <-time.After(sameAccountRetryDelay):
				}
				continue
			}
		}
		failedAccountIDs[account.ID] = struct{}{}
		if e.handler != nil && e.handler.gatewayService != nil {
			e.handler.gatewayService.RecordOpenAIAccountSwitch()
			if e.handler.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount+1, &oauth429State) {
				break
			}
		}
		if openAIFirstOutputFailoverExhausted(failoverErr, &firstOutputTimeoutSwitchCount) {
			break
		}
	}
	if !request.Exchange.Written() {
		status := http.StatusBadGateway
		if errors.Is(lastErr, context.Canceled) {
			status = statusClientClosedRequest
		}
		_ = writeOpenAIRuntimeError(request.Exchange, request.Endpoint, status, "upstream_error", "Upstream request failed")
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return gatewayruntime.Result{StatusCode: runtimeExchangeStatus(request.Exchange)}, lastErr
}

func runtimeOpenAISessionHash(request gatewayruntime.Request, gatewayService *service.GatewayService) string {
	if gatewayService == nil {
		return strings.TrimSpace(request.RequestID)
	}
	protocol := service.PlatformOpenAI
	if request.Endpoint == gatewayruntime.EndpointResponses {
		protocol = "responses"
	} else if request.Endpoint == gatewayruntime.EndpointMessages {
		protocol = service.PlatformAnthropic
	}
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(request.Payload), protocol)
	if err == nil && parsed != nil {
		parsed.SessionContext = &service.SessionContext{
			ClientIP:  request.Metadata.ClientIP,
			UserAgent: request.Metadata.UserAgent,
			APIKeyID:  request.Metadata.APIKeyID,
		}
		if hash := strings.TrimSpace(gatewayService.GenerateSessionHash(parsed)); hash != "" {
			return hash
		}
	}
	return strings.TrimSpace(request.RequestID)
}

func (e sub2APIOpenAIExecutor) reportScheduleResult(account *service.Account, requestedModel string, result *service.OpenAIForwardResult, success bool) {
	if e.handler == nil || e.handler.gatewayService == nil || account == nil {
		return
	}
	model := requestedModel
	if mapped := account.GetMappedModel(requestedModel); strings.TrimSpace(mapped) != "" {
		model = mapped
	}
	var firstTokenMs *int
	if result != nil {
		firstTokenMs = result.FirstTokenMs
	}
	e.handler.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, model, success, firstTokenMs)
}

func runtimeOpenAIRequestModel(request gatewayruntime.Request) string {
	if model := strings.TrimSpace(request.RequestedModel); model != "" {
		return model
	}
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(request.Payload, &payload) == nil {
		return strings.TrimSpace(payload.Model)
	}
	return ""
}

func runtimeOpenAIRequiredCapability(request gatewayruntime.Request, model string) service.OpenAIEndpointCapability {
	if request.Endpoint == gatewayruntime.EndpointResponses &&
		strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformOpenAI) &&
		service.IsExplicitImageGenerationIntent(request.InboundEndpoint, model, request.Payload) {
		return service.OpenAIEndpointCapabilityResponses
	}
	return service.OpenAIEndpointCapabilityChatCompletions
}

func writeOpenAIRuntimeError(exchange gatewayruntime.HTTPExchange, endpoint gatewayruntime.Endpoint, status int, errType, message string) error {
	if exchange == nil {
		return ErrSub2APIRuntimeExchangeUnavailable
	}
	if exchange.Written() {
		return nil
	}
	content := map[string]any{"error": map[string]string{"type": errType, "message": message}}
	if endpoint == gatewayruntime.EndpointMessages {
		content = map[string]any{"type": "error", "error": map[string]string{"type": errType, "message": message}}
	}
	body, err := json.Marshal(content)
	if err != nil {
		return err
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, err = exchange.Write(body)
	return err
}

type openAIExecutorTerminalSink struct {
	sink gatewayruntime.UsageSink
	seen bool
}

func (s *openAIExecutorTerminalSink) RecordFinal(ctx context.Context, event gatewayruntime.UsageEvent) error {
	if s == nil || s.sink == nil {
		return gatewayruntime.ErrUsageSinkUnavailable
	}
	if s.seen {
		return gatewayruntime.ErrTerminalAlreadyRecorded
	}
	s.seen = true
	return s.sink.RecordFinal(ctx, event)
}

func (s *openAIExecutorTerminalSink) recorded() bool {
	return s != nil && s.seen
}

var _ Sub2APIEndpointExecutor = (*sub2APIOpenAIExecutor)(nil)
