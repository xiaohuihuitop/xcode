package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// sub2APIGeminiMediaExecutor owns the native Gemini and media endpoint
// families. The executor only receives runtime contracts; protocol-specific
// service transports are kept behind the service runtime seam.
type sub2APIGeminiMediaExecutor struct {
	gatewayHandler *GatewayHandler
	openAIHandler  *OpenAIGatewayHandler
	endpoint       gatewayruntime.Endpoint
	maxSwitches    int
}

func (e sub2APIGeminiMediaExecutor) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if sink == nil {
		return gatewayruntime.Result{}, gatewayruntime.ErrUsageSinkUnavailable
	}
	trackingSink := &messagesExecutorTerminalSink{sink: sink}
	execution, err := newSub2APIExecution(ctx, request, trackingSink)
	if err != nil {
		return gatewayruntime.Result{}, err
	}
	ctx = execution.Context()
	request = execution.Request()

	var result gatewayruntime.Result
	switch request.Endpoint {
	case gatewayruntime.EndpointGeminiNative:
		result, err = e.executeGemini(ctx, request, trackingSink)
	case gatewayruntime.EndpointImages, gatewayruntime.EndpointVideos:
		result, err = e.executeMedia(ctx, request, trackingSink)
	default:
		err = ErrSub2APIRuntimeEndpointUnavailable
	}

	if event, ok := trackingSink.eventSnapshot(); ok {
		return mergeMediaTerminalResult(result, event), err
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
		event.Error = gatewayruntime.RuntimeErrorFromContext(err)
		if event.Error == nil {
			event.Error = gatewayruntime.RuntimeErrorFromStatus(status, http.StatusText(status))
		}
	}
	if recordErr := trackingSink.RecordFinal(ctx, event); recordErr != nil {
		if err != nil {
			return result, errors.Join(err, recordErr)
		}
		return result, recordErr
	}
	if result.StatusCode == 0 {
		result.StatusCode = status
	}
	return result, err
}

func (e sub2APIGeminiMediaExecutor) executeGemini(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if e.gatewayHandler == nil || e.gatewayHandler.gatewayService == nil || request.Exchange == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	if len(request.Payload) == 0 {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return gatewayruntime.Result{}, errors.New("gemini request body is empty")
	}
	model, action, err := runtimeGeminiModelAction(request)
	if err != nil {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return gatewayruntime.Result{}, err
	}
	sessionHash := runtimeGeminiSessionHash(request, e.gatewayHandler.gatewayService)
	excluded := make(map[int64]struct{})
	maxSwitches := e.maxSwitches
	if maxSwitches <= 0 {
		maxSwitches = e.gatewayHandler.maxAccountSwitchesGemini
	}
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	var lastErr error
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		selection, selectErr := e.gatewayHandler.gatewayService.SelectAccountWithLoadAwareness(
			ctx,
			service.PlatformSchedulingID(ctx),
			sessionHash,
			model,
			excluded,
			"",
			0,
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			if selectErr != nil {
				lastErr = selectErr
			}
			break
		}
		account := selection.Account
		sticky := false
		if sessionHash != "" {
			if cachedID, cacheErr := e.gatewayHandler.gatewayService.GetCachedSessionAccountID(ctx, service.PlatformSchedulingID(ctx), sessionHash); cacheErr == nil && cachedID > 0 {
				sticky = true
			}
		}
		result, forwardErr := e.forwardGemini(ctx, request, account, model, action, sessionHash, sticky)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if forwardErr == nil {
			facts := geminiForwardUsageFacts(request, account, result)
			if err := sink.RecordFinal(ctx, gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true, Facts: facts}); err != nil {
				return gatewayruntime.Result{}, err
			}
			return gatewayruntime.Result{StatusCode: runtimeExchangeStatus(request.Exchange), AccountID: account.ID, UpstreamModel: facts.UpstreamModel, Usage: facts}, nil
		}
		lastErr = forwardErr
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) || request.Exchange.Size() > 0 || !failoverErr.ShouldRetryNextAccount() {
			break
		}
		excluded[account.ID] = struct{}{}
	}
	if !request.Exchange.Written() {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, lastErr
}

