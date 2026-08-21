package v1

import (
	"errors"
	"fmt"
)

const (
	// ContractVersionV1 is the only contract version understood by this package.
	ContractVersionV1 = "v1"
	// CurrentContractVersion identifies the version emitted by this package.
	CurrentContractVersion = ContractVersionV1
)

type Endpoint string

const (
	EndpointMessages        Endpoint = "messages"
	EndpointChatCompletions Endpoint = "chat_completions"
	EndpointResponses       Endpoint = "responses"
	EndpointGeminiNative    Endpoint = "gemini_native"
	EndpointEmbeddings      Endpoint = "embeddings"
	EndpointAlphaSearch     Endpoint = "alpha_search"
	EndpointImages          Endpoint = "images"
	EndpointVideos          Endpoint = "videos"
	EndpointCountTokens     Endpoint = "count_tokens"
	EndpointLive            Endpoint = "live"
	EndpointWebSocket       Endpoint = "websocket"
)

type EventKind string

const (
	EventResponseStarted  EventKind = "response_started"
	EventResponseChunk    EventKind = "response_chunk"
	EventResponseFinished EventKind = "response_finished"
	EventRuntimeFailed    EventKind = "runtime_failed"
	EventUsageFinal       EventKind = "usage_final"
	EventStreamCancelled  EventKind = "stream_cancelled"
)

type PlatformRoute struct {
	ID                   int64    `json:"id"`
	Code                 string   `json:"code"`
	RuntimeAdapter       string   `json:"runtime_adapter"`
	RequestedModel       string   `json:"requested_model"`
	UpstreamModel        string   `json:"upstream_model"`
	EndpointCapabilities []string `json:"endpoint_capabilities,omitempty"`
}

type OwnerRef struct {
	UserID   int64 `json:"user_id"`
	APIKeyID int64 `json:"api_key_id"`
}

type SessionMetadata struct {
	SessionID          string `json:"session_id,omitempty"`
	UserAgent          string `json:"user_agent,omitempty"`
	ClientIP           string `json:"client_ip,omitempty"`
	RequestPayloadHash string `json:"request_payload_hash,omitempty"`
}

