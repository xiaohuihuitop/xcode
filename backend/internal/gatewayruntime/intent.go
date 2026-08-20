package gatewayruntime

import "context"

type DispatchIntent struct {
	Platform PlatformRoute
}

type dispatchIntentContextKey struct{}

func WithDispatchIntent(ctx context.Context, intent *DispatchIntent) context.Context {
	if ctx == nil || intent == nil {
		return ctx
	}
	return context.WithValue(ctx, dispatchIntentContextKey{}, cloneIntent(intent))
}

func DispatchIntentFromContext(ctx context.Context) (*DispatchIntent, bool) {
	if ctx == nil {
		return nil, false
	}
	intent, ok := ctx.Value(dispatchIntentContextKey{}).(*DispatchIntent)
	if !ok || intent == nil {
		return nil, false
	}
	return cloneIntent(intent), true
}

func cloneIntent(intent *DispatchIntent) *DispatchIntent {
	if intent == nil {
		return nil
	}
	return &DispatchIntent{
		Platform: PlatformRoute{
			ID:                   intent.Platform.ID,
			Code:                 intent.Platform.Code,
			Adapter:              intent.Platform.Adapter,
			RequestedModel:       intent.Platform.RequestedModel,
			UpstreamModel:        intent.Platform.UpstreamModel,
			EndpointCapabilities: append([]string(nil), intent.Platform.EndpointCapabilities...),
			MatchPriority:        intent.Platform.MatchPriority,
		},
	}
}