func (e sub2APIGeminiMediaExecutor) forwardGemini(ctx context.Context, request gatewayruntime.Request, account *service.Account, model, action, sessionHash string, sticky bool) (*service.ForwardResult, error) {
	if account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey {
		if e.gatewayHandler.antigravityGatewayService == nil {
			return nil, errors.New("antigravity gateway service is unavailable")
		}
		platformID := int64(0)
		if scoped := service.PlatformSchedulingID(ctx); scoped != nil {
			platformID = *scoped
		}
		return e.gatewayHandler.antigravityGatewayService.ForwardGeminiRuntime(
			ctx,
			request.Exchange,
			account,
			model,
			action,
			request.Stream,
			request.Payload,
			sticky,
			platformID,
			sessionHash,
		)
	}
	if e.gatewayHandler.geminiCompatService == nil {
		return nil, errors.New("gemini compatibility service is unavailable")
	}
	return e.gatewayHandler.geminiCompatService.ForwardNativeRuntime(ctx, request.Exchange, account, model, action, request.Stream, request.Payload)
}

func (e sub2APIGeminiMediaExecutor) executeMedia(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	if e.openAIHandler == nil || e.openAIHandler.gatewayService == nil || request.Exchange == nil {
		return gatewayruntime.Result{}, ErrSub2APIRuntimeEndpointUnavailable
	}
	if strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformGrok) {
		return e.executeGrokMedia(ctx, request, sink)
	}
	if request.Endpoint != gatewayruntime.EndpointImages {
		return gatewayruntime.Result{}, fmt.Errorf("media endpoint %s is not supported for %s", request.Endpoint, request.Adapter)
	}
	parsed, err := e.openAIHandler.gatewayService.ParseOpenAIImagesRequestRuntime(ctx, request.Exchange, request.Payload)
	if err != nil || parsed == nil {
		if err == nil {
			err = errors.New("failed to parse images request")
		}
		_ = writeSyncJSONError(request.Exchange, http.StatusBadRequest, "invalid_request_error", err.Error())
		return gatewayruntime.Result{}, err
	}
	model := strings.TrimSpace(request.RequestedModel)
	if model == "" {
		model = parsed.Model
	}
	return e.executeOpenAIImage(ctx, request, sink, parsed, model)
}

func (e sub2APIGeminiMediaExecutor) executeOpenAIImage(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink, parsed *service.OpenAIImagesRequest, model string) (gatewayruntime.Result, error) {
	sessionHash := runtimeSessionHash(request)
	excluded := make(map[int64]struct{})
	maxSwitches := e.maxSwitches
	if maxSwitches <= 0 {
		maxSwitches = e.openAIHandler.maxAccountSwitches
	}
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	var lastErr error
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		selection, _, selectErr := e.openAIHandler.gatewayService.SelectAccountWithSchedulerForImages(ctx, service.PlatformSchedulingID(ctx), sessionHash, model, excluded, parsed.RequiredCapability)
		if selectErr != nil || selection == nil || selection.Account == nil {
			lastErr = selectErr
			break
		}
		account := selection.Account
		result, forwardErr := e.openAIHandler.gatewayService.ForwardImagesRuntime(ctx, request.Exchange, account, request.Payload, parsed, request.UpstreamModel)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if forwardErr == nil && result != nil {
			facts := openAIMediaUsageFacts(request, account, result)
			if err := sink.RecordFinal(ctx, gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true, Facts: facts}); err != nil {
				return gatewayruntime.Result{}, err
			}
			return gatewayruntime.Result{StatusCode: runtimeExchangeStatus(request.Exchange), AccountID: account.ID, UpstreamEndpoint: facts.UpstreamEndpoint, UpstreamModel: facts.UpstreamModel, Usage: facts}, nil
		}
		lastErr = forwardErr
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) || request.Exchange.Size() > 0 || !failoverErr.ShouldRetryNextAccount() {
			break
		}
		excluded[account.ID] = struct{}{}
		e.openAIHandler.gatewayService.RecordOpenAIAccountSwitch()
	}
	if !request.Exchange.Written() {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, lastErr
}