type Request struct {
	ContractVersion string            `json:"contract_version"`
	RequestID       string            `json:"request_id"`
	Platform        PlatformRoute     `json:"platform"`
	Endpoint        Endpoint          `json:"endpoint"`
	InboundEndpoint string            `json:"inbound_endpoint,omitempty"`
	Stream          bool              `json:"stream"`
	Payload         []byte            `json:"payload,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Owner           OwnerRef          `json:"owner"`
	Session         SessionMetadata   `json:"session"`
}

type Result struct {
	StatusCode       int                 `json:"status_code"`
	ResponseHeaders  map[string][]string `json:"response_headers,omitempty"`
	Body             []byte              `json:"body,omitempty"`
	Streamed         bool                `json:"streamed"`
	AccountID        int64               `json:"account_id"`
	UpstreamEndpoint string              `json:"upstream_endpoint,omitempty"`
	UpstreamModel    string              `json:"upstream_model,omitempty"`
	Usage            UsageFacts          `json:"usage"`
}

type UsageFacts struct {
	Adapter                  string  `json:"adapter"`
	AccountID                int64   `json:"account_id"`
	PlatformID               int64   `json:"platform_id"`
	Endpoint                 string  `json:"endpoint"`
	RequestedModel           string  `json:"requested_model"`
	UpstreamModel            string  `json:"upstream_model"`
	UpstreamEndpoint         string  `json:"upstream_endpoint,omitempty"`
	Model                    string  `json:"model,omitempty"`
	ServiceTier              string  `json:"service_tier,omitempty"`
	ReasoningEffort          string  `json:"reasoning_effort,omitempty"`
	BillingModel             string  `json:"billing_model,omitempty"`
	OriginalModel            string  `json:"original_model,omitempty"`
	MappedModel              string  `json:"mapped_model,omitempty"`
	BillingModelSource       string  `json:"billing_model_source,omitempty"`
	ModelMappingChain        string  `json:"model_mapping_chain,omitempty"`
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheCreationTokens      int     `json:"cache_creation_tokens"`
	CacheReadTokens          int     `json:"cache_read_tokens"`
	ImageInputTokens         int     `json:"image_input_tokens,omitempty"`
	ImageOutputTokens        int     `json:"image_output_tokens,omitempty"`
	ImageCount               int     `json:"image_count,omitempty"`
	VideoCount               int     `json:"video_count,omitempty"`
	ForceCacheBilling        bool    `json:"force_cache_billing,omitempty"`
	CyberBlocked             bool    `json:"cyber_blocked,omitempty"`
	LongContextThreshold     int     `json:"long_context_threshold,omitempty"`
	LongContextMultiplier    float64 `json:"long_context_multiplier,omitempty"`
	FirstTokenMilliseconds   int64   `json:"first_token_milliseconds"`
	DurationMilliseconds     int64   `json:"duration_milliseconds"`
	TerminalStatus           string  `json:"terminal_status"`
	RequestWasClientStream   bool    `json:"request_was_client_stream"`
	ResponseWasPartiallySent bool    `json:"response_was_partially_sent"`
	InboundEndpoint          string  `json:"inbound_endpoint,omitempty"`
	UserAgent                string  `json:"user_agent,omitempty"`
	ClientIP                 string  `json:"client_ip,omitempty"`
	SessionID                string  `json:"session_id,omitempty"`
	RequestPayloadHash       string  `json:"request_payload_hash,omitempty"`
}

type Event struct {
	Sequence uint64        `json:"sequence"`
	Kind     EventKind     `json:"kind"`
	Result   *Result       `json:"result,omitempty"`
	Usage    *UsageFacts   `json:"usage,omitempty"`
	Error    *RuntimeError `json:"error,omitempty"`
}

var (
	ErrMissingContractVersion = errors.New("runtime contract version is required")
	ErrUnknownContractVersion = errors.New("runtime contract version is unsupported")
	ErrMissingRequestID       = errors.New("runtime request id is required")
	ErrInvalidPlatformID      = errors.New("runtime platform id must be positive")
	ErrMissingEndpoint        = errors.New("runtime endpoint is required")
)

// ErrUnsupportedContractVersion is kept as a descriptive alias for callers
// that use the longer name while retaining one stable sentinel value.
var ErrUnsupportedContractVersion = ErrUnknownContractVersion

func (r Request) Validate() error {
	switch r.ContractVersion {
	case ContractVersionV1:
		// supported
	case "":
		return ErrMissingContractVersion
	default:
		return fmt.Errorf("%w: %s", ErrUnknownContractVersion, r.ContractVersion)
	}
	if r.RequestID == "" {
		return ErrMissingRequestID
	}
	if r.Platform.ID <= 0 {
		return ErrInvalidPlatformID
	}
	if r.Endpoint == "" {
		return ErrMissingEndpoint
	}
	return nil
}

func (r Request) Clone() Request {
	r.Payload = cloneBytes(r.Payload)
	r.Headers = cloneStringMap(r.Headers)
	r.Platform.EndpointCapabilities = append([]string(nil), r.Platform.EndpointCapabilities...)
	return r
}

func (r Result) Clone() Result {
	r.Body = cloneBytes(r.Body)
	r.ResponseHeaders = cloneHeaderMap(r.ResponseHeaders)
	return r
}

func (e Event) Clone() Event {
	if e.Result != nil {
		result := e.Result.Clone()
		e.Result = &result
	}
	if e.Usage != nil {
		usage := *e.Usage
		e.Usage = &usage
	}
	if e.Error != nil {
		runtimeErr := e.Error.Clone()
		e.Error = &runtimeErr
	}
	return e
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneHeaderMap(value map[string][]string) map[string][]string {
	if value == nil {
		return nil
	}
	clone := make(map[string][]string, len(value))
	for key, items := range value {
		clone[key] = append([]string(nil), items...)
	}
	return clone
}
