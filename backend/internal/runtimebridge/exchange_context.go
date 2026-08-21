package runtimebridge

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

type localExchangeContextKey struct{}

// WithLocalExchange carries the in-process transport surface only while a
// legacy local Driver is being migrated. It is not part of the public v1
// contract and cannot cross a process boundary.
func WithLocalExchange(ctx context.Context, exchange gatewayruntime.HTTPExchange) context.Context {
	if ctx == nil || exchange == nil {
		return ctx
	}
	return context.WithValue(ctx, localExchangeContextKey{}, exchange)
}

func LocalExchangeFromContext(ctx context.Context) (gatewayruntime.HTTPExchange, bool) {
	if ctx == nil {
		return nil, false
	}
	exchange, ok := ctx.Value(localExchangeContextKey{}).(gatewayruntime.HTTPExchange)
	return exchange, ok && exchange != nil
}
