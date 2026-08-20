package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

type ginHTTPExchange struct {
	context *gin.Context
}

func NewGinHTTPExchange(c *gin.Context) gatewayruntime.HTTPExchange {
	return &ginHTTPExchange{context: c}
}

func (e *ginHTTPExchange) Request() *http.Request {
	if e == nil || e.context == nil {
		return nil
	}
	return e.context.Request
}

func (e *ginHTTPExchange) Header() http.Header {
	if e == nil || e.context == nil || e.context.Writer == nil {
		return make(http.Header)
	}
	return e.context.Writer.Header()
}

func (e *ginHTTPExchange) WriteHeader(status int) {
	if e == nil || e.context == nil || e.context.Writer == nil {
		return
	}
	e.context.Writer.WriteHeader(status)
	e.context.Set(gatewayruntime.HTTPExchangeStatusStateKey, status)
}

func (e *ginHTTPExchange) Write(body []byte) (int, error) {
	if e == nil || e.context == nil || e.context.Writer == nil {
		return 0, http.ErrNotSupported
	}
	return e.context.Writer.Write(body)
}

func (e *ginHTTPExchange) Flush() {
	if e == nil || e.context == nil || e.context.Writer == nil {
		return
	}
	e.context.Writer.Flush()
}

func (e *ginHTTPExchange) Written() bool {
	return e != nil && e.context != nil && e.context.Writer != nil && e.context.Writer.Written()
}

func (e *ginHTTPExchange) Size() int {
	if e == nil || e.context == nil || e.context.Writer == nil {
		return 0
	}
	return e.context.Writer.Size()
}

func (e *ginHTTPExchange) SetState(key string, value any) {
	if e == nil || e.context == nil {
		return
	}
	e.context.Set(key, value)
}

func (e *ginHTTPExchange) State(key string) (any, bool) {
	if e == nil || e.context == nil {
		return nil, false
	}
	return e.context.Get(key)
}
