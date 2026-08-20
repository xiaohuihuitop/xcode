package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/google/uuid"
)

// openAIRequestBuildRuntimeState contains only request-scoped compatibility
// facts needed while constructing an upstream Responses request. It is
// deliberately independent of Gin and product-side entities.
type openAIRequestBuildRuntimeState struct {
	APIKeyID         int64
	MessagesBridge   bool
	CompactSessionID string
}

// buildOpenAIUpstreamRequestFromExchange is the transport-neutral request
// builder used by the runtime boundary. Response handling remains a separate
// migration step so request headers and URL construction can be verified in
// isolation first.
func (s *OpenAIGatewayService) buildOpenAIUpstreamRequestFromExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	token string,
	isStream bool,
	promptCacheKey string,
	isCodexCLI bool,
	apiKeyID int64,
) (*http.Request, error) {
	if exchange == nil || exchange.Request() == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	inbound := exchange.Request()
	state := openAIRequestBuildRuntimeState{
		APIKeyID:       apiKeyID,
		MessagesBridge: isOpenAICompatMessagesBridgeBody(body),
	}
	if value, ok := exchange.State(openAICompatMessagesBridgeContextKey); ok {
		if enabled, ok := value.(bool); ok {
			state.MessagesBridge = state.MessagesBridge || enabled
		}
	}
	if value, ok := exchange.State(openAICompactSessionSeedKey); ok {
		if seed, ok := value.(string); ok {
			state.CompactSessionID = strings.TrimSpace(seed)
		}
	}
	return s.buildOpenAIUpstreamRequestFromRuntimeRequestWithState(
		ctx, inbound, account, body, token, isStream, promptCacheKey, isCodexCLI, state,
	)
}

// buildOpenAIUpstreamRequestFromRuntimeRequest is a small convenience entry
// point for protocol tests and future runtime callers that already own the
// inbound request. The exchange variant above is the production boundary.
func (s *OpenAIGatewayService) buildOpenAIUpstreamRequestFromRuntimeRequest(
	ctx context.Context,
	inbound *http.Request,
	account *Account,
	body []byte,
	token string,
	isStream bool,
	promptCacheKey string,
	isCodexCLI bool,
	apiKeyID int64,
) (*http.Request, error) {
	if inbound == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	return s.buildOpenAIUpstreamRequestFromRuntimeRequestWithState(
		ctx,
		inbound,
		account,
		body,
		token,
		isStream,
		promptCacheKey,
		isCodexCLI,
		openAIRequestBuildRuntimeState{
			APIKeyID:       apiKeyID,
			MessagesBridge: isOpenAICompatMessagesBridgeBody(body),
		},
	)
}

