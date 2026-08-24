package service

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

// codexUpstreamMinVersion 上游 /backend-api/codex 接受的最低 version 头：
// 若请求携带 version 且低于该值，上游直接 404（issue #3901，2026-07 实测）。
const codexUpstreamMinVersion = "0.144.0"

const codexClientVersionMaxLen = 64

var codexClientVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z.]+)?$`)

func NormalizeCodexClientVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > codexClientVersionMaxLen || !codexClientVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

func buildCodexCLIUserAgent(version string) string {
	if version = NormalizeCodexClientVersion(version); version == "" {
		return codexCLIUserAgent
	}
	return openai.CodexDefaultOriginator + "/" + version + codexCLIUserAgentSuffix
}

var codexIdentityEnforcement = func() *atomic.Bool {
	value := &atomic.Bool{}
	value.Store(true)
	return value
}()

func SetCodexIdentityEnforcementEnabled(enabled bool) {
	codexIdentityEnforcement.Store(enabled)
}

var (
	codexCanonicalUAMu       sync.RWMutex
	codexCanonicalUAResolver func() string
)

func SetCodexCanonicalUserAgentResolver(resolver func() string) {
	codexCanonicalUAMu.Lock()
	defer codexCanonicalUAMu.Unlock()
	codexCanonicalUAResolver = resolver
}

func codexCanonicalUserAgent() string {
	codexCanonicalUAMu.RLock()
	resolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	if resolver != nil {
		if userAgent := strings.TrimSpace(resolver()); userAgent != "" {
			return userAgent
		}
	}
	return codexCLIUserAgent
}

type codexOutboundIdentity struct {
	userAgent  string
	originator string
	version    string
}

func resolveCodexOutboundIdentity(candidateUserAgent string) codexOutboundIdentity {
	canonical := codexCanonicalUserAgent()
	userAgent := strings.TrimSpace(candidateUserAgent)
	if userAgent == "" {
		userAgent = canonical
	}
	originator, pairedUserAgent, ok := openai.PairCodexClientIdentity(userAgent)
	if !ok {
		if originator, pairedUserAgent, ok = openai.PairCodexClientIdentity(canonical); !ok {
			originator, pairedUserAgent = openai.CodexDefaultOriginator, codexCLIUserAgent
		}
	}
	version := codexClientVersionFromUA(canonical)
	if rebuilt := openai.SetCodexUserAgentVersion(pairedUserAgent, version); rebuilt != "" {
		pairedUserAgent = rebuilt
	}
	return codexOutboundIdentity{userAgent: pairedUserAgent, originator: originator, version: version}
}

func codexClientVersionFromUA(userAgent string) string {
	version := NormalizeCodexClientVersion(openai.CodexUserAgentVersion(userAgent))
	if version == "" || CompareVersions(version, codexUpstreamMinVersion) < 0 {
		return codexCLIVersion
	}
	return version
}

func CodexCanonicalUserAgent() string {
	return resolveCodexOutboundIdentity("").userAgent
}

func CodexCanonicalAuthIdentity() (userAgent, originator string) {
	identity := resolveCodexOutboundIdentity("")
	return identity.userAgent, identity.originator
}

func ApplyCodexCanonicalAuthIdentity(headers http.Header) {
	if headers == nil {
		return
	}
	userAgent, originator := CodexCanonicalAuthIdentity()
	headers.Set("user-agent", userAgent)
	headers.Set("originator", originator)
}

func CodexCanonicalClientVersion() string {
	return resolveCodexOutboundIdentity("").version
}

// ensureCodexIdentityHeaders 补齐 OAuth（ChatGPT 内部接口）出站请求所需的 Codex 身份头。
// 已有 User-Agent 与 version 保持不变，交给紧随其后的 enforceCodexIdentityHeaders
// 做官方身份配对与最低版本校正。
func ensureCodexIdentityHeaders(h http.Header) {
	if h == nil {
		return
	}
	identity := resolveCodexOutboundIdentity("")
	if strings.TrimSpace(h.Get("user-agent")) == "" {
		h.Set("user-agent", identity.userAgent)
	}
	if strings.TrimSpace(h.Get("originator")) == "" {
		h.Set("originator", identity.originator)
	}
	if strings.TrimSpace(h.Get("version")) == "" {
		h.Set("version", identity.version)
	}
	h.Set("OpenAI-Beta", "responses=experimental")
}

// applyOpenAICodexProbeHeaders 为合成探测请求补齐 Codex 身份和引擎指纹。
func applyOpenAICodexProbeHeaders(h http.Header) {
	if h == nil {
		return
	}
	ensureCodexIdentityHeaders(h)
	h.Set("X-Codex-Window-ID", uuid.NewString())
}

// enforceCodexIdentityHeaders 收口 OAuth（ChatGPT 内部接口）出站请求的客户端身份头。
// 上游要求 originator 与 User-Agent 首段配套且为官方客户端标识，version 头（若携带）
// 不低于 0.144.0，任一不满足即 404（issue #3901）。以最终 User-Agent 为准推导配套
// originator；推导不出官方身份（第三方 UA / UA 缺失）时整体回退为默认 Codex CLI 身份。
//
// 仅对携带 originator 的请求生效；需要从缺失身份头恢复的调用方应先调用
// ensureCodexIdentityHeaders。
// 必须在所有 User-Agent 改写（自定义 UA / ForceCodexCLI / 浏览器 UA 兜底）之后调用。
func enforceCodexIdentityHeaders(h http.Header) {
	enforceCodexIdentityHeadersWithUA(h, "")
}

func enforceCodexIdentityHeadersWithUA(h http.Header, overrideUserAgent string) {
	if h == nil || h.Get("originator") == "" {
		return
	}
	if !codexIdentityEnforcement.Load() {
		pairCodexIdentityHeaders(h)
		return
	}
	identity := resolveCodexOutboundIdentity(overrideUserAgent)
	h.Set("user-agent", identity.userAgent)
	h.Set("originator", identity.originator)
	h.Set("version", identity.version)
}

func pairCodexIdentityHeaders(h http.Header) {
	originator, pairedUA, ok := openai.PairCodexClientIdentity(h.Get("user-agent"))
	if !ok {
		identity := resolveCodexOutboundIdentity("")
		originator, pairedUA = identity.originator, identity.userAgent
		h.Set("version", identity.version)
	}
	h.Set("user-agent", pairedUA)
	h.Set("originator", originator)
	if v := strings.TrimSpace(h.Get("version")); v != "" && CompareVersions(v, codexUpstreamMinVersion) < 0 {
		h.Set("version", resolveCodexOutboundIdentity("").version)
	}
}
