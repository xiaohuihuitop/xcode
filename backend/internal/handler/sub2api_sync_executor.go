package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type syncEndpointExecution func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error)

type syncAccountSelector func(context.Context, gatewayruntime.Request, map[int64]struct{}, service.OpenAIEndpointCapability) (*service.AccountSelectionResult, error)
type syncAccountForwarder func(context.Context, *service.Account) (*service.OpenAIForwardResult, error)

// sub2APISyncExecutor owns the transport-neutral synchronous endpoints. The
// endpoint-specific callbacks are injectable so each protocol can be migrated
// and conformance-tested without routing through a Gin handler.
type sub2APISyncExecutor struct {
	gatewayHandler *GatewayHandler
	openAIHandler  *OpenAIGatewayHandler
	endpoint       gatewayruntime.Endpoint

	executeCountTokens syncEndpointExecution
	executeEmbeddings  syncEndpointExecution
	executeAlphaSearch syncEndpointExecution
	selectAccount      syncAccountSelector
	forwardEmbeddings  syncAccountForwarder
	forwardAlphaSearch syncAccountForwarder
	reportSchedule     func(*service.Account, string, *service.OpenAIForwardResult, bool)
	recordSwitch       func()
	maxSwitches        int
}

func (e sub2APISyncExecutor) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if sink == nil {
		return gatewayruntime.Result{}, gatewayruntime.ErrUsageSinkUnavailable
	}
	trackingSink := &messagesExecutorTerminalSink{sink: sink}
	ctx = gatewayruntime.WithUsageSink(ctx, trackingSink)
	var (
		result gatewayruntime.Result
		err    error
	)

	switch request.Endpoint {
	case gatewayruntime.EndpointCountTokens:
		if e.executeCountTokens != nil {
			result, err = e.executeCountTokens(ctx, request, trackingSink)
		} else if strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformGrok) {
			result, err = e.executeGrokCountTokens(ctx, request, trackingSink)
		} else {
			result, err = e.executeCountTokensRuntime(ctx, request, trackingSink)
		}
	case gatewayruntime.EndpointEmbeddings:
		if e.executeEmbeddings != nil {
			result, err = e.executeEmbeddings(ctx, request, trackingSink)
		} else {
			result, err = e.executeEmbeddingsRuntime(ctx, request, trackingSink)
		}
	case gatewayruntime.EndpointAlphaSearch:
		if e.executeAlphaSearch != nil {
			result, err = e.executeAlphaSearch(ctx, request, trackingSink)
		} else {
			result, err = e.executeAlphaSearchRuntime(ctx, request, trackingSink)
		}
	default:
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}

	if event, ok := trackingSink.eventSnapshot(); ok {
		return mergeSyncTerminalResult(result, event), err
	}

	status := result.StatusCode
	if status == 0 {
		status = http.StatusBadGateway
		if errors.Is(err, context.Canceled) {
			status = statusClientClosedRequest
		}
	}
	event := gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   err == nil && status >= http.StatusOK && status < http.StatusBadRequest,
		Facts: gatewayruntime.UsageFacts{
			Adapter:                  request.Adapter,
			Model:                    request.RequestedModel,
			RequestedModel:           request.RequestedModel,
			UpstreamModel:            request.UpstreamModel,
			InboundEndpoint:          request.InboundEndpoint,
			RequestWasClientStream:   request.Stream,
			ResponseWasPartiallySent: request.Exchange != nil && request.Exchange.Size() > 0,
		},
	}
	if !event.Success {
		if err != nil {
			event.Error = gatewayruntime.RuntimeErrorFromContext(err)
		}
		if event.Error == nil || event.Error.Category == gatewayruntime.ErrorInvalidUpstreamResponse {
			event.Error = gatewayruntime.RuntimeErrorFromStatus(status, http.StatusText(status))
		}
	}
	if recordErr := trackingSink.RecordFinal(ctx, event); recordErr != nil {
		if err != nil {
			return result, errors.Join(err, recordErr)
		}
		return result, recordErr
	}
	return result, err
}

