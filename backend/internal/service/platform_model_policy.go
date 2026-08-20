package service

import (
	"context"
	"strings"
)

// platformRouteOwnsModelPolicy reports whether the request has already been
// resolved to an administrator-owned Platform. In that scope, account-level
// model mappings and endpoint allowlists are legacy metadata, not routing
// authority.
func platformRouteOwnsModelPolicy(ctx context.Context) bool {
	_, ok := PlatformSchedulingScopeFromContext(ctx)
	return ok
}

// resolveRequestUpstreamModel resolves the semantic upstream model before any
// adapter-specific normalization. A resolved Platform owns this decision;
// account model_mapping remains the legacy fallback outside Platform routes.
func resolveRequestUpstreamModel(ctx context.Context, account *Account, requestedModel string) (string, string) {
	requestedModel = strings.TrimSpace(requestedModel)
	if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		return platformModel, "platform"
	}
	if account == nil {
		return requestedModel, ""
	}
	if mappedModel, matched := account.ResolveMappedModel(requestedModel); matched {
		return strings.TrimSpace(mappedModel), "account"
	}
	return requestedModel, ""
}

// resolveOpenAIForwardModelWithContext preserves the existing OpenAI fallback
// mapping while preventing stale account administrator mappings from remapping
// a model already resolved by a Platform route.
func resolveOpenAIForwardModelWithContext(ctx context.Context, account *Account, requestedModel, messagesDispatchMappedModel string) string {
	if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		return platformModel
	}
	return resolveOpenAIForwardModel(account, requestedModel, messagesDispatchMappedModel)
}

func accountSupportsOpenAIEndpointForRequest(ctx context.Context, account *Account, capability OpenAIEndpointCapability) bool {
	if account == nil {
		return false
	}
	if platformRouteOwnsModelPolicy(ctx) {
		return account.SupportsOpenAIEndpointTechnicalCapability(capability)
	}
	return account.SupportsOpenAIEndpointCapability(capability)
}
