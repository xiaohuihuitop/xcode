package gatewayruntime

import "net/http"

type Endpoint string

const (
	EndpointMessages             Endpoint = "messages"
	EndpointChatCompletions      Endpoint = "chat_completions"
	EndpointResponses            Endpoint = "responses"
	EndpointGeminiNative         Endpoint = "gemini_native"
	EndpointEmbeddings           Endpoint = "embeddings"
	EndpointAlphaSearch          Endpoint = "alpha_search"
	EndpointImages               Endpoint = "images"
	EndpointVideos               Endpoint = "videos"
	EndpointCountTokens          Endpoint = "count_tokens"
	EndpointResponsesInputTokens Endpoint = "responses_input_tokens"
	EndpointLive                 Endpoint = "live"
	EndpointWebSocket            Endpoint = "websocket"
)

type PlatformRoute struct {
	ID                   int64
	Code                 string
	Adapter              string
	RequestedModel       string
	UpstreamModel        string
	EndpointCapabilities []string
	MatchPriority        int
}

type RequestMetadata struct {
	// APIKeyID is a scalar correlation value copied by ingress. The runtime
	// never receives the API key entity or any product-side credential state.
	APIKeyID int64
	// UserID is a scalar owner identifier used only for long-lived media task
	// bindings; the runtime never receives the user entity.
	UserID             int64
	Headers            map[string]string
	UserAgent          string
	ClientIP           string
	SessionID          string
	RequestPayloadHash string
}

type Request struct {
	RequestID       string
	PlatformID      int64
	PlatformCode    string
	Adapter         string
	Endpoint        Endpoint
	InboundEndpoint string
	RequestedModel  string
	UpstreamModel   string
	Stream          bool
	Payload         []byte
	Metadata        RequestMetadata
	Exchange        HTTPExchange
}

type Result struct {
	StatusCode       int
	AccountID        int64
	UpstreamEndpoint string
	UpstreamModel    string
	Response         Response
	Usage            UsageFacts
}

type Response struct {
	Header   http.Header
	Body     []byte
	Streamed bool
}

type UsageFacts struct {
	Adapter                  string
	Model                    string
	RequestedModel           string
	InputTokens              int
	OutputTokens             int
	CacheCreationTokens      int
	CacheReadTokens          int
	ImageInputTokens         int
	ImageOutputTokens        int
	ImageCount               int
	VideoCount               int
	FirstTokenMilliseconds   int64
	DurationMilliseconds     int64
	AccountID                int64
	UpstreamEndpoint         string
	UpstreamModel            string
	ServiceTier              string
	ReasoningEffort          string
	BillingModel             string
	OriginalModel            string
	MappedModel              string
	BillingModelSource       string
	ModelMappingChain        string
	ForceCacheBilling        bool
	CyberBlocked             bool
	LongContextThreshold     int
	LongContextMultiplier    float64
	InboundEndpoint          string
	UserAgent                string
	ClientIP                 string
	SessionID                string
	RequestPayloadHash       string
	TerminalStatus           string
	RequestWasClientStream   bool
	ResponseWasPartiallySent bool
}

type UsageEvent struct {
	RequestID string
	Success   bool
	Facts     UsageFacts
	Error     *RuntimeError
}

type TokenCountResult struct {
	InputTokens int
}

type AccountProbeRequest struct {
	AccountID int64
}

type AccountProbeResult struct {
	Healthy              bool
	EndpointCapabilities []string
	ErrorCategory        ErrorCategory
}

type PricingRequest struct {
	Model                     string
	Adapter                   string
	Tokens                    UsageFacts
	ServiceTier               string
	RequestCount              int
	SizeTier                  string
	LongContextBillingEnabled *bool
}

type PricingQuote struct {
	InputCost   float64
	OutputCost  float64
	TotalCost   float64
	BillingMode string
}