func (s *OpenAIGatewayService) buildOpenAIUpstreamRequestFromRuntimeRequestWithState(
	ctx context.Context,
	inbound *http.Request,
	account *Account,
	body []byte,
	token string,
	isStream bool,
	promptCacheKey string,
	isCodexCLI bool,
	state openAIRequestBuildRuntimeState,
) (*http.Request, error) {
	if inbound == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = inbound.Context()
	}

	var targetURL string
	switch account.Type {
	case AccountTypeOAuth:
		targetURL = chatgptCodexURL
	case AccountTypeAPIKey:
		baseURL := account.GetOpenAIBaseURL()
		if baseURL == "" {
			targetURL = openaiPlatformAPIURL
		} else {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = buildOpenAIResponsesURL(validatedURL)
		}
	default:
		targetURL = openaiPlatformAPIURL
	}
	targetURL = appendOpenAIResponsesRequestPathSuffix(
		targetURL,
		openAIResponsesRequestPathSuffixFromRuntimeRequest(inbound),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))

	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, fmt.Errorf("build openai authentication headers: %w", err)
	}
	for key, values := range authHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	if account.Type == AccountTypeOAuth {
		req.Host = "chatgpt.com"
		if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, account); err != nil {
			return nil, fmt.Errorf("resolve chatgpt account headers: %w", err)
		}
	}

	for key, values := range inbound.Header {
		lowerKey := strings.ToLower(key)
		if !openaiAllowedHeaders[lowerKey] {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	compact := isOpenAIResponsesCompactPathFromRuntimeRequest(inbound)
	if account.Type == AccountTypeOAuth {
		compatMessagesBridge := state.MessagesBridge || isOpenAICompatMessagesBridgeBody(body)
		clientConversationID := strings.TrimSpace(req.Header.Get("conversation_id"))
		req.Header.Del("conversation_id")
		req.Header.Del("session_id")

		if compatMessagesBridge {
			req.Header.Del("OpenAI-Beta")
			req.Header.Del("originator")
		} else {
			req.Header.Set("OpenAI-Beta", "responses=experimental")
			req.Header.Set("originator", resolveOpenAIUpstreamOriginatorFromHeaders(inbound.Header, isCodexCLI))
		}

		if compact {
			req.Header.Set("accept", "application/json")
			if req.Header.Get("version") == "" {
				req.Header.Set("version", codexCLIVersion)
			}
			compactSession := strings.TrimSpace(state.CompactSessionID)
			if compactSession == "" {
				compactSession = resolveOpenAICompactSessionIDFromRuntimeRequest(inbound)
			}
			req.Header.Set("session_id", isolateOpenAISessionID(state.APIKeyID, compactSession))
		} else {
			req.Header.Set("accept", "text/event-stream")
		}
		if promptCacheKey != "" {
			isolated := isolateOpenAISessionID(state.APIKeyID, promptCacheKey)
			req.Header.Set("session_id", isolated)
			if !compatMessagesBridge || clientConversationID != "" {
				req.Header.Set("conversation_id", isolated)
			}
		}
	} else if compact {
		req.Header.Set("accept", "application/json")
	}

	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		req.Header.Set("user-agent", customUA)
	}
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		req.Header.Set("user-agent", codexCLIUserAgent)
	}
	s.overrideBrowserUserAgent(ctx, account, req)
	if account.Type == AccountTypeOAuth {
		enforceCodexIdentityHeaders(req.Header)
	}
	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}
	account.ApplyHeaderOverrides(req.Header)

	_ = isStream // retained in the shared signature for parity with the legacy builder
	return req, nil
}

func openAIResponsesRequestPathSuffixFromRuntimeRequest(inbound *http.Request) string {
	if inbound == nil || inbound.URL == nil {
		return ""
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(inbound.URL.Path), "/")
	if normalizedPath == "" {
		return ""
	}
	idx := strings.LastIndex(normalizedPath, "/responses")
	if idx < 0 {
		return ""
	}
	suffix := normalizedPath[idx+len("/responses"):]
	if suffix == "" || suffix == "/" || !strings.HasPrefix(suffix, "/") {
		return ""
	}
	clean, ok := sanitizedUpstreamPathSuffix(suffix)
	if !ok {
		return ""
	}
	return clean
}

func isOpenAIResponsesCompactPathFromRuntimeRequest(inbound *http.Request) bool {
	suffix := openAIResponsesRequestPathSuffixFromRuntimeRequest(inbound)
	return suffix == "/compact" || strings.HasPrefix(suffix, "/compact/")
}

func resolveOpenAICompactSessionIDFromRuntimeRequest(inbound *http.Request) string {
	if inbound != nil {
		if sessionID := strings.TrimSpace(inbound.Header.Get("session_id")); sessionID != "" {
			return sessionID
		}
		if conversationID := strings.TrimSpace(inbound.Header.Get("conversation_id")); conversationID != "" {
			return conversationID
		}
	}
	return openAICompactSessionSeedFallback()
}

func openAICompactSessionSeedFallback() string {
	return uuid.NewString()
}

func resolveOpenAIUpstreamOriginatorFromHeaders(headers http.Header, isOfficialClient bool) string {
	if originator := strings.TrimSpace(headers.Get("originator")); originator != "" {
		return originator
	}
	if isOfficialClient {
		return "codex_cli_rs"
	}
	return "opencode"
}
