package service

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

// ForwardResponsesInputTokens preserves the official service-level entry point
// for callers that still own a Gin context. Production Runtime dispatch uses
// ForwardResponsesInputTokensExchange instead.
func (s *OpenAIGatewayService) ForwardResponsesInputTokens(ctx context.Context, c *gin.Context, account *Account, body []byte) error {
	if c == nil || c.Request == nil || c.Writer == nil {
		return ErrRuntimeExchangeUnavailable
	}
	return s.ForwardResponsesInputTokensExchange(ctx, &ginContextRuntimeExchange{context: c}, account, body)
}

type ginContextRuntimeExchange struct{ context *gin.Context }

func (e *ginContextRuntimeExchange) Request() *http.Request { return e.context.Request }
func (e *ginContextRuntimeExchange) Header() http.Header    { return e.context.Writer.Header() }
func (e *ginContextRuntimeExchange) WriteHeader(status int) {
	e.context.Writer.WriteHeader(status)
	e.context.Set(gatewayruntime.HTTPExchangeStatusStateKey, status)
}
func (e *ginContextRuntimeExchange) Write(body []byte) (int, error) {
	return e.context.Writer.Write(body)
}
func (e *ginContextRuntimeExchange) Flush()                         { e.context.Writer.Flush() }
func (e *ginContextRuntimeExchange) Written() bool                  { return e.context.Writer.Written() }
func (e *ginContextRuntimeExchange) Size() int                      { return e.context.Writer.Size() }
func (e *ginContextRuntimeExchange) SetState(key string, value any) { e.context.Set(key, value) }
func (e *ginContextRuntimeExchange) State(key string) (any, bool)   { return e.context.Get(key) }

var _ gatewayruntime.HTTPExchange = (*ginContextRuntimeExchange)(nil)