func (e sub2APISyncExecutor) executeCountTokensRuntime(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if request.Exchange == nil || len(request.Payload) == 0 {
		if request.Exchange != nil {
			_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		}
		return gatewayruntime.Result{}, errors.New("count_tokens request body is empty")
	}
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(request.Payload), domain.PlatformAnthropic)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Model) == "" {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		if err == nil {
			err = errors.New("count_tokens model is required")
		}
		return gatewayruntime.Result{}, err
	}
	if strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformOpenAI) || strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformGrok) {
		if e.openAIHandler == nil || e.openAIHandler.gatewayService == nil {
			return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
		}
		return e.executeOpenAICountTokensRuntime(ctx, request, parsed, sink)
	}
	if e.gatewayHandler == nil || e.gatewayHandler.gatewayService == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	selection, err := e.gatewayHandler.gatewayService.SelectAccountForModelWithExclusions(
		ctx,
		service.PlatformSchedulingID(ctx),
		request.Metadata.SessionID,
		parsed.Model,
		nil,
	)
	if err != nil || selection == nil {
		if err == nil {
			err = service.ErrNoAvailableAccounts
		}
		_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "api_error", "No available accounts")
		return gatewayruntime.Result{}, err
	}
	if err := e.gatewayHandler.gatewayService.ForwardCountTokensRuntime(ctx, request.Exchange, selection, parsed); err != nil {
		return gatewayruntime.Result{StatusCode: runtimeExchangeStatus(request.Exchange)}, err
	}
	if status := runtimeExchangeStatus(request.Exchange); status < http.StatusOK || status >= http.StatusMultipleChoices {
		return gatewayruntime.Result{StatusCode: status}, fmt.Errorf("count_tokens upstream returned status %d", status)
	}
	event := gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			Adapter:         request.Adapter,
			Model:           parsed.Model,
			RequestedModel:  request.RequestedModel,
			UpstreamModel:   request.UpstreamModel,
			AccountID:       selection.ID,
			InboundEndpoint: request.InboundEndpoint,
			TerminalStatus:  http.StatusText(http.StatusOK),
		},
	}
	if err := sink.RecordFinal(ctx, event); err != nil {
		return gatewayruntime.Result{}, err
	}
	return gatewayruntime.Result{StatusCode: http.StatusOK, AccountID: selection.ID}, nil
}

func (e sub2APISyncExecutor) executeOpenAICountTokensRuntime(ctx context.Context, request gatewayruntime.Request, parsed *service.ParsedRequest, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	svc := e.openAIHandler.gatewayService
	excluded := make(map[int64]struct{})
	for switchCount := 0; switchCount <= e.syncMaxSwitches(); switchCount++ {
		selection, _, err := svc.SelectAccountWithSchedulerForCapability(
			ctx,
			service.PlatformSchedulingID(ctx),
			"",
			request.Metadata.SessionID,
			parsed.Model,
			excluded,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			false,
			request.Adapter,
		)
		if err != nil || selection == nil || selection.Account == nil {
			if err == nil {
				err = service.ErrNoAvailableAccounts
			}
			_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "api_error", "No available accounts")
			return gatewayruntime.Result{}, err
		}
		account := selection.Account
		release := selection.ReleaseFunc
		err = svc.ForwardCountTokensAsAnthropicRuntime(ctx, request.Exchange, account, parsed.Body.Bytes(), request.UpstreamModel, request.Metadata.APIKeyID)
		if release != nil {
			release()
		}
		status := runtimeExchangeStatus(request.Exchange)
		if err == nil && status >= http.StatusOK && status < http.StatusMultipleChoices {
			event := gatewayruntime.UsageEvent{
				RequestID: request.RequestID,
				Success:   true,
				Facts: gatewayruntime.UsageFacts{
					Adapter:         request.Adapter,
					Model:           parsed.Model,
					RequestedModel:  request.RequestedModel,
					UpstreamModel:   request.UpstreamModel,
					AccountID:       account.ID,
					InboundEndpoint: request.InboundEndpoint,
					TerminalStatus:  http.StatusText(status),
				},
			}
			if recordErr := sink.RecordFinal(ctx, event); recordErr != nil {
				return gatewayruntime.Result{}, recordErr
			}
			return gatewayruntime.Result{StatusCode: status, AccountID: account.ID}, nil
		}
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(err, &failoverErr) || request.Exchange.Size() > 0 || !failoverErr.ShouldRetryNextAccount() {
			if err == nil {
				err = fmt.Errorf("count_tokens upstream returned status %d", status)
			}
			return gatewayruntime.Result{StatusCode: status}, err
		}
		excluded[account.ID] = struct{}{}
		svc.RecordOpenAIAccountSwitch()
	}
	return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, service.ErrNoAvailableAccounts
}

