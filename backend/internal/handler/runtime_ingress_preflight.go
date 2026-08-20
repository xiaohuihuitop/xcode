package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type runtimeProductPreflightSpec struct {
	Protocol  string
	Model     string
	Audit     bool
	AuditBody []byte
	UserSlot  bool
	Billing   bool
	ImageSlot bool
	Stream    bool
}

func buildRuntimeProductPreflightSpec(request gatewayruntime.Request, images *service.OpenAIGatewayService) (runtimeProductPreflightSpec, error) {
	spec := runtimeProductPreflightSpec{
		Model:  strings.TrimSpace(request.RequestedModel),
		Stream: request.Stream,
	}
	switch request.Endpoint {
	case gatewayruntime.EndpointChatCompletions, gatewayruntime.EndpointResponses, gatewayruntime.EndpointMessages:
		model, err := validateRuntimeJSONModel(request.Payload)
		if err != nil {
			return runtimeProductPreflightSpec{}, err
		}
		if model != "" {
			spec.Model = model
		}
		if stream, ok := parseOpenAICompatibleStream(request.Payload); ok {
			spec.Stream = stream
		}
		spec.UserSlot, spec.Billing, spec.Audit = true, true, true
		spec.AuditBody = append([]byte(nil), request.Payload...)
		switch request.Endpoint {
		case gatewayruntime.EndpointChatCompletions:
			spec.Protocol = service.ContentModerationProtocolOpenAIChat
			if service.IsGPTImageGenerationModel(spec.Model) {
				return runtimeProductPreflightSpec{}, fmt.Errorf("This model is not supported on the Chat Completions endpoint")
			}
		case gatewayruntime.EndpointResponses:
			spec.Protocol = service.ContentModerationProtocolOpenAIResponses
		case gatewayruntime.EndpointMessages:
			spec.Protocol = service.ContentModerationProtocolAnthropicMessages
		}
		return spec, nil
	case gatewayruntime.EndpointCountTokens:
		spec.Billing = !strings.EqualFold(strings.TrimSpace(request.Adapter), service.PlatformGrok)
		if spec.Billing {
			if model, err := validateRuntimeJSONModel(request.Payload); err != nil {
				return runtimeProductPreflightSpec{}, err
			} else if model != "" {
				spec.Model = model
			}
		}
		return spec, nil
	case gatewayruntime.EndpointEmbeddings:
		spec.Protocol, spec.Audit, spec.UserSlot, spec.Billing = "openai_embeddings", true, true, true
		model, err := validateRuntimeJSONModel(request.Payload)
		if err != nil {
			return runtimeProductPreflightSpec{}, err
		}
		if model != "" {
			spec.Model = model
		}
		spec.AuditBody = append([]byte(nil), request.Payload...)
		return spec, nil
	case gatewayruntime.EndpointAlphaSearch:
		spec.Protocol, spec.Audit, spec.UserSlot, spec.Billing = "openai_alpha_search", true, true, true
		model, err := validateRuntimeJSONModel(request.Payload)
		if err != nil {
			return runtimeProductPreflightSpec{}, err
		}
		if model != "" {
			spec.Model = model
		}
		spec.AuditBody = append([]byte(nil), request.Payload...)
		return spec, nil
	case gatewayruntime.EndpointGeminiNative:
		spec.Protocol, spec.Audit, spec.UserSlot, spec.Billing = service.ContentModerationProtocolGemini, true, true, true
		if len(request.Payload) == 0 {
			return runtimeProductPreflightSpec{}, fmt.Errorf("Request body is empty")
		}
		if upstream := strings.TrimSpace(request.UpstreamModel); upstream != "" {
			spec.Model = upstream
		}
		spec.AuditBody = append([]byte(nil), request.Payload...)
		return spec, nil
	case gatewayruntime.EndpointImages:
		return buildRuntimeMediaPreflightSpec(request, images)
	case gatewayruntime.EndpointVideos:
		return buildRuntimeMediaPreflightSpec(request, images)
	default:
		return spec, nil
	}
}

