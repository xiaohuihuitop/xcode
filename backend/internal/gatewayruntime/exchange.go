package gatewayruntime

import (
	"net/http"
)

// HTTPExchangeStatusStateKey is written by the outer transport implementation
// after a response is committed. Runtime code can inspect the status without
// depending on Gin's response writer type.
const HTTPExchangeStatusStateKey = "runtime.http.status"

// HTTPExchange is the small transport surface needed by an in-process
// runtime. It deliberately exposes no Gin or product-asset types.
type HTTPExchange interface {
	Request() *http.Request
	Header() http.Header
	WriteHeader(status int)
	Write(body []byte) (int, error)
	Flush()
	Written() bool
	Size() int
	SetState(key string, value any)
	State(key string) (any, bool)
}
