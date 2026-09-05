package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	ErrRuntimeIngressUnavailable  = errors.New("runtime ingress is unavailable")
	ErrRuntimeIngressUnauthorized = errors.New("runtime ingress authentication is unavailable")
)

type runtimeIngressPreflight func(*gin.Context, applicationgateway.DispatchRequest) (release func(), proceed bool)

// buildRuntimeDispatchRequest is the only place where Gin authentication
// state is converted into the application/runtime contracts. The runtime
// receives copied grant and route values, never the middleware objects.
func buildRuntimeDispatchRequest(c *gin.Context, endpoint gatewayruntime.Endpoint) (applicationgateway.DispatchRequest, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return applicationgateway.DispatchRequest{}, ErrRuntimeIngressUnavailable
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		return applicationgateway.DispatchRequest{}, ErrRuntimeIngressUnauthorized
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		return applicationgateway.DispatchRequest{}, ErrRuntimeIngressUnauthorized
	}
	route, ok := service.GatewayPlatformAssetContextFromContext(c.Request.Context())
	if !ok || route == nil || route.Platform == nil {
		return applicationgateway.DispatchRequest{}, service.ErrAPIKeyPlatformForbidden
	}
	platform := route.Platform
	payload, err := readRuntimePayload(c.Request)
	if err != nil {
		return applicationgateway.DispatchRequest{}, fmt.Errorf("read runtime request body: %w", err)
	}
	if endpoint == gatewayruntime.EndpointResponses {
		reqLog := requestLogger(c, "handler.openai_gateway.runtime_ingress")
		if normalized, changed := normalizeCodexAutomationBootstrap(payload); changed {
			payload = normalized
			reqLog.Info("openai.codex_automation_bootstrap_normalized",
				zap.String("normalization", "call_output_to_user_message"),
			)
		}
		if normalized, changed := normalizeCodexDelegationBootstrap(payload); changed {
			payload = normalized
			reqLog.Info("openai.codex_delegation_bootstrap_normalized",
				zap.String("normalization", "call_output_to_user_message"),
			)
		}
	}
	request := gatewayruntime.Request{
		RequestID:       runtimeRequestID(c),
		Endpoint:        endpoint,
		InboundEndpoint: c.Request.URL.Path,
		PlatformID:      platform.PlatformID,
		PlatformCode:    platform.PlatformCode,
		Adapter:         platform.AccountPlatform,
		RequestedModel:  strings.TrimSpace(platform.RequestedModel),
		UpstreamModel:   strings.TrimSpace(platform.UpstreamModel),
		Stream:          requestLikelyStreams(c),
		Payload:         payload,
		Metadata:        runtimeRequestMetadata(c),
		Exchange:        NewGinHTTPExchange(c),
	}
	if stream, ok := parseOpenAICompatibleStream(payload); ok {
		request.Stream = stream
	}
	return applicationgateway.DispatchRequest{
		Grant: productcore.AccessGrant{
			UserID:              subject.UserID,
			KeyID:               apiKey.ID,
			PlatformIDs:         append([]int64(nil), apiKey.AllowedPlatformIDs...),
			SubscriptionPlanIDs: append([]int64(nil), apiKey.AllowedSubscriptionPlanIDs...),
			AllowBalance:        apiKey.AllowBalance,
		},
		Product: productcore.Request{
			Model:              strings.TrimSpace(platform.RequestedModel),
			EndpointCapability: endpointCapabilityForRuntime(endpoint),
		},
		Runtime: request,
	}, nil
}

func runtimeRequestID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if value, ok := c.Request.Context().Value(ctxkey.RequestID).(string); ok {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	if value, ok := c.Request.Context().Value(ctxkey.ClientRequestID).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func readRuntimePayload(request *http.Request) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func dispatchRuntimeEndpoint(
	c *gin.Context,
	endpoint gatewayruntime.Endpoint,
	gateway *applicationgateway.Gateway,
	preflights ...runtimeIngressPreflight,
) error {
	request, err := buildRuntimeDispatchRequest(c, endpoint)
	if err != nil {
		return writeRuntimeIngressError(c, err)
	}
	if gateway == nil {
		return writeRuntimeIngressError(c, ErrSub2APIRuntimeUnavailable)
	}
	if len(preflights) > 1 {
		return writeRuntimeIngressError(c, fmt.Errorf("runtime ingress accepts at most one product preflight"))
	}
	if len(preflights) == 1 && preflights[0] != nil {
		release, proceed := preflights[0](c, request)
		if release != nil {
			defer release()
		}
		if !proceed {
			return nil
		}
	}
	_, err = gateway.Dispatch(c.Request.Context(), request)
	if err != nil && (c.Writer == nil || !c.Writer.Written()) {
		return writeRuntimeIngressError(c, err)
	}
	return err
}

func writeRuntimeIngressError(c *gin.Context, err error) error {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return err
	}
	status := http.StatusInternalServerError
	if errors.Is(err, ErrRuntimeIngressUnauthorized) {
		status = http.StatusUnauthorized
	}
	if errors.Is(err, service.ErrAPIKeyPlatformForbidden) {
		status = http.StatusForbidden
	}
	c.JSON(status, gin.H{"error": gin.H{"type": "server_error", "message": err.Error()}})
	return err
}

func runtimeRequestMetadata(c *gin.Context) gatewayruntime.RequestMetadata {
	if c == nil || c.Request == nil {
		return gatewayruntime.RequestMetadata{}
	}
	headers := make(map[string]string)
	for _, name := range []string{"Accept", "Content-Type", "User-Agent"} {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			headers[name] = value
		}
	}
	return gatewayruntime.RequestMetadata{
		APIKeyID:  apiKeyIDFromContext(c),
		UserID:    authSubjectUserIDFromContext(c),
		Headers:   headers,
		UserAgent: c.GetHeader("User-Agent"),
		ClientIP:  c.ClientIP(),
		SessionID: service.ExtractClientSessionID(c),
	}
}

func authSubjectUserIDFromContext(c *gin.Context) int64 {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}

func apiKeyIDFromContext(c *gin.Context) int64 {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		return 0
	}
	return apiKey.ID
}