func buildRuntimeMediaPreflightSpec(request gatewayruntime.Request, images *service.OpenAIGatewayService) (runtimeProductPreflightSpec, error) {
	spec := runtimeProductPreflightSpec{
		Model:    strings.TrimSpace(request.RequestedModel),
		Stream:   request.Stream,
		UserSlot: true,
		Billing:  true,
	}
	adapter := strings.ToLower(strings.TrimSpace(request.Adapter))
	if adapter == service.PlatformOpenAI && request.Endpoint == gatewayruntime.EndpointImages {
		if images == nil {
			return runtimeProductPreflightSpec{}, fmt.Errorf("openai images service is unavailable")
		}
		parsed, err := images.ParseOpenAIImagesRequestFromMetadata(
			request.InboundEndpoint,
			runtimeRequestHeader(request, "Content-Type"),
			request.Payload,
		)
		if err != nil {
			return runtimeProductPreflightSpec{}, err
		}
		if parsed != nil {
			if model := strings.TrimSpace(parsed.Model); model != "" {
				spec.Model = model
			}
			spec.Stream = parsed.Stream
			spec.AuditBody = parsed.ModerationBody()
		}
		spec.Protocol, spec.Audit, spec.ImageSlot = service.ContentModerationProtocolOpenAIImages, true, true
		return spec, nil
	}

	if adapter != service.PlatformGrok {
		return spec, nil
	}
	endpoint := runtimeGrokMediaEndpoint(request)
	if endpoint.IsVideoLookupRequest() {
		if runtimeGrokMediaRequestID(request) == "" {
			return runtimeProductPreflightSpec{}, fmt.Errorf("request_id is required")
		}
		return spec, nil
	}
	info := service.ParseGrokMediaRequest(runtimeRequestHeader(request, "Content-Type"), request.Payload)
	if endpoint.RequiresRequestBody() && len(request.Payload) == 0 {
		return runtimeProductPreflightSpec{}, fmt.Errorf("Request body is empty")
	}
	if endpoint.IsGenerationRequest() && strings.TrimSpace(info.Model) == "" {
		return runtimeProductPreflightSpec{}, fmt.Errorf("model is required")
	}
	if model := strings.TrimSpace(info.Model); model != "" {
		spec.Model = model
	}
	spec.Protocol = service.ContentModerationProtocolOpenAIImages
	spec.AuditBody = info.ModerationBody()
	spec.Audit = len(spec.AuditBody) > 0
	spec.ImageSlot = endpoint.IsGenerationRequest()
	return spec, nil
}

func validateRuntimeJSONModel(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("Request body is empty")
	}
	if !gjson.ValidBytes(body) {
		return "", fmt.Errorf("Failed to parse request body")
	}
	model := gjson.GetBytes(body, "model")
	if !model.Exists() || model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return "", fmt.Errorf("model is required")
	}
	return strings.TrimSpace(model.String()), nil
}

func runtimeProductPreflightSpecForRequest(request applicationgateway.DispatchRequest, images *service.OpenAIGatewayService) (runtimeProductPreflightSpec, error) {
	return buildRuntimeProductPreflightSpec(request.Runtime, images)
}

func (h *OpenAIGatewayHandler) dispatchRuntimeEndpoint(c *gin.Context, endpoint gatewayruntime.Endpoint) {
	_ = dispatchRuntimeEndpoint(c, endpoint, h.applicationGateway, h.runtimeProductPreflight)
}

func (h *GatewayHandler) dispatchRuntimeEndpoint(c *gin.Context, endpoint gatewayruntime.Endpoint) {
	_ = dispatchRuntimeEndpoint(c, endpoint, h.applicationGateway, h.runtimeProductPreflight)
}