func (e sub2APISyncExecutor) syncMaxSwitches() int {
	if e.maxSwitches > 0 {
		return e.maxSwitches
	}
	if e.openAIHandler != nil && e.openAIHandler.maxAccountSwitches > 0 {
		return e.openAIHandler.maxAccountSwitches
	}
	return 3
}

func runtimeExchangeStatus(exchange gatewayruntime.HTTPExchange) int {
	if exchange == nil {
		return 0
	}
	if value, ok := exchange.State(gatewayruntime.HTTPExchangeStatusStateKey); ok {
		if status, ok := value.(int); ok {
			return status
		}
	}
	if exchange.Written() {
		return http.StatusOK
	}
	return 0
}

func (e sub2APISyncExecutor) executeEmbeddingsRuntime(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if (e.openAIHandler == nil || e.openAIHandler.gatewayService == nil) && e.selectAccount == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	forward := e.forwardEmbeddings
	if forward == nil {
		forward = func(ctx context.Context, account *service.Account) (*service.OpenAIForwardResult, error) {
			return e.openAIHandler.gatewayService.ForwardEmbeddingsRuntime(ctx, request.Exchange, account, request.Payload, request.UpstreamModel)
		}
	}
	return e.executeOpenAISyncWithFailover(ctx, request, sink,
		service.OpenAIEndpointCapabilityEmbeddings,
		true,
		forward,
	)
}

func (e sub2APISyncExecutor) executeAlphaSearchRuntime(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if (e.openAIHandler == nil || e.openAIHandler.gatewayService == nil) && e.selectAccount == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	forward := e.forwardAlphaSearch
	if forward == nil {
		forward = func(ctx context.Context, account *service.Account) (*service.OpenAIForwardResult, error) {
			return e.openAIHandler.gatewayService.ForwardAlphaSearchRuntime(ctx, request.Exchange, account, request.Payload, request.Metadata.APIKeyID)
		}
	}
	return e.executeOpenAISyncWithFailover(ctx, request, sink,
		service.OpenAIEndpointCapabilityAlphaSearch,
		false,
		forward,
	)
}

