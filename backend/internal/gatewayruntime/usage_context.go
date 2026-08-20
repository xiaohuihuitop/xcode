package gatewayruntime

import "context"

type usageSinkContextKey struct{}

// WithUsageSink carries the request-scoped terminal sink through compatibility
// helpers without exposing product or framework types in the runtime contract.
func WithUsageSink(ctx context.Context, sink UsageSink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, usageSinkContextKey{}, sink)
}

func UsageSinkFromContext(ctx context.Context) (UsageSink, bool) {
	if ctx == nil {
		return nil, false
	}
	sink, ok := ctx.Value(usageSinkContextKey{}).(UsageSink)
	return sink, ok && sink != nil
}