func (h *OpenAIGatewayHandler) runtimeProductPreflight(c *gin.Context, request applicationgateway.DispatchRequest) (func(), bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	spec, err := runtimeProductPreflightSpecForRequest(request, h.gatewayService)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, false
	}
	if !spec.Audit && !spec.UserSlot && !spec.Billing && !spec.ImageSlot {
		return nil, true
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.runtime_preflight",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.String("endpoint", string(request.Runtime.Endpoint)),
		zap.String("model", spec.Model),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return nil, false
	}
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
		if request.Runtime.Exchange != nil {
			request.Runtime.Exchange.SetState("error_passthrough_service", h.errorPassthroughService)
		}
	}
	setOpsRequestContext(c, spec.Model, spec.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(spec.Stream, false)))
	if spec.Audit && len(spec.AuditBody) > 0 {
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, spec.Protocol, spec.Model, spec.AuditBody)
		if decision != nil && !decision.AllowNextStage {
			h.openAISecurityAuditError(c, decision)
			return nil, false
		}
	}
	// Keep the local Cyber session block before account selection. The legacy
	// endpoints used the raw client body (Responses deliberately used the
	// pre-normalization body), so use the runtime payload unchanged here too.
	if format, ok := runtimeOpenAICyberBlockFormat(request.Runtime.Endpoint); ok {
		if h.rejectIfCyberSessionBlocked(c, apiKey, request.Runtime.Payload, spec.Model, format) {
			return nil, false
		}
	}
	releases := make([]func(), 0, 2)
	addRelease := func(release func()) {
		if release != nil {
			releases = append(releases, release)
		}
	}
	if spec.ImageSlot {
		release, acquired := h.acquireImageGenerationSlot(c, false)
		if !acquired {
			return nil, false
		}
		addRelease(release)
	}
	if spec.UserSlot {
		streamStarted := false
		release, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, spec.Stream, &streamStarted, reqLog)
		if !acquired {
			releaseAllRuntimePreflightSlots(releases)
			return nil, false
		}
		addRelease(release)
	}
	if spec.Billing {
		subscription, _ := middleware2.GetSubscriptionFromContext(c)
		if h.billingCacheService == nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable")
			releaseAllRuntimePreflightSlots(releases)
			return nil, false
		}
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			}
			if request.Runtime.Endpoint == gatewayruntime.EndpointCountTokens {
				h.anthropicErrorResponse(c, status, code, message)
			} else {
				h.handleStreamingAwareError(c, status, code, message, false)
			}
			releaseAllRuntimePreflightSlots(releases)
			return nil, false
		}
	}
	return func() { releaseAllRuntimePreflightSlots(releases) }, true
}

func runtimeOpenAICyberBlockFormat(endpoint gatewayruntime.Endpoint) (cyberSessionBlockFormat, bool) {
	switch endpoint {
	case gatewayruntime.EndpointResponses:
		return cyberBlockFormatResponses, true
	case gatewayruntime.EndpointChatCompletions:
		return cyberBlockFormatChat, true
	case gatewayruntime.EndpointMessages:
		return cyberBlockFormatAnthropic, true
	default:
		return cyberBlockFormatResponses, false
	}
}

func (h *GatewayHandler) runtimeProductPreflight(c *gin.Context, request applicationgateway.DispatchRequest) (func(), bool) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return nil, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return nil, false
	}
	spec, err := buildRuntimeProductPreflightSpec(request.Runtime, nil)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, false
	}
	if request.Runtime.Endpoint == gatewayruntime.EndpointCountTokens && strings.EqualFold(strings.TrimSpace(request.Runtime.Adapter), service.PlatformGrok) {
		return nil, true
	}
	if !spec.Audit && !spec.UserSlot && !spec.Billing {
		return nil, true
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.runtime_preflight",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.String("endpoint", string(request.Runtime.Endpoint)),
		zap.String("model", spec.Model),
	)
	setOpsRequestContext(c, spec.Model, spec.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(spec.Stream, false)))
	if spec.Audit {
		decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, spec.Protocol, spec.Model, spec.AuditBody)
		if decision != nil && !decision.AllowNextStage {
			googleSecurityAuditError(c, decision)
			return nil, false
		}
	}
	if spec.UserSlot {
		if h.concurrencyHelper == nil || h.concurrencyHelper.concurrencyService == nil {
			googleError(c, http.StatusServiceUnavailable, "Concurrency service unavailable")
			return nil, false
		}
		streamStarted := false
		geminiConcurrency := NewConcurrencyHelper(h.concurrencyHelper.concurrencyService, SSEPingFormatNone, 0)
		release, err := geminiConcurrency.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, spec.Stream, &streamStarted)
		if err != nil {
			googleError(c, http.StatusTooManyRequests, err.Error())
			return nil, false
		}
		if release != nil {
			release = wrapReleaseOnDone(c.Request.Context(), release)
		}
		if spec.Billing {
			if h.billingCacheService == nil {
				if release != nil {
					release()
				}
				googleError(c, http.StatusServiceUnavailable, "Service temporarily unavailable")
				return nil, false
			}
			subscription, _ := middleware2.GetSubscriptionFromContext(c)
			if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
				if release != nil {
					release()
				}
				status, _, message, retryAfter := billingErrorDetails(err)
				if retryAfter > 0 {
					c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
				}
				googleError(c, status, message)
				return nil, false
			}
		}
		return release, true
	}
	if spec.Billing {
		if h.billingCacheService == nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Service temporarily unavailable")
			return nil, false
		}
		subscription, _ := middleware2.GetSubscriptionFromContext(c)
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return nil, false
		}
	}
	return nil, true
}

func releaseAllRuntimePreflightSlots(releases []func()) {
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i] != nil {
			releases[i]()
		}
	}
}
