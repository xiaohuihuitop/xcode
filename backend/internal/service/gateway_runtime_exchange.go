package service

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

// runtimeGinContext is private to this service compatibility seam. Runtime
// executors receive only gatewayruntime.HTTPExchange; the alias is not exposed
// outside service and can be removed when each Forward implementation is fully
// transport-neutral.
type runtimeGinContext = *gin.Context

const runtimeHTTPExchangeContextKey = "runtime.http.exchange"

type runtimeGinResponseWriter struct {
	exchange    gatewayruntime.HTTPExchange
	status      int
	size        int
	closeNotify chan bool
}

var _ gin.ResponseWriter = (*runtimeGinResponseWriter)(nil)

func newRuntimeGinResponseWriter(exchange gatewayruntime.HTTPExchange) *runtimeGinResponseWriter {
	return &runtimeGinResponseWriter{
		exchange:    exchange,
		status:      http.StatusOK,
		size:        -1,
		closeNotify: make(chan bool),
	}
}

func (w *runtimeGinResponseWriter) Header() http.Header {
	if w == nil || w.exchange == nil {
		return make(http.Header)
	}
	return w.exchange.Header()
}

func (w *runtimeGinResponseWriter) WriteHeader(status int) {
	if w == nil || w.exchange == nil || status <= 0 || w.Written() {
		return
	}
	w.status = status
	w.exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, status)
	w.exchange.WriteHeader(status)
	w.size = 0
}

func (w *runtimeGinResponseWriter) WriteHeaderNow() {
	if w == nil || w.Written() {
		return
	}
	w.WriteHeader(w.status)
}

func (w *runtimeGinResponseWriter) Write(body []byte) (int, error) {
	if w == nil || w.exchange == nil {
		return 0, errors.New("runtime response writer is unavailable")
	}
	w.WriteHeaderNow()
	n, err := w.exchange.Write(body)
	if w.size < 0 {
		w.size = 0
	}
	w.size += n
	return n, err
}

func (w *runtimeGinResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *runtimeGinResponseWriter) Status() int {
	if w == nil || w.status <= 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *runtimeGinResponseWriter) Size() int {
	if w == nil {
		return 0
	}
	if w.size < 0 && w.exchange != nil {
		return w.exchange.Size()
	}
	return w.size
}

func (w *runtimeGinResponseWriter) Written() bool {
	return w != nil && (w.size >= 0 || (w.exchange != nil && w.exchange.Written()))
}

func (w *runtimeGinResponseWriter) Flush() {
	if w == nil || w.exchange == nil {
		return
	}
	w.WriteHeaderNow()
	w.exchange.Flush()
}

func (w *runtimeGinResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("runtime exchange does not support hijacking")
}

func (w *runtimeGinResponseWriter) CloseNotify() <-chan bool {
	if w == nil {
		return nil
	}
	return w.closeNotify
}

func (w *runtimeGinResponseWriter) Pusher() http.Pusher { return nil }

// withRuntimeGinContext keeps the legacy service implementation isolated behind
// an exchange-based API while preserving response headers, body and diagnostic
// state for the outer runtime ingress.
func withRuntimeGinContext(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	apiKeyID int64,
	fn func(runtimeGinContext) error,
) error {
	if exchange == nil || fn == nil {
		return ErrRuntimeExchangeUnavailable
	}
	request := exchange.Request()
	if request == nil {
		return ErrRuntimeExchangeUnavailable
	}
	if ctx == nil {
		ctx = request.Context()
	}
	request = request.WithContext(ctx)
	c, _ := gin.CreateTestContext(nil)
	c.Request = request
	c.Writer = newRuntimeGinResponseWriter(exchange)
	c.Set(runtimeHTTPExchangeContextKey, exchange)
	if apiKeyID > 0 {
		c.Set("api_key", &APIKey{ID: apiKeyID})
	}
	// Preserve request-scoped service bindings installed by the product
	// preflight without exposing the original Gin context to the runtime.
	if value, ok := exchange.State(errorPassthroughServiceContextKey); ok && value != nil {
		c.Set(errorPassthroughServiceContextKey, value)
	}

	err := fn(c)
	copyRuntimeGinState(exchange, c)
	return err
}

