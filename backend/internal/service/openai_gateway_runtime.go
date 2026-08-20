package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

// ClearActualOpenAIUpstreamEndpointExchange clears the per-attempt endpoint
// marker before a runtime retry. This prevents a failed Chat/Responses
// attempt from contaminating the next account's usage or Ops facts.
func ClearActualOpenAIUpstreamEndpointExchange(exchange gatewayruntime.HTTPExchange) {
	if exchange != nil {
		exchange.SetState(openAIUpstreamEndpointContextKey, "")
	}
}

// SetActualOpenAIUpstreamEndpointExchange stores the canonical inbound
// endpoint before the pure preparation stage starts. The attempt core may
// overwrite it for a compatibility/failover endpoint.
func SetActualOpenAIUpstreamEndpointExchange(exchange gatewayruntime.HTTPExchange, endpoint string) {
	if exchange == nil {
		return
	}
	exchange.SetState(openAIUpstreamEndpointContextKey, strings.TrimSpace(endpoint))
}

// ActualOpenAIUpstreamEndpointFromExchange returns the endpoint marker copied
// out of the temporary service transport context.
func ActualOpenAIUpstreamEndpointFromExchange(exchange gatewayruntime.HTTPExchange) string {
	if exchange == nil {
		return ""
	}
	value, ok := exchange.State(openAIUpstreamEndpointContextKey)
	if !ok {
		return ""
	}
	endpoint, _ := value.(string)
	return strings.TrimSpace(endpoint)
}

// ForwardRuntime forwards an OpenAI Responses request through the runtime
// exchange surface. Ordinary native OpenAI HTTP/SSE requests use the pure
// exchange preparation and attempt boundary; compact, passthrough, WebSocket
// and compatibility modes remain explicit legacy branches for now.
func (s *OpenAIGatewayService) ForwardRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	SetOpenAIClientTransportExchange(exchange, OpenAIClientTransportHTTP)
	if account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.ForwardResponsesExchange(ctx, exchange, account, body)
	}
	if s.shouldUseOpenAIResponsesHTTPRuntime(exchange, account) {
		return s.forwardOpenAIResponsesHTTPRuntime(ctx, exchange, account, body, apiKeyID)
	}
	// Deferred protocol modes intentionally retain the compatibility bridge so
	// their distinct session/WS semantics are not silently changed by the
	// ordinary HTTP migration.
	var result *OpenAIForwardResult
	err := withRuntimeGinContext(ctx, exchange, apiKeyID, func(c runtimeGinContext) error {
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		var err error
		result, err = s.Forward(ctx, c, account, body)
		return err
	})
	return result, err
}

func (s *OpenAIGatewayService) shouldUseOpenAIResponsesHTTPRuntime(
	exchange gatewayruntime.HTTPExchange,
	account *Account,
) bool {
	if s == nil || exchange == nil || exchange.Request() == nil || account == nil || account.Platform != PlatformOpenAI {
		return false
	}
	if account.IsOpenAIPassthroughEnabled() || isOpenAIResponsesCompactPathFromRuntimeRequest(exchange.Request()) {
		return false
	}
	if account.Type == AccountTypeAPIKey && !openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return false
	}
	decision := s.getOpenAIWSProtocolResolver().Resolve(account)
	decision = resolveOpenAIWSDecisionByClientTransport(decision, OpenAIClientTransportHTTP)
	return decision.Transport == OpenAIUpstreamTransportHTTPSSE
}

// ForwardAsChatCompletionsRuntime forwards a Chat Completions request through
// the exchange surface while preserving the existing account-specific
// protocol negotiation and failover errors.
func (s *OpenAIGatewayService) ForwardAsChatCompletionsRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey &&
		!openai_compat.ShouldRouteChatCompletionsViaResponses(account.Extra) {
		return s.ForwardAsChatCompletionsExchange(ctx, exchange, account, body, promptCacheKey, defaultMappedModel)
	}
	if s.shouldUseOpenAIChatResponsesHTTPRuntime(exchange, account) {
		return s.forwardOpenAIChatCompletionsHTTPRuntime(ctx, exchange, account, body, promptCacheKey, defaultMappedModel, apiKeyID)
	}
	var result *OpenAIForwardResult
	err := withRuntimeGinContext(ctx, exchange, apiKeyID, func(c runtimeGinContext) error {
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		var err error
		result, err = s.ForwardAsChatCompletions(ctx, c, account, body, promptCacheKey, defaultMappedModel)
		return err
	})
	return result, err
}

// ForwardAsAnthropicRuntime forwards an Anthropic Messages request through
// the exchange surface. OpenAI HTTP/SSE accounts, including GPT-5/Codex
// replay and continuation traffic, use the pure Responses exchange pipeline;
// only compact, passthrough and WebSocket transports remain explicit legacy
// protocol modes.
func (s *OpenAIGatewayService) ForwardAsAnthropicRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	promptCacheKey string,
	defaultMappedModel string,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	if account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return s.ForwardAsAnthropicExchange(ctx, exchange, account, body, defaultMappedModel)
	}
	if s.shouldUseOpenAIMessagesHTTPRuntime(ctx, exchange, account, body, defaultMappedModel) {
		return s.forwardOpenAIMessagesHTTPRuntime(ctx, exchange, account, body, promptCacheKey, defaultMappedModel, apiKeyID)
	}
	var result *OpenAIForwardResult
	err := withRuntimeGinContext(ctx, exchange, apiKeyID, func(c runtimeGinContext) error {
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		var err error
		result, err = s.ForwardAsAnthropic(ctx, c, account, body, promptCacheKey, defaultMappedModel)
		return err
	})
	return result, err
}