func (e sub2APISyncExecutor) executeOpenAISyncWithFailover(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
	capability service.OpenAIEndpointCapability,
	useUpstreamTokenCost bool,
	forward func(context.Context, *service.Account) (*service.OpenAIForwardResult, error),
) (gatewayruntime.Result, error) {
	if request.Exchange == nil || forward == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	maxSwitches := 3
	if e.openAIHandler != nil && e.openAIHandler.maxAccountSwitches > 0 {
		maxSwitches = e.openAIHandler.maxAccountSwitches
	}
	sessionHash := strings.TrimSpace(request.Metadata.SessionID)
	if sessionHash == "" {
		sessionHash = strings.TrimSpace(request.RequestID)
	}
	excluded := make(map[int64]struct{})
	var lastErr error
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		var selection *service.AccountSelectionResult
		var err error
		if e.selectAccount != nil {
			selection, err = e.selectAccount(ctx, request, excluded, capability)
		} else {
			selection, _, err = e.openAIHandler.gatewayService.SelectAccountWithSchedulerForCapability(
				ctx,
				service.PlatformSchedulingID(ctx),
				"",
				sessionHash,
				request.RequestedModel,
				excluded,
				service.OpenAIUpstreamTransportHTTPSSE,
				capability,
				false,
				false,
				useUpstreamTokenCost,
				service.PlatformOpenAI,
			)
		}
		if err != nil || selection == nil || selection.Account == nil {
			if err != nil {
				lastErr = err
			}
			break
		}
		account := selection.Account
		release := selection.ReleaseFunc
		result, forwardErr := forward(ctx, account)
		if release != nil {
			release()
		}
		if forwardErr == nil {
			e.updateOpenAIScheduleResult(account, request.RequestedModel, result, true)
			if result == nil {
				return gatewayruntime.Result{}, errors.New("runtime forward returned no result")
			}
			event := gatewayruntime.UsageEvent{
				RequestID: request.RequestID,
				Success:   true,
				Facts:     openAIForwardUsageFacts(request, account, result),
			}
			if err := sink.RecordFinal(ctx, event); err != nil {
				return gatewayruntime.Result{}, err
			}
			return gatewayruntime.Result{
				StatusCode:       http.StatusOK,
				AccountID:        account.ID,
				UpstreamEndpoint: result.UpstreamEndpoint,
				UpstreamModel:    result.UpstreamModel,
				Usage:            event.Facts,
			}, nil
		}
		lastErr = forwardErr
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) || request.Exchange.Size() > 0 || !failoverErr.ShouldRetryNextAccount() {
			e.updateOpenAIScheduleResult(account, request.RequestedModel, result, false)
			break
		}
		e.updateOpenAIScheduleResult(account, request.RequestedModel, result, false)
		excluded[account.ID] = struct{}{}
		if e.recordSwitch != nil {
			e.recordSwitch()
		} else if e.openAIHandler != nil && e.openAIHandler.gatewayService != nil {
			e.openAIHandler.gatewayService.RecordOpenAIAccountSwitch()
		}
	}
	if !request.Exchange.Written() {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, lastErr
}

func (e sub2APISyncExecutor) updateOpenAIScheduleResult(account *service.Account, requestedModel string, result *service.OpenAIForwardResult, success bool) {
	if account == nil {
		return
	}
	if e.reportSchedule != nil {
		e.reportSchedule(account, requestedModel, result, success)
		return
	}
	if e.openAIHandler == nil || e.openAIHandler.gatewayService == nil {
		return
	}
	model := requestedModel
	if account.GetMappedModel(requestedModel) != "" {
		model = account.GetMappedModel(requestedModel)
	}
	var firstTokenMs *int
	if result != nil {
		firstTokenMs = result.FirstTokenMs
	}
	e.openAIHandler.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, model, success, firstTokenMs)
}

func openAIForwardUsageFacts(request gatewayruntime.Request, account *service.Account, result *service.OpenAIForwardResult) gatewayruntime.UsageFacts {
	model := request.RequestedModel
	upstreamModel := request.UpstreamModel
	endpoint := ""
	if result != nil {
		if strings.TrimSpace(result.Model) != "" {
			model = result.Model
		}
		if strings.TrimSpace(result.UpstreamModel) != "" {
			upstreamModel = result.UpstreamModel
		}
		endpoint = strings.TrimSpace(result.UpstreamEndpoint)
	}
	if endpoint == "" {
		endpoint = defaultSyncUpstreamEndpoint(request.Endpoint)
	}
	var usage service.OpenAIUsage
	var durationMilliseconds int64
	var firstTokenMilliseconds int64
	if result != nil {
		usage = result.Usage
		durationMilliseconds = result.Duration.Milliseconds()
		firstTokenMilliseconds = int64Value(result.FirstTokenMs)
	}
	facts := gatewayruntime.UsageFacts{
		Adapter:                request.Adapter,
		Model:                  model,
		RequestedModel:         request.RequestedModel,
		InputTokens:            usage.InputTokens,
		OutputTokens:           usage.OutputTokens,
		CacheCreationTokens:    usage.CacheCreationInputTokens,
		CacheReadTokens:        usage.CacheReadInputTokens,
		ImageInputTokens:       usage.ImageInputTokens,
		ImageOutputTokens:      usage.ImageOutputTokens,
		AccountID:              account.ID,
		UpstreamEndpoint:       endpoint,
		UpstreamModel:          upstreamModel,
		DurationMilliseconds:   durationMilliseconds,
		FirstTokenMilliseconds: firstTokenMilliseconds,
		InboundEndpoint:        request.InboundEndpoint,
		UserAgent:              request.Metadata.UserAgent,
		ClientIP:               request.Metadata.ClientIP,
		SessionID:              request.Metadata.SessionID,
		RequestWasClientStream: request.Stream,
	}
	if result != nil {
		facts.BillingModel = strings.TrimSpace(result.BillingModel)
		facts.OriginalModel = strings.TrimSpace(result.Model)
		facts.MappedModel = strings.TrimSpace(result.UpstreamModel)
		if result.ServiceTier != nil {
			facts.ServiceTier = strings.TrimSpace(*result.ServiceTier)
		}
		if result.ReasoningEffort != nil {
			facts.ReasoningEffort = strings.TrimSpace(*result.ReasoningEffort)
		}
	}
	return facts
}