func (e sub2APIGeminiMediaExecutor) executeGrokMedia(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	endpoint := runtimeGrokMediaEndpoint(request)
	platformID := service.PlatformSchedulingID(ctx)
	contentType := runtimeRequestHeader(request, "Content-Type")
	requestInfo := service.ParseGrokMediaRequest(contentType, request.Payload)
	model := strings.TrimSpace(request.RequestedModel)
	if model == "" {
		model = requestInfo.Model
	}
	sessionHash := runtimeSessionHash(request)
	excluded := make(map[int64]struct{})
	maxSwitches := e.maxSwitches
	if maxSwitches <= 0 {
		maxSwitches = e.openAIHandler.maxAccountSwitches
	}
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	boundLookupAccountID := int64(0)
	if endpoint.IsVideoLookupRequest() {
		if request.Metadata.UserID <= 0 || request.Metadata.APIKeyID <= 0 || e.openAIHandler.gatewayService == nil {
			_ = writeSyncJSONError(request.Exchange, http.StatusNotFound, "not_found_error", "Video request not found")
			return gatewayruntime.Result{StatusCode: http.StatusNotFound}, errors.New("grok video request owner binding is unavailable")
		}
		var resolveErr error
		boundLookupAccountID, resolveErr = e.openAIHandler.gatewayService.ResolveGrokMediaVideoRequestAccount(
			ctx,
			platformID,
			runtimeGrokMediaRequestID(request),
			request.Metadata.UserID,
			request.Metadata.APIKeyID,
		)
		if resolveErr != nil || boundLookupAccountID <= 0 {
			_ = writeSyncJSONError(request.Exchange, http.StatusNotFound, "not_found_error", "Video request not found")
			if resolveErr == nil {
				resolveErr = errors.New("grok video request binding not found")
			}
			return gatewayruntime.Result{StatusCode: http.StatusNotFound}, resolveErr
		}
	}
	var lastErr error
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		selection, _, selectErr := e.openAIHandler.gatewayService.SelectAccountWithSchedulerForCapability(
			ctx,
			service.PlatformSchedulingID(ctx),
			"",
			sessionHash,
			model,
			excluded,
			service.OpenAIUpstreamTransportHTTPSSE,
			grokMediaRequiredCapability(endpoint),
			false,
			false,
			false,
			service.PlatformGrok,
		)
		if selectErr != nil || selection == nil || selection.Account == nil {
			lastErr = selectErr
			break
		}
		account := selection.Account
		if boundLookupAccountID > 0 && account.ID != boundLookupAccountID {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			_ = writeSyncJSONError(request.Exchange, http.StatusNotFound, "not_found_error", "Video request not found")
			return gatewayruntime.Result{StatusCode: http.StatusNotFound}, errors.New("grok video request account is unavailable")
		}
		result, forwardErr := e.openAIHandler.gatewayService.ForwardGrokMediaRuntime(
			ctx, request.Exchange, account, endpoint, runtimeGrokMediaRequestID(request), request.Payload, contentType,
		)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		if forwardErr == nil && result != nil {
			if endpoint.IsGenerationRequest() && strings.TrimSpace(result.ResponseID) != "" && request.Metadata.UserID > 0 && request.Metadata.APIKeyID > 0 {
				_ = e.openAIHandler.gatewayService.BindGrokMediaVideoRequestAccount(
					ctx,
					platformID,
					result.ResponseID,
					request.Metadata.UserID,
					request.Metadata.APIKeyID,
					account.ID,
				)
			}
			facts := openAIMediaUsageFacts(request, account, result)
			if err := sink.RecordFinal(ctx, gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true, Facts: facts}); err != nil {
				return gatewayruntime.Result{}, err
			}
			return gatewayruntime.Result{StatusCode: runtimeExchangeStatus(request.Exchange), AccountID: account.ID, UpstreamEndpoint: facts.UpstreamEndpoint, UpstreamModel: facts.UpstreamModel, Usage: facts}, nil
		}
		lastErr = forwardErr
		var failoverErr *service.UpstreamFailoverError
		if !errors.As(forwardErr, &failoverErr) || request.Exchange.Size() > 0 || !failoverErr.ShouldRetryNextAccount() || endpoint.IsVideoLookupRequest() {
			break
		}
		excluded[account.ID] = struct{}{}
		e.openAIHandler.gatewayService.RecordOpenAIAccountSwitch()
	}
	if !request.Exchange.Written() {
		_ = writeSyncJSONError(request.Exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
	}
	if lastErr == nil {
		lastErr = service.ErrNoAvailableAccounts
	}
	return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, lastErr
}