func runtimeHTTPExchangeFromGinContext(c *gin.Context) (gatewayruntime.HTTPExchange, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(runtimeHTTPExchangeContextKey)
	if !ok || value == nil {
		return nil, false
	}
	exchange, ok := value.(gatewayruntime.HTTPExchange)
	return exchange, ok && exchange != nil
}

func copyRuntimeGinState(exchange gatewayruntime.HTTPExchange, c *gin.Context) {
	if exchange == nil || c == nil || c.Keys == nil {
		return
	}
	for key, value := range c.Keys {
		exchange.SetState(key, value)
	}
}

var ErrRuntimeExchangeUnavailable = errRuntimeExchangeUnavailable{}

type errRuntimeExchangeUnavailable struct{}

func (errRuntimeExchangeUnavailable) Error() string { return "runtime exchange is unavailable" }

func (s *OpenAIGatewayService) ForwardEmbeddingsRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	return s.ForwardEmbeddingsExchange(ctx, exchange, account, body, defaultMappedModel)
}

func (s *OpenAIGatewayService) ForwardAlphaSearchRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	apiKeyID int64,
) (*OpenAIForwardResult, error) {
	return s.ForwardAlphaSearchExchange(ctx, exchange, account, body, apiKeyID)
}

func (s *GatewayService) ForwardCountTokensRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	parsed *ParsedRequest,
) error {
	return s.ForwardCountTokensExchange(ctx, exchange, account, parsed)
}

func (s *OpenAIGatewayService) ForwardCountTokensAsAnthropicRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	defaultMappedModel string,
	apiKeyID int64,
) error {
	_ = apiKeyID
	return s.ForwardCountTokensAsAnthropicExchange(ctx, exchange, account, body, defaultMappedModel)
}

func (s *GeminiMessagesCompatService) ForwardNativeRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	originalModel, action string,
	stream bool,
	body []byte,
) (*ForwardResult, error) {
	if action == "countTokens" {
		return s.ForwardNativeCountTokensExchange(ctx, exchange, account, originalModel, body)
	}
	var result *ForwardResult
	err := withRuntimeGinContext(ctx, exchange, 0, func(c *gin.Context) error {
		var err error
		result, err = s.ForwardNative(ctx, c, account, originalModel, action, stream, body)
		return err
	})
	return result, err
}

func (s *AntigravityGatewayService) ForwardGeminiRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	originalModel, action string,
	stream bool,
	body []byte,
	isStickySession bool,
	platformID int64,
	sessionHash string,
) (*ForwardResult, error) {
	var result *ForwardResult
	err := withRuntimeGinContext(ctx, exchange, 0, func(c *gin.Context) error {
		var err error
		result, err = s.ForwardGemini(ctx, c, account, originalModel, action, stream, body, isStickySession,
			WithForwardGeminiSession(platformID, sessionHash))
		return err
	})
	return result, err
}

func (s *OpenAIGatewayService) ParseOpenAIImagesRequestRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	body []byte,
) (*OpenAIImagesRequest, error) {
	if exchange == nil || exchange.Request() == nil || exchange.Request().URL == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	return s.ParseOpenAIImagesRequestFromMetadata(
		exchange.Request().URL.Path,
		exchange.Request().Header.Get("Content-Type"),
		body,
	)
}

func (s *OpenAIGatewayService) ForwardImagesRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	return s.ForwardImagesExchange(ctx, exchange, account, body, parsed, channelMappedModel)
}

func (s *OpenAIGatewayService) ForwardGrokMediaRuntime(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	endpoint GrokMediaEndpoint,
	requestID string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	var result *OpenAIForwardResult
	err := withRuntimeGinContext(ctx, exchange, 0, func(c *gin.Context) error {
		var err error
		result, err = s.ForwardGrokMedia(ctx, c, account, endpoint, requestID, body, contentType)
		return err
	})
	return result, err
}