func defaultSyncUpstreamEndpoint(endpoint gatewayruntime.Endpoint) string {
	switch endpoint {
	case gatewayruntime.EndpointEmbeddings:
		return "/v1/embeddings"
	case gatewayruntime.EndpointAlphaSearch:
		return "/v1/alpha/search"
	default:
		return ""
	}
}

func int64Value(value *int) int64 {
	if value == nil || *value < 0 {
		return 0
	}
	return int64(*value)
}

func (e sub2APISyncExecutor) executeGrokCountTokens(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if request.Exchange == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeExchangeUnavailable
	}
	if len(request.Payload) == 0 {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return gatewayruntime.Result{}, errors.New("count_tokens request body is empty")
	}
	estimated, err := service.EstimateGrokCountTokens(request.Payload)
	if err != nil {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return gatewayruntime.Result{}, err
	}
	if err := writeSyncJSON(request.Exchange, http.StatusOK, map[string]int{"input_tokens": estimated}); err != nil {
		return gatewayruntime.Result{}, err
	}
	event := gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   true,
		Facts: gatewayruntime.UsageFacts{
			Adapter:         request.Adapter,
			Model:           request.RequestedModel,
			RequestedModel:  request.RequestedModel,
			UpstreamModel:   request.UpstreamModel,
			InboundEndpoint: request.InboundEndpoint,
			TerminalStatus:  http.StatusText(http.StatusOK),
		},
	}
	if err := sink.RecordFinal(ctx, event); err != nil {
		return gatewayruntime.Result{}, err
	}
	return gatewayruntime.Result{StatusCode: http.StatusOK, UpstreamModel: request.UpstreamModel}, nil
}

func mergeSyncTerminalResult(result gatewayruntime.Result, event gatewayruntime.UsageEvent) gatewayruntime.Result {
	if result.AccountID == 0 {
		result.AccountID = event.Facts.AccountID
	}
	if result.UpstreamEndpoint == "" {
		result.UpstreamEndpoint = event.Facts.UpstreamEndpoint
	}
	if result.UpstreamModel == "" {
		result.UpstreamModel = event.Facts.UpstreamModel
	}
	if result.Usage.AccountID == 0 {
		result.Usage = event.Facts
	}
	return result
}

func writeSyncJSON(exchange gatewayruntime.HTTPExchange, status int, value any) error {
	if exchange == nil {
		return ErrSub2APIRuntimeExchangeUnavailable
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal runtime response: %w", err)
	}
	if !exchange.Written() {
		exchange.Header().Set("Content-Type", "application/json")
		exchange.WriteHeader(status)
	}
	_, err = exchange.Write(body)
	return err
}

func writeSyncJSONError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) error {
	return writeSyncJSON(exchange, status, map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
}

var _ Sub2APIEndpointExecutor = (*sub2APISyncExecutor)(nil)