func runtimeGeminiModelAction(request gatewayruntime.Request) (string, string, error) {
	path := request.InboundEndpoint
	if request.Exchange != nil && request.Exchange.Request() != nil && request.Exchange.Request().URL != nil {
		path = request.Exchange.Request().URL.Path
	}
	const prefix = "/v1beta/models/"
	value := strings.TrimPrefix(path, prefix)
	if value == path {
		value = strings.TrimSpace(request.RequestedModel)
		if value != "" {
			return value, "generateContent", nil
		}
	}
	model, action, err := parseGeminiModelAction(strings.TrimPrefix(value, "/"))
	if err != nil {
		return "", "", err
	}
	if model == "" {
		model = strings.TrimSpace(request.RequestedModel)
	}
	return model, action, nil
}

func runtimeGrokMediaEndpoint(request gatewayruntime.Request) service.GrokMediaEndpoint {
	path := strings.ToLower(request.InboundEndpoint)
	switch {
	case strings.Contains(path, "/videos/edits"):
		return service.GrokMediaEndpointVideosEdits
	case strings.Contains(path, "/videos/extensions"):
		return service.GrokMediaEndpointVideosExtensions
	case strings.Contains(path, "/videos/generations"):
		return service.GrokMediaEndpointVideosGenerations
	case strings.HasSuffix(path, "/content"):
		return service.GrokMediaEndpointVideoContent
	case strings.Contains(path, "/videos/"):
		return service.GrokMediaEndpointVideoStatus
	case strings.Contains(path, "/images/edits"):
		return service.GrokMediaEndpointImagesEdits
	default:
		return service.GrokMediaEndpointImagesGenerations
	}
}

func runtimeGrokMediaRequestID(request gatewayruntime.Request) string {
	path := request.InboundEndpoint
	if request.Exchange != nil && request.Exchange.Request() != nil && request.Exchange.Request().URL != nil {
		path = request.Exchange.Request().URL.Path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if !strings.EqualFold(part, "videos") || i+1 >= len(parts) {
			continue
		}
		candidate := strings.TrimSpace(parts[i+1])
		switch strings.ToLower(candidate) {
		case "", "generations", "edits", "extensions":
			return ""
		default:
			return candidate
		}
	}
	return ""
}

func runtimeSessionHash(request gatewayruntime.Request) string {
	if session := strings.TrimSpace(request.Metadata.SessionID); session != "" {
		return session
	}
	// Images and Grok media historically use GenerateExplicitSessionHash: when
	// the client sends no explicit session signal, scheduling must remain
	// stateless rather than inventing a per-request sticky key.
	return ""
}

func runtimeGeminiSessionHash(request gatewayruntime.Request, gatewayService *service.GatewayService) string {
	if match := geminiCLITmpDirRegex.FindSubmatch(request.Payload); len(match) >= 2 {
		tmpDirHash := string(match[1])
		privilegedUserID := runtimeRequestHeader(request, "x-gemini-api-privileged-user-id")
		if privilegedUserID != "" {
			digest := sha256.Sum256([]byte(privilegedUserID + ":" + tmpDirHash))
			return "gemini:" + hex.EncodeToString(digest[:])
		}
		return "gemini:" + tmpDirHash
	}
	if gatewayService != nil {
		parsed, parseErr := service.ParseGatewayRequest(service.NewRequestBodyRef(request.Payload), service.PlatformGemini)
		if parseErr == nil && parsed != nil {
			parsed.SessionContext = &service.SessionContext{
				ClientIP:  request.Metadata.ClientIP,
				UserAgent: request.Metadata.UserAgent,
				APIKeyID:  request.Metadata.APIKeyID,
			}
			if hash := gatewayService.GenerateSessionHash(parsed); strings.TrimSpace(hash) != "" {
				return "gemini:" + hash
			}
		}
	}
	return ""
}

