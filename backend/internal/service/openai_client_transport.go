package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

// OpenAIClientTransport 表示客户端入站协议类型。
type OpenAIClientTransport string

const (
	OpenAIClientTransportUnknown OpenAIClientTransport = ""
	OpenAIClientTransportHTTP    OpenAIClientTransport = "http"
	OpenAIClientTransportWS      OpenAIClientTransport = "ws"
)

const openAIClientTransportContextKey = "openai_client_transport"

// SetOpenAIClientTransport 标记当前请求的客户端入站协议。
func SetOpenAIClientTransport(c *gin.Context, transport OpenAIClientTransport) {
	if c == nil {
		return
	}
	normalized := normalizeOpenAIClientTransport(transport)
	if normalized == OpenAIClientTransportUnknown {
		return
	}
	c.Set(openAIClientTransportContextKey, string(normalized))
}

// GetOpenAIClientTransport 读取当前请求的客户端入站协议。
func GetOpenAIClientTransport(c *gin.Context) OpenAIClientTransport {
	if c == nil {
		return OpenAIClientTransportUnknown
	}
	raw, ok := c.Get(openAIClientTransportContextKey)
	if !ok || raw == nil {
		return OpenAIClientTransportUnknown
	}

	switch v := raw.(type) {
	case OpenAIClientTransport:
		return normalizeOpenAIClientTransport(v)
	case string:
		return normalizeOpenAIClientTransport(OpenAIClientTransport(v))
	default:
		return OpenAIClientTransportUnknown
	}
}

// SetOpenAIClientTransportExchange stores the inbound protocol on the pure
// runtime exchange. It is the exchange counterpart of the legacy Gin helper;
// the value is a transport fact and contains no product-side state.
func SetOpenAIClientTransportExchange(exchange gatewayruntime.HTTPExchange, transport OpenAIClientTransport) {
	if exchange == nil {
		return
	}
	normalized := normalizeOpenAIClientTransport(transport)
	if normalized == OpenAIClientTransportUnknown {
		return
	}
	exchange.SetState(openAIClientTransportContextKey, string(normalized))
}

// GetOpenAIClientTransportExchange reads the inbound protocol from an
// exchange without requiring a Gin compatibility context.
func GetOpenAIClientTransportExchange(exchange gatewayruntime.HTTPExchange) OpenAIClientTransport {
	if exchange == nil {
		return OpenAIClientTransportUnknown
	}
	raw, ok := exchange.State(openAIClientTransportContextKey)
	if !ok || raw == nil {
		return OpenAIClientTransportUnknown
	}
	switch value := raw.(type) {
	case OpenAIClientTransport:
		return normalizeOpenAIClientTransport(value)
	case string:
		return normalizeOpenAIClientTransport(OpenAIClientTransport(value))
	default:
		return OpenAIClientTransportUnknown
	}
}

func normalizeOpenAIClientTransport(transport OpenAIClientTransport) OpenAIClientTransport {
	switch strings.ToLower(strings.TrimSpace(string(transport))) {
	case string(OpenAIClientTransportHTTP), "http_sse", "sse":
		return OpenAIClientTransportHTTP
	case string(OpenAIClientTransportWS), "websocket":
		return OpenAIClientTransportWS
	default:
		return OpenAIClientTransportUnknown
	}
}

func resolveOpenAIWSDecisionByClientTransport(
	decision OpenAIWSProtocolDecision,
	clientTransport OpenAIClientTransport,
) OpenAIWSProtocolDecision {
	if clientTransport == OpenAIClientTransportHTTP {
		return openAIWSHTTPDecision("client_protocol_http")
	}
	return decision
}
