package handler

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var (
	ErrSub2APIExecutionUnavailable = errors.New("sub2api execution context is unavailable")
)

// sub2APIExecution is the adapter-local execution surface. It carries a pure
// runtime request and exchange; the temporary service route compatibility
// context is created here and never exposed to ProductCore.
type sub2APIExecution struct {
	ctx      context.Context
	request  gatewayruntime.Request
	exchange gatewayruntime.HTTPExchange
	sink     gatewayruntime.UsageSink
}

func newSub2APIExecution(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (*sub2APIExecution, error) {
	if request.Exchange == nil {
		return nil, ErrSub2APIExecutionUnavailable
	}
	if sink == nil {
		return nil, gatewayruntime.ErrUsageSinkUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = gatewayruntime.WithUsageSink(ctx, sink)
	// Always replace the ingress route with the adapter compatibility route.
	// Product billing is owned by ApplicationGateway/UsageSink and must not
	// leak into the Sub2API execution context, even when the parent context
	// already carries a resolved billing asset.
	if route := runtimeCompatibilityRoute(request); route != nil {
		ctx = service.WithGatewayPlatformAssetContext(ctx, route)
	}
	request.Payload = append([]byte(nil), request.Payload...)
	request.Metadata.Headers = cloneRuntimeMetadataHeaders(request.Metadata.Headers)
	return &sub2APIExecution{
		ctx:      ctx,
		request:  request,
		exchange: request.Exchange,
		sink:     sink,
	}, nil
}

func (e *sub2APIExecution) Context() context.Context {
	if e == nil || e.ctx == nil {
		return context.Background()
	}
	return e.ctx
}

func (e *sub2APIExecution) Request() gatewayruntime.Request {
	if e == nil {
		return gatewayruntime.Request{}
	}
	return e.request
}

func (e *sub2APIExecution) Exchange() gatewayruntime.HTTPExchange {
	if e == nil {
		return nil
	}
	return e.exchange
}

func (e *sub2APIExecution) Sink() gatewayruntime.UsageSink {
	if e == nil {
		return nil
	}
	return e.sink
}

func cloneRuntimeMetadataHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}