func runtimeRequestHeader(request gatewayruntime.Request, key string) string {
	for name, value := range request.Metadata.Headers {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return strings.TrimSpace(value)
		}
	}
	if request.Exchange != nil && request.Exchange.Request() != nil {
		return strings.TrimSpace(request.Exchange.Request().Header.Get(key))
	}
	return ""
}

func geminiForwardUsageFacts(request gatewayruntime.Request, account *service.Account, result *service.ForwardResult) gatewayruntime.UsageFacts {
	facts := gatewayruntime.UsageFacts{
		Adapter:                request.Adapter,
		Model:                  request.RequestedModel,
		RequestedModel:         request.RequestedModel,
		UpstreamModel:          request.UpstreamModel,
		AccountID:              account.ID,
		InboundEndpoint:        request.InboundEndpoint,
		UserAgent:              request.Metadata.UserAgent,
		ClientIP:               request.Metadata.ClientIP,
		SessionID:              request.Metadata.SessionID,
		RequestWasClientStream: request.Stream,
	}
	if result == nil {
		return facts
	}
	facts.Model = firstNonEmptyString(result.Model, facts.Model)
	facts.UpstreamModel = firstNonEmptyString(result.UpstreamModel, facts.UpstreamModel)
	facts.InputTokens = result.Usage.InputTokens
	facts.OutputTokens = result.Usage.OutputTokens
	facts.CacheCreationTokens = result.Usage.CacheCreationInputTokens
	facts.CacheReadTokens = result.Usage.CacheReadInputTokens
	facts.DurationMilliseconds = result.Duration.Milliseconds()
	return facts
}

func openAIMediaUsageFacts(request gatewayruntime.Request, account *service.Account, result *service.OpenAIForwardResult) gatewayruntime.UsageFacts {
	facts := openAIForwardUsageFacts(request, account, result)
	if strings.TrimSpace(facts.UpstreamEndpoint) == "" {
		facts.UpstreamEndpoint = runtimeMediaUpstreamEndpoint(request)
	}
	if result == nil {
		return facts
	}
	facts.ImageCount = result.ImageCount
	facts.VideoCount = result.VideoCount
	return facts
}

func runtimeMediaUpstreamEndpoint(request gatewayruntime.Request) string {
	path := request.InboundEndpoint
	if request.Exchange != nil && request.Exchange.Request() != nil && request.Exchange.Request().URL != nil {
		path = request.Exchange.Request().URL.Path
	}
	path = strings.ToLower(strings.TrimSpace(path))
	if strings.Contains(path, "/images/edits") {
		return "/v1/images/edits"
	}
	if strings.Contains(path, "/images/") {
		return "/v1/images/generations"
	}
	if strings.Contains(path, "/videos/") {
		return strings.TrimSpace(request.InboundEndpoint)
	}
	return ""
}

func mergeMediaTerminalResult(result gatewayruntime.Result, event gatewayruntime.UsageEvent) gatewayruntime.Result {
	if result.StatusCode == 0 {
		result.StatusCode = http.StatusOK
	}
	if result.AccountID == 0 {
		result.AccountID = event.Facts.AccountID
	}
	if result.UpstreamModel == "" {
		result.UpstreamModel = event.Facts.UpstreamModel
	}
	if result.Usage.AccountID == 0 {
		result.Usage = event.Facts
	}
	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

var _ Sub2APIEndpointExecutor = (*sub2APIGeminiMediaExecutor)(nil)
