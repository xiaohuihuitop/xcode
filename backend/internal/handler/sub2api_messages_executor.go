package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type gatewayEndpointExecution func(
	context.Context,
	gatewayruntime.Request,
	gatewayruntime.UsageSink,
) (gatewayruntime.Result, error)

// sub2APIMessagesExecutor owns the Gateway implementation of Messages and its
// Chat/Responses compatibility protocols. OpenAI/Grok requests are delegated
// to the dedicated OpenAI runtime executor; other platforms remain on the
// Gateway migration path until their protocol family is selected for work.
type sub2APIMessagesExecutor struct {
	gatewayHandler *GatewayHandler
	openaiHandler  *OpenAIGatewayHandler
	endpoint       gatewayruntime.Endpoint
	executeGateway gatewayEndpointExecution
}

func (e sub2APIMessagesExecutor) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	route, _ := service.GatewayPlatformAssetContextFromContext(ctx)
	if route == nil || route.Platform == nil {
		if compatibilityRoute := runtimeCompatibilityRoute(request); compatibilityRoute != nil {
			ctx = service.WithGatewayPlatformAssetContext(ctx, compatibilityRoute)
			route = compatibilityRoute
		}
	}
	if route != nil && route.Platform != nil {
		switch route.Platform.AccountPlatform {
		case service.PlatformOpenAI, service.PlatformGrok:
			return (sub2APIOpenAIExecutor{
				handler:        e.openaiHandler,
				gatewayHandler: e.gatewayHandler,
				endpoint:       e.endpoint,
			}).Execute(ctx, request, sink)
		default:
			return e.executeGatewayRequest(ctx, request, sink)
		}
	}
	return gatewayruntime.Result{}, service.ErrAPIKeyPlatformForbidden
}

func (e sub2APIMessagesExecutor) executeGatewayRequest(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
	trackingSink := &messagesExecutorTerminalSink{sink: sink}
	run := e.executeGateway
	if run == nil {
		run = e.executeGatewayEndpoint
	}
	result, err := run(ctx, request, trackingSink)
	if event, ok := trackingSink.eventSnapshot(); ok {
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
		return result, err
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

func (e sub2APIMessagesExecutor) executeGatewayEndpoint(
	ctx context.Context,
	request gatewayruntime.Request,
	sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
	carrier, ok := request.Exchange.(ginContextCarrier)
	if !ok || carrier.GinContext() == nil || e.gatewayHandler == nil {
		return gatewayruntime.Result{}, ErrLegacyRuntimeExchangeUnavailable
	}
	c := carrier.GinContext()
	originalRequest := c.Request
	if originalRequest != nil {
		baseContext := ctx
		if baseContext == nil {
			baseContext = originalRequest.Context()
		}
		c.Request = originalRequest.WithContext(gatewayruntime.WithUsageSink(baseContext, sink))
		defer func() { c.Request = originalRequest }()
	}

	switch request.Endpoint {
	case gatewayruntime.EndpointMessages:
		e.executeMessages(c, sink)
	case gatewayruntime.EndpointChatCompletions:
		e.executeChatCompletions(c, sink)
	case gatewayruntime.EndpointResponses:
		e.executeResponses(c, sink)
	default:
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}

	status := c.Writer.Status()
	return gatewayruntime.Result{
		StatusCode: status,
		Response: gatewayruntime.Response{
			Streamed: request.Stream && c.Writer.Size() > 0,
		},
	}, nil
}

type messagesExecutorTerminalSink struct {
	mu       sync.Mutex
	sink     gatewayruntime.UsageSink
	recorded bool
	event    gatewayruntime.UsageEvent
}

func (s *messagesExecutorTerminalSink) RecordFinal(ctx context.Context, event gatewayruntime.UsageEvent) error {
	if s == nil || s.sink == nil {
		return gatewayruntime.ErrUsageSinkUnavailable
	}
	s.mu.Lock()
	if s.recorded {
		s.mu.Unlock()
		return gatewayruntime.ErrTerminalAlreadyRecorded
	}
	s.recorded = true
	s.event = event
	s.mu.Unlock()
	return s.sink.RecordFinal(ctx, event)
}

func (s *messagesExecutorTerminalSink) eventSnapshot() (gatewayruntime.UsageEvent, bool) {
	if s == nil {
		return gatewayruntime.UsageEvent{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.event, s.recorded
}

func recordGatewayExecutorUsage(
	ctx context.Context,
	sink gatewayruntime.UsageSink,
	svc *service.GatewayService,
	input *service.RecordUsageInput,
) error {
	if sink != nil {
		return sink.RecordFinal(ctx, gatewayUsageEvent(input))
	}
	return recordGatewayUsage(ctx, svc, input)
}

func runtimeCompatibilityRoute(request gatewayruntime.Request) *service.GatewayPlatformAssetContext {
	if request.PlatformID <= 0 {
		return nil
	}
	adapter := strings.ToLower(strings.TrimSpace(request.Adapter))
	if adapter == "" {
		return nil
	}
	requestedModel := strings.TrimSpace(request.RequestedModel)
	upstreamModel := strings.TrimSpace(request.UpstreamModel)
	return &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:           request.PlatformID,
			PlatformCode:         strings.TrimSpace(request.PlatformCode),
			AccountPlatform:      adapter,
			RequestedModel:       requestedModel,
			UpstreamModel:        upstreamModel,
			EndpointCapabilities: []string{runtimeEndpointCapability(request.Endpoint)},
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      request.PlatformID,
			PlatformCode:    strings.TrimSpace(request.PlatformCode),
			AccountPlatform: adapter,
		},
	}
}

func runtimeEndpointCapability(endpoint gatewayruntime.Endpoint) string {
	switch endpoint {
	case gatewayruntime.EndpointChatCompletions:
		return "chat_completions"
	case gatewayruntime.EndpointResponses:
		return "responses"
	case gatewayruntime.EndpointMessages:
		return "messages"
	default:
		return string(endpoint)
	}
}
