package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const gatewayCompatibilityMetricsLogInterval = 1024

var gatewayCompatibilityMetricsLogCounter atomic.Uint64

type authorizedPlatformModelLister interface {
	ListAuthorizedModels(context.Context, []int64) ([]string, error)
}

// GatewayHandler handles API gateway requests
type GatewayHandler struct {
	gatewayService            *service.GatewayService
	openAIGatewayService      *service.OpenAIGatewayService
	geminiCompatService       *service.GeminiMessagesCompatService
	antigravityGatewayService *service.AntigravityGatewayService
	userService               *service.UserService
	billingCacheService       *service.BillingCacheService
	usageService              *service.UsageService
	apiKeyService             *service.APIKeyService
	usageRecordWorkerPool     *service.UsageRecordWorkerPool
	errorPassthroughService   *service.ErrorPassthroughService
	contentModerationService  *service.ContentModerationService
	securityAuditCoordinator  *securityaudit.Coordinator
	concurrencyHelper         *ConcurrencyHelper
	userMsgQueueHelper        *UserMsgQueueHelper
	maxAccountSwitches        int
	maxAccountSwitchesGemini  int
	cfg                       *config.Config
	settingService            *service.SettingService
	platformModels            authorizedPlatformModelLister
	applicationGateway        *applicationgateway.Gateway
}

// Messages is the single public ApplicationGateway entry for the Claude
// compatible endpoint. Authenticated Gateway execution is owned by the
// sub2APIMessagesExecutor.
func (h *GatewayHandler) Messages(c *gin.Context) {
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointMessages)
}

// SetApplicationGateway installs the single production runtime entrypoint.
// It is kept as a small composition hook so Wire can construct both protocol
// handlers before sharing one adapter instance.
func (h *GatewayHandler) SetApplicationGateway(gateway *applicationgateway.Gateway) {
	if h != nil {
		h.applicationGateway = gateway
	}
}

// NewGatewayHandler creates a new GatewayHandler
func NewGatewayHandler(
	gatewayService *service.GatewayService,
	openAIGatewayService *service.OpenAIGatewayService,
	geminiCompatService *service.GeminiMessagesCompatService,
	antigravityGatewayService *service.AntigravityGatewayService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	userMsgQueueService *service.UserMessageQueueService,
	cfg *config.Config,
	settingService *service.SettingService,
	platformModels authorizedPlatformModelLister,
) *GatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 10
	maxAccountSwitchesGemini := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
		if cfg.Gateway.MaxAccountSwitchesGemini > 0 {
			maxAccountSwitchesGemini = cfg.Gateway.MaxAccountSwitchesGemini
		}
	}

	// 鍒濆鍖栫敤鎴锋秷鎭覆琛岄槦鍒?helper
	var umqHelper *UserMsgQueueHelper
	if userMsgQueueService != nil && cfg != nil {
		umqHelper = NewUserMsgQueueHelper(userMsgQueueService, SSEPingFormatClaude, pingInterval)
	}

	return &GatewayHandler{
		gatewayService:            gatewayService,
		openAIGatewayService:      openAIGatewayService,
		geminiCompatService:       geminiCompatService,
		antigravityGatewayService: antigravityGatewayService,
		userService:               userService,
		billingCacheService:       billingCacheService,
		usageService:              usageService,
		apiKeyService:             apiKeyService,
		usageRecordWorkerPool:     usageRecordWorkerPool,
		errorPassthroughService:   errorPassthroughService,
		contentModerationService:  contentModerationService,
		concurrencyHelper:         NewConcurrencyHelper(concurrencyService, SSEPingFormatClaude, pingInterval),
		userMsgQueueHelper:        umqHelper,
		maxAccountSwitches:        maxAccountSwitches,
		maxAccountSwitchesGemini:  maxAccountSwitchesGemini,
		cfg:                       cfg,
		settingService:            settingService,
		platformModels:            platformModels,
	}
}

// Messages handles Claude API compatible messages endpoint
// POST /v1/messages
func (h *GatewayHandler) legacyMessages(c *gin.Context) {
	(sub2APIMessagesExecutor{
		gatewayHandler: h,
		endpoint:       gatewayruntime.EndpointMessages,
	}).executeMessages(c, nil)
}

func (e sub2APIMessagesExecutor) executeMessages(c *gin.Context, usageSink gatewayruntime.UsageSink) {
	h := e.gatewayHandler
	if h == nil {
		return
	}
	// 浠巆ontext鑾峰彇apiKey鍜寀ser锛圓piKeyAuth涓棿浠跺凡璁剧疆锛?
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
	)
	defer h.maybeLogCompatibilityFallbackMetrics(reqLog)

	// 璇诲彇璇锋眰浣?
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	body = parsedReq.Body.Bytes()
	reqModel := parsedReq.Model
	reqStream := parsedReq.Stream
	ensureModelTargetPlatform(c, reqModel)
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	// 瑙ｆ瀽娓犻亾绾фā鍨嬫槧灏?
	modelMapping := h.gatewayService.ResolvePlatformModelMapping(c.Request.Context(), reqModel)

	// 璁剧疆 max_tokens=1 + haiku 鎺㈡祴璇锋眰鏍囪瘑鍒?context 涓?
	// 蹇呴』鍦?SetClaudeCodeClientContext 涔嬪墠璁剧疆锛屽洜涓?ClaudeCodeValidator 闇€瑕佽鍙栨鏍囪瘑杩涜缁曡繃鍒ゆ柇
	if isMaxTokensOneHaikuRequest(reqModel, parsedReq.MaxTokens) {
		ctx := service.WithIsMaxTokensOneHaikuRequest(c.Request.Context(), true, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)
	}

	// 妫€鏌ユ槸鍚︿负 Claude Code 瀹㈡埛绔紝璁剧疆鍒?context 涓紙澶嶇敤宸茶В鏋愯姹傦紝閬垮厤浜屾鍙嶅簭鍒楀寲锛夈€?
	SetClaudeCodeClientContext(c, body, parsedReq)
	isClaudeCodeClient := service.IsClaudeCodeClient(c.Request.Context())

	// 鐗堟湰妫€鏌ワ細浠呭 Claude Code 瀹㈡埛绔紝鎷掔粷浣庝簬鏈€浣庣増鏈殑璇锋眰
	if !h.checkClaudeCodeVersion(c) {
		return
	}

	// 鍦ㄨ姹備笂涓嬫枃涓褰?thinking 鐘舵€侊紝渚?Antigravity 鏈€缁堟ā鍨?key 鎺ㄥ/妯″瀷缁村害闄愭祦浣跨敤
	c.Request = c.Request.WithContext(service.WithThinkingEnabled(c.Request.Context(), parsedReq.ThinkingEnabled, h.metadataBridgeEnabled()))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 楠岃瘉 model 蹇呭～
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if !modelTargetPlatformResolved(c, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by any authorized platform")
		return
	}

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.anthropicSecurityAuditError(c, decision)
		return
	}

	// Track if we've started streaming (for error handling)
	streamStarted := false

	// 缁戝畾閿欒閫忎紶鏈嶅姟锛屽厑璁?service 灞傚湪闈?failover 閿欒鍦烘櫙澶嶇敤瑙勫垯銆?
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	// 鑾峰彇璁㈤槄淇℃伅锛堝彲鑳戒负nil锛? 鎻愬墠鑾峰彇鐢ㄤ簬鍚庣画妫€鏌?
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	// 1. 棣栧厛鑾峰彇鐢ㄦ埛骞跺彂妲戒綅
	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	// 鍦ㄨ姹傜粨鏉熸垨 Context 鍙栨秷鏃剁‘淇濋噴鏀炬Ы浣嶏紝閬垮厤瀹㈡埛绔柇寮€閫犳垚娉勬紡
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. 銆愭柊澧炪€慦ait鍚庝簩娆℃鏌ヤ綑棰?璁㈤槄
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gateway.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 璁剧疆璇锋眰鎵€灞炲垎缁?ID锛堢敤浜庢笭閬撶骇鍔熻兘鍒ゆ柇锛屽 WebSearch 妯℃嫙锛?
	parsedReq.PlatformID = service.PlatformSchedulingID(c.Request.Context())

	// 璁＄畻绮樻€т細璇漢ash
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)

	// [DEBUG-STICKY] 鎵撳嵃浼氳瘽 hash 鐢熸垚缁撴灉
	reqLog.Info("sticky.session_hash_generated",
		zap.String("session_hash", sessionHash),
		zap.String("metadata_user_id_raw", parsedReq.MetadataUserID),
	)

	// 鑾峰彇骞冲彴锛氫紭鍏堜娇鐢ㄥ己鍒跺钩鍙帮紙/antigravity 璺敱锛夛紝鍏舵浣跨敤 composite 瑙ｆ瀽鍑虹殑鐩爣骞冲彴锛屽惁鍒欎娇鐢ㄥ垎缁勫钩鍙?
	platform := ""
	if forcePlatform, ok := middleware2.GetForcePlatformFromContext(c); ok {
		platform = forcePlatform
	} else if resolvedPlatform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
		platform = resolvedPlatform
	}
	sessionKey := sessionHash
	if platform == service.PlatformGemini && sessionHash != "" {
		sessionKey = "gemini:" + sessionHash
	}

	// 鏌ヨ绮樻€т細璇濈粦瀹氱殑璐﹀彿 ID
	var sessionBoundAccountID int64
	if sessionKey != "" {
		sessionBoundAccountID, _ = h.gatewayService.GetCachedSessionAccountID(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey)
		// [DEBUG-STICKY] 鎵撳嵃绮樻€т細璇濇煡璇㈢粨鏋?
		reqLog.Info("sticky.cache_lookup",
			zap.String("session_key", sessionKey),
			zap.Int64("bound_account_id", sessionBoundAccountID),
		)
		if sessionBoundAccountID > 0 {
			prefetchedPlatformNamespaceID := int64(0)
			if service.PlatformSchedulingID(c.Request.Context()) != nil {
				prefetchedPlatformNamespaceID = *service.PlatformSchedulingID(c.Request.Context())
			}
			ctx := service.WithPrefetchedStickySession(c.Request.Context(), sessionBoundAccountID, prefetchedPlatformNamespaceID, h.metadataBridgeEnabled())
			c.Request = c.Request.WithContext(ctx)
		}
	} else {
		reqLog.Info("sticky.no_session_key", zap.String("session_hash", sessionHash))
	}
	// 鍒ゆ柇鏄惁鐪熺殑缁戝畾浜嗙矘鎬т細璇濓細鏈?sessionKey 涓斿凡缁忕粦瀹氬埌鏌愪釜璐﹀彿
	hasBoundSession := sessionKey != "" && sessionBoundAccountID > 0

	if platform == service.PlatformGemini {
		fs := NewFailoverState(h.maxAccountSwitchesGemini, hasBoundSession)

		// 鍗曡处鍙峰垎缁勬彁鍓嶈缃?SingleAccountRetry 鏍囪锛岃 Service 灞傞娆?503 灏变笉璁炬ā鍨嬮檺娴佹爣璁般€?
		// 閬垮厤鍗曡处鍙峰垎缁勬敹鍒?503 (MODEL_CAPACITY_EXHAUSTED) 鏃惰 29s 闄愭祦锛屽鑷村悗缁姹傝繛缁揩閫熷け璐ャ€?
		if h.gatewayService.IsSingleAntigravityPlatformAccount(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context())) {
			ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
			c.Request = c.Request.WithContext(ctx)
		}

		for {
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, reqModel, fs.FailedAccountIDs, "", int64(0)) // Gemini 涓嶄娇鐢ㄤ細璇濋檺鍒?
			if err != nil {
				if len(fs.FailedAccountIDs) == 0 {
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, service.PlatformGemini)
					if !cls.ModelNotFound {
						markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					}
					reqLog.Warn("gateway.select_account_no_available",
						zap.String("model", reqModel),
						zap.Int64p("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
						zap.String("platform", platform),
						zap.Bool("model_not_found", cls.ModelNotFound),
						zap.Error(err),
					)
					message := cls.Message
					if !cls.ModelNotFound {
						message = "No available accounts: " + err.Error()
					}
					h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
					c.Request = c.Request.WithContext(ctx)
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				default: // FailoverExhausted
					if fs.LastFailoverErr != nil {
						h.handleFailoverExhausted(c, fs.LastFailoverErr, service.PlatformGemini, streamStarted)
					} else {
						h.handleFailoverExhaustedSimple(c, 502, streamStarted)
					}
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)

			// 妫€鏌ヨ姹傛嫤鎴紙棰勭儹璇锋眰銆丼UGGESTION MODE绛夛級
			if account.IsInterceptWarmupEnabled() {
				interceptType := detectInterceptType(body, reqModel, parsedReq.MaxTokens, isClaudeCodeClient)
				if interceptType != InterceptTypeNone {
					if selection.Acquired && selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					if reqStream {
						sendMockInterceptStream(c, reqModel, interceptType)
					} else {
						sendMockInterceptResponse(c, reqModel, interceptType)
					}
					return
				}
			}

			// 3. 鑾峰彇璐﹀彿骞跺彂妲戒綅
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				if selection.WaitPlan == nil {
					markOpsRoutingCapacityLimited(c)
					reqLog.Warn("gateway.select_account_no_slot_no_wait_plan",
						zap.Int64("account_id", account.ID),
						zap.String("model", reqModel),
						zap.String("platform", platform),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
					return
				}
				accountWaitCounted := false
				canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
				if err != nil {
					reqLog.Warn("gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				} else if !canWait {
					reqLog.Info("gateway.account_wait_queue_full",
						zap.Int64("account_id", account.ID),
						zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
					)
					h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
					return
				}
				if err == nil && canWait {
					accountWaitCounted = true
				}
				releaseWait := func() {
					if accountWaitCounted {
						h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
						accountWaitCounted = false
					}
				}

				accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					reqStream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gateway.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					releaseWait()
					h.handleConcurrencyError(c, err, "account", streamStarted)
					return
				}
				// Slot acquired: no longer waiting in queue.
				releaseWait()
				if err := h.gatewayService.BindStickySession(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			// 璐﹀彿妲戒綅/绛夊緟璁℃暟闇€瑕佸湪瓒呮椂鎴栨柇寮€鏃跺畨鍏ㄥ洖鏀?
			accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

			// 杞彂璇锋眰 - 鏍规嵁璐﹀彿骞冲彴鍒嗘祦
			var result *service.ForwardResult
			requestCtx := c.Request.Context()
			if fs.SwitchCount > 0 {
				requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
			}
			// 璁板綍 Forward 鍓嶅凡鍐欏叆瀛楄妭鏁帮紝Forward 鍚庤嫢澧炲姞鍒欒鏄?SSE 鍐呭宸插彂锛岀姝?failover
			writerSizeBeforeForward := c.Writer.Size()
			if account.Platform == service.PlatformAntigravity {
				result, err = h.antigravityGatewayService.ForwardGemini(
					requestCtx,
					c,
					account,
					reqModel,
					"generateContent",
					reqStream,
					body,
					hasBoundSession,
					service.WithForwardGeminiSession(derefPlatformID(service.PlatformSchedulingID(c.Request.Context())), sessionKey),
				)
			} else {
				result, err = h.geminiCompatService.Forward(requestCtx, c, account, body)
			}
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			if err != nil {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					// 娴佸紡鍐呭宸插啓鍏ュ鎴风锛屾棤娉曟挙閿€锛岀姝?failover 浠ラ槻姝㈡祦鎷兼帴鑵愬寲
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, service.PlatformGemini, true)
						return
					}
					action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
					switch action {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						h.handleFailoverExhausted(c, fs.LastFailoverErr, service.PlatformGemini, streamStarted)
						return
					case FailoverCanceled:
						failoverClientGone(c)
						return
					}
				}
				upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				forwardFailedFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.String("account_name", account.Name),
					zap.String("account_platform", account.Platform),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				if account.Proxy != nil {
					forwardFailedFields = append(forwardFailedFields,
						zap.Int64("proxy_id", account.Proxy.ID),
						zap.String("proxy_name", account.Proxy.Name),
						zap.String("proxy_host", account.Proxy.Host),
						zap.Int("proxy_port", account.Proxy.Port),
					)
				} else if account.ProxyID != nil {
					forwardFailedFields = append(forwardFailedFields, zap.Int64p("proxy_id", account.ProxyID))
				}
				reqLog.Error("gateway.forward_failed", forwardFailedFields...)
				return
			}

			// RPM 璁℃暟閫掑锛團orward 鎴愬姛鍚庯級
			// 娉ㄦ剰锛歍OCTOU 绔炴€佹槸宸茬煡涓斿彲鎺ュ彈鐨勮璁℃潈琛★紝涓?WindowCost 涓€鑷寸殑 soft-limit 妯″紡銆?
			// 鍦ㄩ珮骞跺彂涓嬪彲鑳界煭鏆傝秴鍑?RPM 闄愬埗锛屼絾涓嶄細瀵艰嚧璇锋眰澶辫触銆?
			if account.IsAnthropicOAuthOrSetupToken() && account.GetBaseRPM() > 0 {
				if err := h.gatewayService.IncrementAccountRPM(c.Request.Context(), account.ID); err != nil {
					reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 鎹曡幏璇锋眰淇℃伅锛堢敤浜庡紓姝ヨ褰曪紝閬垮厤鍦?goroutine 涓闂?gin.Context锛?
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

			if result.ReasoningEffort == nil {
				result.ReasoningEffort = service.NormalizeClaudeOutputEffort(parsedReq.OutputEffort)
			}
			// 鍥戒骇妯″瀷 thinking-enabled 榛樿 effort 濉厖锛欿imi/GLM/MiniMax 杩欎簺涓嶆敮鎸?effort 妗ｄ綅鐨?
			// passback-required 涓婃父锛屼粎瑕?thinking 鍚敤涓?OutputEffort 鏈槑纭紶閫掓椂锛屽湪 usage_log 鍐?"high"
			// 閬垮厤璇ュ瓧娈甸暱鏈熶负 NULL锛堣瑙?DefaultEffortForThinkingEnabled 鏂囨。锛夈€?
			if result.ReasoningEffort == nil && parsedReq.ThinkingEnabled {
				protocolModel := result.UpstreamModel
				if protocolModel == "" {
					protocolModel = result.Model
				}
				result.ReasoningEffort = service.DefaultEffortForThinkingEnabled(protocolModel)
			}

			// 浣跨敤閲忚褰曢€氳繃鏈夌晫 worker 姹犳彁浜わ紝閬垮厤璇锋眰鐑矾寰勫垱寤烘棤鐣?goroutine銆?
			// ForceCacheBilling 鎻愬墠鎷嶆垚鏍囬噺锛岄伩鍏?worker 闂寘淇濇椿 failover 鐘舵€侀噷鐨勫搷搴斾綋銆?
			forceCacheBilling := fs.ForceCacheBilling
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			sessionID := service.ExtractClientSessionID(c)
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := recordGatewayExecutorUsage(ctx, usageSink, h.gatewayService, &service.RecordUsageInput{
					Result:                  result,
					QuotaPlatform:           quotaPlatform,
					APIKey:                  apiKey,
					User:                    apiKey.User,
					Account:                 account,
					Subscription:            subscription,
					InboundEndpoint:         inboundEndpoint,
					UpstreamEndpoint:        upstreamEndpoint,
					UserAgent:               userAgent,
					IPAddress:               clientIP,
					SessionID:               sessionID,
					RequestPayloadHash:      requestPayloadHash,
					ForceCacheBilling:       forceCacheBilling,
					APIKeyService:           h.apiKeyService,
					ModelRoutingUsageFields: clientRequestedUsageFields(c, modelMapping, reqModel, result.UpstreamModel),
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.gateway.messages"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", apiKey.ID),
						zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("gateway.record_usage_failed", zap.Error(err))
				}
			})
			return
		}
	}

	currentAPIKey := apiKey
	currentSubscription := subscription

	// 鍗曡处鍙峰垎缁勬彁鍓嶈缃?SingleAccountRetry 鏍囪锛岃 Service 灞傞娆?503 灏变笉璁炬ā鍨嬮檺娴佹爣璁般€?
	// 閬垮厤鍗曡处鍙峰垎缁勬敹鍒?503 (MODEL_CAPACITY_EXHAUSTED) 鏃惰 29s 闄愭祦锛屽鑷村悗缁姹傝繛缁揩閫熷け璐ャ€?
	if h.gatewayService.IsSingleAntigravityPlatformAccount(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context())) {
		ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)
	}

	{
		fs := NewFailoverState(h.maxAccountSwitches, hasBoundSession)

		for {
			attemptParsedReq, err := parsedReq.CloneForBody(body)
			if err != nil {
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
				return
			}

			// 閫夋嫨鏀寔璇ユā鍨嬬殑璐﹀彿
			reqLog.Info("sticky.selecting_account",
				zap.String("session_key", sessionKey),
				zap.Int64("sticky_bound_account_id", sessionBoundAccountID),
				zap.Bool("has_bound_session", hasBoundSession),
				zap.Int("failed_account_count", len(fs.FailedAccountIDs)),
			)
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, reqModel, fs.FailedAccountIDs, parsedReq.MetadataUserID, subject.UserID)
			if err != nil {
				if len(fs.FailedAccountIDs) == 0 {
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, currentAPIKey, reqModel, reqModel, platform)
					if !cls.ModelNotFound {
						markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					}
					reqLog.Warn("gateway.select_account_no_available",
						zap.String("model", reqModel),
						zap.Int64p("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
						zap.String("platform", platform),
						zap.Bool("model_not_found", cls.ModelNotFound),
						zap.Error(err),
					)
					message := cls.Message
					if !cls.ModelNotFound {
						message = "No available accounts: " + err.Error()
					}
					h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
					c.Request = c.Request.WithContext(ctx)
					continue
				case FailoverCanceled:
					failoverClientGone(c)
					return
				default: // FailoverExhausted
					if fs.LastFailoverErr != nil {
						h.handleFailoverExhausted(c, fs.LastFailoverErr, platform, streamStarted)
					} else {
						h.handleFailoverExhaustedSimple(c, 502, streamStarted)
					}
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)

			// [DEBUG-STICKY] 鎵撳嵃璐﹀彿閫夋嫨缁撴灉
			reqLog.Info("sticky.account_selected",
				zap.Int64("selected_account_id", account.ID),
				zap.String("account_name", account.Name),
				zap.Bool("slot_acquired", selection.Acquired),
				zap.Bool("has_wait_plan", selection.WaitPlan != nil),
				zap.Int64("sticky_bound_account_id", sessionBoundAccountID),
				zap.Bool("sticky_honored", sessionBoundAccountID > 0 && sessionBoundAccountID == account.ID),
			)

			// 妫€鏌ヨ姹傛嫤鎴紙棰勭儹璇锋眰銆丼UGGESTION MODE绛夛級
			if account.IsInterceptWarmupEnabled() {
				interceptType := detectInterceptType(body, reqModel, parsedReq.MaxTokens, isClaudeCodeClient)
				if interceptType != InterceptTypeNone {
					if selection.Acquired && selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					if reqStream {
						sendMockInterceptStream(c, reqModel, interceptType)
					} else {
						sendMockInterceptResponse(c, reqModel, interceptType)
					}
					return
				}
			}

			// 3. 鑾峰彇璐﹀彿骞跺彂妲戒綅
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				if selection.WaitPlan == nil {
					markOpsRoutingCapacityLimited(c)
					reqLog.Warn("gateway.select_account_no_slot_no_wait_plan",
						zap.Int64("account_id", account.ID),
						zap.String("model", reqModel),
						zap.String("platform", platform),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
					return
				}
				accountWaitCounted := false
				canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
				if err != nil {
					reqLog.Warn("gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				} else if !canWait {
					reqLog.Info("gateway.account_wait_queue_full",
						zap.Int64("account_id", account.ID),
						zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
					)
					h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
					return
				}
				if err == nil && canWait {
					accountWaitCounted = true
				}
				releaseWait := func() {
					if accountWaitCounted {
						h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
						accountWaitCounted = false
					}
				}

				accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					reqStream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gateway.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					releaseWait()
					h.handleConcurrencyError(c, err, "account", streamStarted)
					return
				}
				// Slot acquired: no longer waiting in queue.
				releaseWait()
				reqLog.Info("sticky.bind_after_wait",
					zap.String("session_key", sessionKey),
					zap.Int64("account_id", account.ID),
				)
				if err := h.gatewayService.BindStickySession(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			// 璐﹀彿妲戒綅/绛夊緟璁℃暟闇€瑕佸湪瓒呮椂鎴栨柇寮€鏃跺畨鍏ㄥ洖鏀?
			accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

			// ===== 鐢ㄦ埛娑堟伅涓茶闃熷垪 START =====
			var queueRelease func()
			umqMode := h.getUserMsgQueueMode(account, attemptParsedReq)

			switch umqMode {
			case config.UMQModeSerialize:
				// 涓茶妯″紡锛氳幏鍙栭攣 + RPM 寤惰繜 + 閲婃斁锛堝綋鍓嶈涓轰笉鍙橈級
				baseRPM := account.GetBaseRPM()
				release, qErr := h.userMsgQueueHelper.AcquireWithWait(
					c, account.ID, baseRPM, reqStream, &streamStarted,
					h.cfg.Gateway.UserMessageQueue.WaitTimeout(),
					reqLog,
				)
				if qErr != nil {
					// fail-open: 璁板綍 warn锛屼笉闃绘璇锋眰
					reqLog.Warn("gateway.umq_acquire_failed",
						zap.Int64("account_id", account.ID),
						zap.Error(qErr),
					)
				} else {
					queueRelease = release
				}

			case config.UMQModeThrottle:
				// 杞€ч檺閫燂細浠呮柦鍔?RPM 鑷€傚簲寤惰繜锛屼笉闃诲骞跺彂
				baseRPM := account.GetBaseRPM()
				if tErr := h.userMsgQueueHelper.ThrottleWithPing(
					c, account.ID, baseRPM, reqStream, &streamStarted,
					h.cfg.Gateway.UserMessageQueue.WaitTimeout(),
					reqLog,
				); tErr != nil {
					reqLog.Warn("gateway.umq_throttle_failed",
						zap.Int64("account_id", account.ID),
						zap.Error(tErr),
					)
				}

			default:
				if umqMode != "" {
					reqLog.Warn("gateway.umq_unknown_mode",
						zap.String("mode", umqMode),
						zap.Int64("account_id", account.ID),
					)
				}
			}

			// 鐢?wrapReleaseOnDone 纭繚 context 鍙栨秷鏃惰嚜鍔ㄩ噴鏀撅紙浠?serialize 妯″紡鏈?queueRelease锛?
			queueRelease = wrapReleaseOnDone(c.Request.Context(), queueRelease)
			// 娉ㄥ叆鍥炶皟鍒?ParsedRequest锛氫娇鐢ㄥ灞?wrapper 浠ヤ究鎻愬墠娓呯悊 AfterFunc
			attemptParsedReq.OnUpstreamAccepted = queueRelease
			// ===== 鐢ㄦ埛娑堟伅涓茶闃熷垪 END =====

			// 娓犻亾妯″瀷鏄犲皠鍙綔鐢ㄤ簬鏈璐﹀彿灏濊瘯锛岄伩鍏?failover 鍚庢薄鏌撳師濮?ParsedRequest銆?
			if modelMapping.Mapped {
				attemptParsedReq.Model = modelMapping.MappedModel
				if err := attemptParsedReq.ReplaceBody(h.gatewayService.ReplaceModelInBody(attemptParsedReq.Body.Bytes(), modelMapping.MappedModel)); err != nil {
					h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
					return
				}
			}
			// Bedrock CC 鍏煎锛氭竻鐞?body 涓撴湁瀛楁 + 杩囨护 anthropic-beta header锛岄€傜敤浜庢墍鏈夎浆鍙戣矾寰?
			if err := attemptParsedReq.ReplaceBody(h.gatewayService.ApplyBedrockCCCompat(c, attemptParsedReq.Body.Bytes(), attemptParsedReq.Model, account, service.PlatformSchedulingID(c.Request.Context()))); err != nil {
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
				return
			}
			attemptBody := attemptParsedReq.Body.Bytes()

			// 杞彂璇锋眰 - 鏍规嵁璐﹀彿骞冲彴鍒嗘祦
			c.Set("parsed_request", attemptParsedReq)
			var result *service.ForwardResult
			requestCtx := c.Request.Context()
			if fs.SwitchCount > 0 {
				requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
			}
			if fs.ForceCacheBilling {
				requestCtx = service.WithForceCacheBilling(requestCtx)
			}
			// 璁板綍 Forward 鍓嶅凡鍐欏叆瀛楄妭鏁帮紝Forward 鍚庤嫢澧炲姞鍒欒鏄?SSE 鍐呭宸插彂锛岀姝?failover
			writerSizeBeforeForward := c.Writer.Size()
			if account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey {
				result, err = h.antigravityGatewayService.Forward(requestCtx, c, account, attemptBody, hasBoundSession)
			} else {
				result, err = h.gatewayService.Forward(requestCtx, c, account, attemptParsedReq)
			}

			// 鍏滃簳閲婃斁涓茶閿侊紙姝ｅ父鎯呭喌宸查€氳繃鍥炶皟鎻愬墠閲婃斁锛?
			if queueRelease != nil {
				queueRelease()
			}
			// 娓呯悊鍥炶皟寮曠敤锛岄槻姝?failover 閲嶈瘯鏃舵棫鍥炶皟琚敊璇皟鐢?
			attemptParsedReq.OnUpstreamAccepted = nil

			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}

			// 鎻愪氦 usage 璁板綍銆傛垚鍔熻矾寰勪笌"娴佷腑鏂絾 Forward 宸茶娴嬪埌 usage 鐨勯儴鍒嗙粨鏋?
			// 閿欒璺緞鍏辩敤锛氬悗鑰呰嫢涓嶅叆璐︼紝涓婃父宸茶閲忕殑璇锋眰浼氬畬鍏ㄦ紡璁版紡璁¤垂锛?5148锛夈€?
			submitForwardUsage := func(result *service.ForwardResult) {
				// 鎹曡幏璇锋眰淇℃伅锛堢敤浜庡紓姝ヨ褰曪紝閬垮厤鍦?goroutine 涓闂?gin.Context锛?
				userAgent := c.GetHeader("User-Agent")
				clientIP := ip.GetClientIP(c)
				// Forward 鍐呴儴鍙兘缁х画鏀瑰啓 body锛寀sage 鍘婚噸鎸囩汗蹇呴』浣跨敤鏈€缁堜笂娓告帴鍙楃殑褰撳墠 body銆?
				requestPayloadHash := service.HashUsageRequestPayload(attemptParsedReq.Body.Bytes())
				inboundEndpoint := GetInboundEndpoint(c)
				upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

				if result.ReasoningEffort == nil {
					result.ReasoningEffort = service.NormalizeClaudeOutputEffort(attemptParsedReq.OutputEffort)
				}
				// 鍚屼笂锛堥噸璇曡矾寰勪腑鐨勫绉板～鍏咃級銆傝瑙侀潪閲嶈瘯璺緞鍚屽悕娉ㄩ噴銆?
				if result.ReasoningEffort == nil && attemptParsedReq.ThinkingEnabled {
					protocolModel := result.UpstreamModel
					if protocolModel == "" {
						protocolModel = result.Model
					}
					result.ReasoningEffort = service.DefaultEffortForThinkingEnabled(protocolModel)
				}

				// 浣跨敤閲忚褰曢€氳繃鏈夌晫 worker 姹犳彁浜わ紝閬垮厤璇锋眰鐑矾寰勫垱寤烘棤鐣?goroutine銆?
				// ForceCacheBilling 鎻愬墠鎷嶆垚鏍囬噺锛岄伩鍏?worker 闂寘淇濇椿 failover 鐘舵€侀噷鐨勫搷搴斾綋銆?
				forceCacheBilling := fs.ForceCacheBilling
				quotaPlatform := service.QuotaPlatform(c.Request.Context(), currentAPIKey)
				sessionID := service.ExtractClientSessionID(c)
				h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
					if err := recordGatewayExecutorUsage(ctx, usageSink, h.gatewayService, &service.RecordUsageInput{
						Result:                  result,
						QuotaPlatform:           quotaPlatform,
						APIKey:                  currentAPIKey,
						User:                    currentAPIKey.User,
						Account:                 account,
						Subscription:            currentSubscription,
						InboundEndpoint:         inboundEndpoint,
						UpstreamEndpoint:        upstreamEndpoint,
						UserAgent:               userAgent,
						IPAddress:               clientIP,
						SessionID:               sessionID,
						RequestPayloadHash:      requestPayloadHash,
						ForceCacheBilling:       forceCacheBilling,
						APIKeyService:           h.apiKeyService,
						ModelRoutingUsageFields: clientRequestedUsageFields(c, modelMapping, reqModel, result.UpstreamModel),
					}); err != nil {
						logger.L().With(
							zap.String("component", "handler.gateway.messages"),
							zap.Int64("user_id", subject.UserID),
							zap.Int64("api_key_id", currentAPIKey.ID),
							zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
							zap.String("model", reqModel),
							zap.Int64("account_id", account.ID),
						).Error("gateway.record_usage_failed", zap.Error(err))
					}
				})
			}

			if err != nil {
				// Beta policy block: return 400 immediately, no failover
				var betaBlockedErr *service.BetaBlockedError
				if errors.As(err, &betaBlockedErr) {
					service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
					h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", betaBlockedErr.Message)
					return
				}

				var promptTooLongErr *service.PromptTooLongError
				if errors.As(err, &promptTooLongErr) {
					reqLog.Warn("gateway.prompt_too_long_from_antigravity",
						zap.Any("platform_scheduling_id", service.PlatformSchedulingID(c.Request.Context())),
					)
					_ = h.antigravityGatewayService.WriteMappedClaudeError(c, account, promptTooLongErr.StatusCode, promptTooLongErr.RequestID, promptTooLongErr.Body)
					return
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					// 娴佸紡鍐呭宸插啓鍏ュ鎴风锛屾棤娉曟挙閿€锛岀姝?failover 浠ラ槻姝㈡祦鎷兼帴鑵愬寲
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, account.Platform, true)
						return
					}
					action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
					switch action {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						h.handleFailoverExhausted(c, fs.LastFailoverErr, account.Platform, streamStarted)
						return
					case FailoverCanceled:
						failoverClientGone(c)
						return
					}
				}
				upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				forwardFailedFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.String("account_name", account.Name),
					zap.String("account_platform", account.Platform),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				if account.Proxy != nil {
					forwardFailedFields = append(forwardFailedFields,
						zap.Int64("proxy_id", account.Proxy.ID),
						zap.String("proxy_name", account.Proxy.Name),
						zap.String("proxy_host", account.Proxy.Host),
						zap.Int("proxy_port", account.Proxy.Port),
					)
				} else if account.ProxyID != nil {
					forwardFailedFields = append(forwardFailedFields, zap.Int64p("proxy_id", account.ProxyID))
				}
				reqLog.Error("gateway.forward_failed", forwardFailedFields...)
				// Forward 涓庨敊璇竴璧疯繑鍥炵殑閮ㄥ垎缁撴灉锛氭祦涓柇鍓嶄笂娓稿凡璁￠噺鐨?usage 鐓у父鍏ヨ处锛?
				// 閬垮厤涓婃父宸蹭骇鐢熸秷鑰楃殑璇锋眰瀹屽叏婕忚锛?5148锛夈€俧ailover 閿欒鎭掑畾 result=nil锛?
				// 涓嶄細璧板埌杩欓噷閲嶅璁¤垂銆?
				if result != nil {
					submitForwardUsage(result)
				}
				return
			}

			// RPM 璁℃暟閫掑锛團orward 鎴愬姛鍚庯級
			// 娉ㄦ剰锛歍OCTOU 绔炴€佹槸宸茬煡涓斿彲鎺ュ彈鐨勮璁℃潈琛★紝涓?WindowCost 涓€鑷寸殑 soft-limit 妯″紡銆?
			// 鍦ㄩ珮骞跺彂涓嬪彲鑳界煭鏆傝秴鍑?RPM 闄愬埗锛屼絾涓嶄細瀵艰嚧璇锋眰澶辫触銆?
			if account.IsAnthropicOAuthOrSetupToken() && account.GetBaseRPM() > 0 {
				if err := h.gatewayService.IncrementAccountRPM(c.Request.Context(), account.ID); err != nil {
					reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 缁戝畾绮樻€т細璇濓紙鎴愬姛杞彂鍚庣粦瀹?鍒锋柊锛?
			// - 鏃犵幇鏈夌粦瀹氾紙棣栨璇锋眰锛夛細鍒涘缓缁戝畾
			// - 閫変腑璐﹀彿涓庣矘鎬ц处鍙蜂竴鑷达細鍒锋柊 TTL
			// - 绮樻€ц处鍙峰洜璐熻浇/RPM 琚烦杩囥€侀€変腑浜嗗叾浠栬处鍙凤細涓嶈鐩栧師缁戝畾锛?
			//   涓嬫璇锋眰绮樻€ц处鍙锋仮澶嶅悗浠嶅彲鍛戒腑
			if sessionKey != "" && (sessionBoundAccountID == 0 || sessionBoundAccountID == account.ID) {
				if err := h.gatewayService.BindStickySession(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			submitForwardUsage(result)
			return
		}
	}
}

// Models handles listing available models
// GET /v1/models
// Returns models based on account configurations (model_mapping whitelist)
// Falls back to default models if no whitelist is configured
func (h *GatewayHandler) Models(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key required")
		return
	}
	models, err := h.platformModels.ListAuthorizedModels(c.Request.Context(), apiKey.AllowedPlatformIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	writeModelsList(c, "", models)
}
func writeModelsList(c *gin.Context, platform string, modelIDs []string) {
	if platform == service.PlatformGrok {
		writeGrokModelsList(c, modelIDs)
		return
	}
	models := make([]claude.Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, claude.Model{
			ID:          modelID,
			Type:        "model",
			DisplayName: modelID,
			CreatedAt:   "2024-01-01T00:00:00Z",
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func writeCustomModelsList(c *gin.Context, platform string, modelIDs []string) {
	if platform == service.PlatformOpenAI {
		writeOpenAIModelsList(c, modelIDs)
		return
	}
	writeModelsList(c, platform, modelIDs)
}

type grokReasoningEffortOption struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	Default bool   `json:"default,omitempty"`
}

type grokModelListItem struct {
	xai.Model
	SupportsReasoningEffort bool                        `json:"supportsReasoningEffort,omitempty"`
	ReasoningEffort         string                      `json:"reasoningEffort,omitempty"`
	ReasoningEfforts        []grokReasoningEffortOption `json:"reasoningEfforts,omitempty"`
}

func writeGrokModelsList(c *gin.Context, modelIDs []string) {
	defaults := xai.DefaultModels()
	defaultsByID := make(map[string]xai.Model, len(defaults))
	for _, model := range defaults {
		defaultsByID[model.ID] = model
	}

	models := make([]grokModelListItem, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		model, ok := defaultsByID[modelID]
		if !ok {
			model = xai.Model{
				ID:          modelID,
				Object:      "model",
				OwnedBy:     "xai",
				DisplayName: modelID,
			}
		}
		item := grokModelListItem{Model: model}
		if grokModelSupportsConfigurableReasoning(modelID) {
			item.SupportsReasoningEffort = true
			item.ReasoningEffort = "high"
			item.ReasoningEfforts = []grokReasoningEffortOption{
				{Value: "low", Label: "Low"},
				{Value: "medium", Label: "Medium"},
				{Value: "high", Label: "High", Default: true},
			}
		}
		models = append(models, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func grokModelSupportsConfigurableReasoning(modelID string) bool {
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "grok-4.5", "grok-4.5-latest", "grok", "grok-latest", "grok-build", "grok-build-latest", "grok-build-0.1":
		return true
	default:
		return false
	}
}

func writeOpenAIModelsList(c *gin.Context, modelIDs []string) {
	defaultsByID := make(map[string]openai.Model, len(openai.DefaultModels))
	for _, model := range openai.DefaultModels {
		defaultsByID[model.ID] = model
	}

	models := make([]openai.Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if model, ok := defaultsByID[modelID]; ok {
			models = append(models, model)
			continue
		}
		models = append(models, openai.Model{
			ID:          modelID,
			Object:      "model",
			Created:     1704067200,
			OwnedBy:     "openai",
			Type:        "model",
			DisplayName: modelID,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func customModelsListSource(platform string, availableModels, fallbackModels []string) []string {
	if platform == service.PlatformAnthropic && len(availableModels) > 0 {
		return mergeModelIDs(availableModels, fallbackModels)
	}
	return availableModels
}

func filterModelsByCustomList(availableModels, fallbackModels, selectedModels []string) []string {
	if len(selectedModels) == 0 {
		return availableModels
	}
	source := availableModels
	if len(source) == 0 {
		source = fallbackModels
	}
	if len(source) == 0 {
		return nil
	}

	allowed := make([]string, 0, len(source))
	for _, model := range source {
		model = strings.TrimSpace(model)
		if model != "" {
			allowed = append(allowed, model)
		}
	}

	seen := make(map[string]struct{}, len(selectedModels))
	filtered := make([]string, 0, len(selectedModels))
	for _, model := range selectedModels {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if !customModelsListAllowsModel(allowed, model) {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		filtered = append(filtered, model)
	}
	return filtered
}

func customModelsListAllowsModel(availablePatterns []string, model string) bool {
	for _, pattern := range availablePatterns {
		if pattern == model {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func defaultModelIDsForPlatform(platform string) []string {
	switch platform {
	case service.PlatformOpenAI:
		return openai.DefaultModelIDs()
	case service.PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case service.PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	case service.PlatformAnthropic:
		ids := make([]string, 0, len(claude.DefaultModels)+len(antigravity.DefaultModels()))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		for _, model := range antigravity.DefaultModels() {
			ids = append(ids, model.ID)
		}
		return mergeModelIDs(ids, nil)
	case service.PlatformGrok:
		return xai.DefaultModelIDs()
	case service.PlatformComposite:
		ids := make([]string, 0)
		seen := make(map[string]struct{})
		for _, concretePlatform := range []string{service.PlatformAnthropic, service.PlatformGemini, service.PlatformOpenAI, service.PlatformAntigravity, service.PlatformGrok} {
			for _, id := range defaultModelIDsForPlatform(concretePlatform) {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	default:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

func mergeModelIDs(primary, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, models := range [][]string{primary, secondary} {
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			merged = append(merged, model)
		}
	}
	return merged
}

// AntigravityModels 杩斿洖 Antigravity 鏀寔鐨勫叏閮ㄦā鍨?// GET /antigravity/models
func (h *GatewayHandler) AntigravityModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   antigravity.DefaultModels(),
	})
}

// Usage handles getting account balance and usage statistics for CC Switch integration
// GET /v1/usage
//
// Two modes:
//   - quota_limited: API Key has quota or rate limits configured. Returns key-level limits/usage.
//   - unrestricted:  No key-level limits. Returns subscription or wallet balance info.
func (h *GatewayHandler) Usage(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	ctx := c.Request.Context()

	// 瑙ｆ瀽鍙€夌殑鏃ユ湡鑼冨洿鍙傛暟锛堢敤浜?model_stats 鏌ヨ锛?
	startTime, endTime := h.parseUsageDateRange(c)
	days, ok := parseAPIKeyDailyUsageDays(c.DefaultQuery("days", ""))
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid days, allowed range is 1-90")
		return
	}

	// Best-effort: 鑾峰彇鐢ㄩ噺缁熻锛堟寜褰撳墠 API Key 杩囨护锛夛紝澶辫触涓嶅奖鍝嶅熀纭€鍝嶅簲
	usageData := h.buildUsageData(ctx, apiKey.ID)
	dailyUsage := h.buildAPIKeyDailyUsage(c, subject.UserID, apiKey.ID, days)

	// Best-effort: 鑾峰彇妯″瀷缁熻
	var modelStats any
	if h.usageService != nil {
		if stats, err := h.usageService.GetAPIKeyModelStats(ctx, apiKey.ID, startTime, endTime); err == nil && len(stats) > 0 {
			modelStats = stats
		}
	}

	// 鍒ゆ柇妯″紡: key 鏈夋€婚搴︽垨閫熺巼闄愬埗 鈫?quota_limited锛屽惁鍒?鈫?unrestricted
	isQuotaLimited := apiKey.Quota > 0 || apiKey.HasRateLimits()

	if isQuotaLimited {
		h.usageQuotaLimited(c, ctx, apiKey, usageData, dailyUsage, modelStats)
		return
	}

	h.usageUnrestricted(c, ctx, apiKey, subject, usageData, dailyUsage, modelStats)
}

// parseUsageDateRange 瑙ｆ瀽 start_date / end_date query params锛岄粯璁よ繑鍥炶繎 30 澶╄寖鍥?
func (h *GatewayHandler) parseUsageDateRange(c *gin.Context) (time.Time, time.Time) {
	now := timezone.Now()
	endTime := now
	startTime := now.AddDate(0, 0, -30)

	if s := c.Query("start_date"); s != "" {
		if t, err := timezone.ParseInLocation("2006-01-02", s); err == nil {
			startTime = t
		}
	}
	if s := c.Query("end_date"); s != "" {
		if t, err := timezone.ParseInLocation("2006-01-02", s); err == nil {
			endTime = t.AddDate(0, 0, 1) // half-open range upper bound
		}
	}
	return startTime, endTime
}

// buildUsageData 鏋勫缓 today/total 鐢ㄩ噺鎽樿
func (h *GatewayHandler) buildUsageData(ctx context.Context, apiKeyID int64) gin.H {
	if h.usageService == nil {
		return nil
	}
	dashStats, err := h.usageService.GetAPIKeyDashboardStats(ctx, apiKeyID)
	if err != nil || dashStats == nil {
		return nil
	}
	return gin.H{
		"today": gin.H{
			"requests":              dashStats.TodayRequests,
			"input_tokens":          dashStats.TodayInputTokens,
			"output_tokens":         dashStats.TodayOutputTokens,
			"cache_creation_tokens": dashStats.TodayCacheCreationTokens,
			"cache_read_tokens":     dashStats.TodayCacheReadTokens,
			"total_tokens":          dashStats.TodayTokens,
			"cost":                  dashStats.TodayCost,
			"actual_cost":           dashStats.TodayActualCost,
		},
		"total": gin.H{
			"requests":              dashStats.TotalRequests,
			"input_tokens":          dashStats.TotalInputTokens,
			"output_tokens":         dashStats.TotalOutputTokens,
			"cache_creation_tokens": dashStats.TotalCacheCreationTokens,
			"cache_read_tokens":     dashStats.TotalCacheReadTokens,
			"total_tokens":          dashStats.TotalTokens,
			"cost":                  dashStats.TotalCost,
			"actual_cost":           dashStats.TotalActualCost,
		},
		"average_duration_ms": dashStats.AverageDurationMs,
		"rpm":                 dashStats.Rpm,
		"tpm":                 dashStats.Tpm,
	}
}

func (h *GatewayHandler) buildAPIKeyDailyUsage(c *gin.Context, userID, apiKeyID int64, days int) any {
	if h.usageService == nil {
		return nil
	}
	startTime, endTime := apiKeyDailyUsageRange(days, c.Query("timezone"))
	stats, err := h.usageService.GetAPIKeyDailyUsage(c.Request.Context(), userID, apiKeyID, startTime, endTime)
	if err != nil {
		return nil
	}
	return stats
}

// usageQuotaLimited 澶勭悊 quota_limited 妯″紡鐨勫搷搴?
func (h *GatewayHandler) usageQuotaLimited(c *gin.Context, ctx context.Context, apiKey *service.APIKey, usageData gin.H, dailyUsage any, modelStats any) {
	resp := gin.H{
		"mode":    "quota_limited",
		"isValid": apiKey.Status == service.StatusAPIKeyActive || apiKey.Status == service.StatusAPIKeyQuotaExhausted || apiKey.Status == service.StatusAPIKeyExpired,
		"status":  apiKey.Status,
	}

	// 鎬婚搴︿俊鎭?
	if apiKey.Quota > 0 {
		remaining := apiKey.GetQuotaRemaining()
		resp["quota"] = gin.H{
			"limit":     apiKey.Quota,
			"used":      apiKey.QuotaUsed,
			"remaining": remaining,
			"unit":      "USD",
		}
		resp["remaining"] = remaining
		resp["unit"] = "USD"
	}

	// 閫熺巼闄愬埗淇℃伅锛堜粠 DB 鑾峰彇瀹炴椂鐢ㄩ噺锛?
	if apiKey.HasRateLimits() && h.apiKeyService != nil {
		rateLimitData, err := h.apiKeyService.GetRateLimitData(ctx, apiKey.ID)
		if err == nil && rateLimitData != nil {
			var rateLimits []gin.H
			if apiKey.RateLimit5h > 0 {
				used := rateLimitData.EffectiveUsage5h()
				entry := gin.H{
					"window":       "5h",
					"limit":        apiKey.RateLimit5h,
					"used":         used,
					"remaining":    max(0, apiKey.RateLimit5h-used),
					"window_start": rateLimitData.Window5hStart,
				}
				if rateLimitData.Window5hStart != nil && !service.IsWindowExpired(rateLimitData.Window5hStart, service.RateLimitWindow5h) {
					entry["reset_at"] = rateLimitData.Window5hStart.Add(service.RateLimitWindow5h)
				}
				rateLimits = append(rateLimits, entry)
			}
			if apiKey.RateLimit1d > 0 {
				used := rateLimitData.EffectiveUsage1d()
				entry := gin.H{
					"window":       "1d",
					"limit":        apiKey.RateLimit1d,
					"used":         used,
					"remaining":    max(0, apiKey.RateLimit1d-used),
					"window_start": rateLimitData.Window1dStart,
				}
				if rateLimitData.Window1dStart != nil && !service.IsWindowExpired(rateLimitData.Window1dStart, service.RateLimitWindow1d) {
					entry["reset_at"] = rateLimitData.Window1dStart.Add(service.RateLimitWindow1d)
				}
				rateLimits = append(rateLimits, entry)
			}
			if apiKey.RateLimit7d > 0 {
				used := rateLimitData.EffectiveUsage7d()
				entry := gin.H{
					"window":       "7d",
					"limit":        apiKey.RateLimit7d,
					"used":         used,
					"remaining":    max(0, apiKey.RateLimit7d-used),
					"window_start": rateLimitData.Window7dStart,
				}
				if rateLimitData.Window7dStart != nil && !service.IsWindowExpired(rateLimitData.Window7dStart, service.RateLimitWindow7d) {
					entry["reset_at"] = rateLimitData.Window7dStart.Add(service.RateLimitWindow7d)
				}
				rateLimits = append(rateLimits, entry)
			}
			if len(rateLimits) > 0 {
				resp["rate_limits"] = rateLimits
			}
		}
	}

	// 杩囨湡鏃堕棿
	if apiKey.ExpiresAt != nil {
		resp["expires_at"] = apiKey.ExpiresAt
		resp["days_until_expiry"] = apiKey.GetDaysUntilExpiry()
	}

	if usageData != nil {
		resp["usage"] = usageData
	}
	if dailyUsage != nil {
		resp["daily_usage"] = dailyUsage
	}
	if modelStats != nil {
		resp["model_stats"] = modelStats
	}

	c.JSON(http.StatusOK, resp)
}

// usageUnrestricted 澶勭悊 unrestricted 妯″紡鐨勫搷搴旓紙鍚戝悗鍏煎锛?
func (h *GatewayHandler) usageUnrestricted(c *gin.Context, ctx context.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, usageData gin.H, dailyUsage any, modelStats any) {
	// 璁㈤槄妯″紡
	if subscription, ok := middleware2.GetSubscriptionFromContext(c); ok && subscription != nil {
		resp := gin.H{
			"mode":     "unrestricted",
			"isValid":  true,
			"planName": subscription.PlanNameSnapshot,
			"unit":     "USD",
		}

		remaining := h.calculateSubscriptionRemaining(subscription)
		resp["remaining"] = remaining
		resp["subscription"] = gin.H{
			"daily_usage_usd":     subscription.DailyUsageUSD,
			"weekly_usage_usd":    subscription.WeeklyUsageUSD,
			"monthly_usage_usd":   subscription.MonthlyUsageUSD,
			"daily_limit_usd":     subscription.DailyLimitUSD(),
			"weekly_limit_usd":    subscription.WeeklyLimitUSD(),
			"monthly_limit_usd":   subscription.MonthlyLimitUSD(),
			"weekly_window_start": subscription.WeeklyWindowStart,
			"expires_at":          subscription.ExpiresAt,
		}

		if usageData != nil {
			resp["usage"] = usageData
		}
		if dailyUsage != nil {
			resp["daily_usage"] = dailyUsage
		}
		if modelStats != nil {
			resp["model_stats"] = modelStats
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	// 浣欓妯″紡
	latestUser, err := h.userService.GetByID(ctx, subject.UserID)
	if err != nil {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to get user info")
		return
	}

	resp := gin.H{
		"mode":      "unrestricted",
		"isValid":   true,
		"planName":  "閽卞寘浣欓",
		"remaining": latestUser.Balance,
		"unit":      "USD",
		"balance":   latestUser.Balance,
	}
	if usageData != nil {
		resp["usage"] = usageData
	}
	if dailyUsage != nil {
		resp["daily_usage"] = dailyUsage
	}
	if modelStats != nil {
		resp["model_stats"] = modelStats
	}
	c.JSON(http.StatusOK, resp)
}

// calculateSubscriptionRemaining 璁＄畻璁㈤槄鍓╀綑鍙敤棰濆害
// 閫昏緫锛?// 1. 濡傛灉鏃?鍛?鏈堜换涓€闄愰杈惧埌100%锛岃繑鍥?
// 2. 鍚﹀垯杩斿洖鎵€鏈夊凡閰嶇疆鍛ㄦ湡涓墿浣欓搴︾殑鏈€灏忓€?
func (h *GatewayHandler) calculateSubscriptionRemaining(sub *service.UserSubscription) float64 {
	var remainingValues []float64
	if sub == nil {
		return 0
	}

	// 妫€鏌ユ棩闄愰
	if limit := sub.DailyLimitUSD(); limit != nil && *limit > 0 {
		remaining := *limit - sub.DailyUsageUSD
		if remaining <= 0 {
			return 0
		}
		remainingValues = append(remainingValues, remaining)
	}

	// 妫€鏌ュ懆闄愰
	if limit := sub.WeeklyLimitUSD(); limit != nil && *limit > 0 {
		remaining := *limit - sub.WeeklyUsageUSD
		if remaining <= 0 {
			return 0
		}
		remainingValues = append(remainingValues, remaining)
	}

	// 妫€鏌ユ湀闄愰
	if limit := sub.MonthlyLimitUSD(); limit != nil && *limit > 0 {
		remaining := *limit - sub.MonthlyUsageUSD
		if remaining <= 0 {
			return 0
		}
		remainingValues = append(remainingValues, remaining)
	}

	// 濡傛灉娌℃湁閰嶇疆浠讳綍闄愰锛岃繑鍥?1琛ㄧず鏃犻檺鍒?
	if len(remainingValues) == 0 {
		return -1
	}

	// 杩斿洖鏈€灏忓€?
	min := remainingValues[0]
	for _, v := range remainingValues[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// handleConcurrencyError handles concurrency-related acquire errors.
func (h *GatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool) {
	status, errType, message := concurrencyErrorResponse(err, slotType)
	h.handleStreamingAwareError(c, status, errType, message, streamStarted)
}

func (h *GatewayHandler) handleFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, platform string, streamStarted bool) {
	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody
	if service.IsOpenAISilentRefusalErrorBody(responseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage(), streamStarted)
		return
	}

	// 鍏堟鏌ラ€忎紶瑙勫垯
	if h.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.errorPassthroughService.MatchRule(platform, statusCode, responseBody); rule != nil {
			// 纭畾鍝嶅簲鐘舵€佺爜
			respCode := statusCode
			if !rule.PassthroughCode && rule.ResponseCode != nil {
				respCode = *rule.ResponseCode
			}

			// 纭畾鍝嶅簲娑堟伅
			msg := service.ExtractUpstreamErrorMessage(responseBody)
			if !rule.PassthroughBody && rule.CustomMessage != nil {
				msg = *rule.CustomMessage
			}

			if rule.SkipMonitoring {
				c.Set(service.OpsSkipPassthroughKey, true)
			}

			h.handleStreamingAwareError(c, respCode, "upstream_error", msg, streamStarted)
			return
		}
	}

	// 璁板綍鍘熷涓婃父鐘舵€佺爜锛屼互渚?ops 閿欒鏃ュ織鎹曡幏鐪熷疄鐨勪笂娓搁敊璇?
	upstreamMsg := service.ExtractUpstreamErrorMessage(responseBody)
	service.SetOpsUpstreamError(c, statusCode, upstreamMsg, "")

	// 浣跨敤榛樿鐨勯敊璇槧灏?
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

// handleFailoverExhaustedSimple 绠€鍖栫増鏈紝鐢ㄤ簬娌℃湁鍝嶅簲浣撶殑鎯呭喌
func (h *GatewayHandler) handleFailoverExhaustedSimple(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	service.SetOpsUpstreamError(c, statusCode, errMsg, "")
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

func (h *GatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "overloaded_error", "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}

// handleStreamingAwareError handles errors that may occur after streaming has started
func (h *GatewayHandler) handleStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	if streamStarted {
		// 鍝嶅簲鐘舵€佺爜宸插浐鍖栦负 200锛坧ing/閮ㄥ垎鏁版嵁宸?flush锛夛紝閿欒鍙兘灏卞湴浠?SSE 甯у洖浼犮€?
		// 鏍囪鏈娴佸唴閿欒锛屼緵 ops_error_logger 琛ヨ鈥斺€斿惁鍒欒涓棿浠舵寜 status>=400 閲囬泦锛?
		// 杩欑被鎸傚湪 200 娴佷笂鐨勫け璐ワ紙濡傚苟鍙戦檺娴佸洖閫€锛変笉浼氳繘閿欒鐪嬫澘銆?
		service.MarkOpsStreamError(c, errType, message, status)

		// /v1/responses 鐨勪弗鏍?SDK锛圕odex CLI锛夎姹傜粓姝簨浠跺繀椤诲睘浜?
		// response.completed/failed/incomplete/cancelled 闆嗗悎銆?
		// Anthropic-backed Responses 璺緞鍚屾牱浼氬洜涓洪€氱敤 error 甯ц鎷掋€?
		if inboundIsResponses(c) {
			if writeResponsesFailedSSE(c, errType, message) {
				return
			}
		}
		// Stream already started, send error as SSE event then close
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			// SSE 閿欒浜嬩欢鍥哄畾 schema锛屼娇鐢?Quote 鐩存嫾鍙伩鍏嶉澶?Marshal 鍒嗛厤銆?
			errorEvent := `data: {"type":"error","error":{"type":` + strconv.Quote(errType) + `,"message":` + strconv.Quote(message) + `}}` + "\n\n"
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	h.errorResponse(c, status, errType, message)
}

// ensureForwardErrorResponse 鍦?Forward 杩斿洖閿欒浣嗗皻鏈啓鍝嶅簲鏃惰ˉ鍐欑粺涓€閿欒鍝嶅簲銆?// Writer 宸茶鍐欒繃鏃讹紙ping 宸?flush锛夎蛋 streamStarted 鍒嗘敮锛?// 璁?handleStreamingAwareError 閫氳繃 SSE 鍙戝崗璁悎瑙勭殑缁堟浜嬩欢锛?// 鍚﹀垯涓嬫父鏀跺埌鐨勫氨鏄?silent EOF銆?
func (h *GatewayHandler) ensureForwardErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if service.IsResponseCommitted(c) {
		return false
	}
	if c.Writer.Written() {
		streamStarted = true
	}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
	return true
}

// gatewayForwardErrorAlreadyCommunicated reports whether a Forward implementation
// has already written a complete error response to the client before returning
// an error to the handler.
//
// This is intentionally narrower than "writer size changed": a stream may have
// only emitted keepalive pings or partial data, in which case the handler still
// needs to append a protocol-level terminal error. Non-SSE output from Forward
// is different: service-level helpers such as handleErrorResponse/writeClaudeError
// already wrote the client-visible JSON body, so adding the generic streaming
// fallback would corrupt the response by appending a second `data: ...` frame.
func gatewayForwardErrorAlreadyCommunicated(c *gin.Context, writerSizeBeforeForward int, err error) bool {
	if err == nil || c == nil || c.Writer == nil {
		return false
	}
	if c.Writer.Size() == writerSizeBeforeForward {
		return false
	}

	contentType := strings.ToLower(strings.TrimSpace(c.Writer.Header().Get("Content-Type")))
	if contentType == "" {
		return false
	}
	return !strings.Contains(contentType, "text/event-stream")
}

// checkClaudeCodeVersion 妫€鏌?Claude Code 瀹㈡埛绔増鏈槸鍚︽弧瓒崇増鏈姹?// 浠呭宸茶瘑鍒殑 Claude Code 瀹㈡埛绔墽琛岋紝count_tokens 璺緞闄ゅ
func (h *GatewayHandler) checkClaudeCodeVersion(c *gin.Context) bool {
	ctx := c.Request.Context()
	if !service.IsClaudeCodeClient(ctx) {
		return true
	}

	// 鎺掗櫎 count_tokens 瀛愯矾寰?
	if strings.HasSuffix(c.Request.URL.Path, "/count_tokens") {
		return true
	}

	minVersion, maxVersion := h.settingService.GetClaudeCodeVersionBounds(ctx)
	if minVersion == "" && maxVersion == "" {
		return true
	}

	clientVersion := service.GetClaudeCodeVersion(ctx)
	if clientVersion == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"Unable to determine Claude Code version. Please update Claude Code: npm update -g @anthropic-ai/claude-code")
		return false
	}

	if minVersion != "" && service.CompareVersions(clientVersion, minVersion) < 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Your Claude Code version (%s) is below the minimum required version (%s). Please update: npm update -g @anthropic-ai/claude-code",
				clientVersion, minVersion))
		return false
	}

	if maxVersion != "" && service.CompareVersions(clientVersion, maxVersion) > 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Your Claude Code version (%s) exceeds the maximum allowed version (%s). "+
				"Please downgrade: npm install -g @anthropic-ai/claude-code@%s && "+
				"set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 to prevent auto-upgrade",
				clientVersion, maxVersion, maxVersion))
		return false
	}

	return true
}

// errorResponse 杩斿洖Claude API鏍煎紡鐨勯敊璇搷搴?
func (h *GatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// CountTokens handles token counting endpoint
// POST /v1/messages/count_tokens
// 鐗圭偣锛氭牎楠岃闃?浣欓锛屼絾涓嶈绠楀苟鍙戙€佷笉璁板綍浣跨敤閲?
func (h *GatewayHandler) CountTokens(c *gin.Context) {
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointCountTokens)
}

func (h *GatewayHandler) legacyCountTokens(c *gin.Context) {
	// 浠巆ontext鑾峰彇apiKey鍜寀ser锛圓piKeyAuth涓棿浠跺凡璁剧疆锛?
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	_, ok = middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.count_tokens",
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
	)
	defer h.maybeLogCompatibilityFallbackMetrics(reqLog)

	// 璇诲彇璇锋眰浣?
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	body = parsedReq.Body.Bytes()
	// count_tokens 璧?messages 涓ユ牸鏍￠獙鏃讹紝澶嶇敤宸茶В鏋愯姹傦紝閬垮厤浜屾鍙嶅簭鍒楀寲銆?
	SetClaudeCodeClientContext(c, body, parsedReq)
	ensureModelTargetPlatform(c, parsedReq.Model)
	reqLog = reqLog.With(zap.String("model", parsedReq.Model), zap.Bool("stream", parsedReq.Stream))
	// 鍦ㄨ姹備笂涓嬫枃涓褰?thinking 鐘舵€侊紝渚?Antigravity 鏈€缁堟ā鍨?key 鎺ㄥ/妯″瀷缁村害闄愭祦浣跨敤
	c.Request = c.Request.WithContext(service.WithThinkingEnabled(c.Request.Context(), parsedReq.ThinkingEnabled, h.metadataBridgeEnabled()))

	// 楠岃瘉 model 蹇呭～
	if parsedReq.Model == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if !modelTargetPlatformResolved(c, parsedReq.Model) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by any authorized platform")
		return
	}

	setOpsRequestContext(c, parsedReq.Model, parsedReq.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsedReq.Stream, false)))

	// 鑾峰彇璁㈤槄淇℃伅锛堝彲鑳戒负nil锛?
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	// 鏍￠獙 billing eligibility锛堣闃?浣欓锛?
	// 銆愭敞鎰忋€戜笉璁＄畻骞跺彂锛屼絾闇€瑕佹牎楠岃闃?浣欓
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	// 璁＄畻绮樻€т細璇?hash
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)

	// 閫夋嫨鏀寔璇ユā鍨嬬殑璐﹀彿
	account, err := h.gatewayService.SelectAccountForModel(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionHash, parsedReq.Model)
	if err != nil {
		reqLog.Warn("gateway.count_tokens_select_account_failed", zap.Error(err))
		cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, parsedReq.Model, parsedReq.Model, service.PlatformAnthropic)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		}
		h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
		return
	}
	setOpsSelectedAccount(c, account.ID, account.Platform)

	// 杞彂璇锋眰锛堜笉璁板綍浣跨敤閲忥級
	if err := h.gatewayService.ForwardCountTokens(c.Request.Context(), c, account, parsedReq); err != nil {
		reqLog.Error("gateway.count_tokens_forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		// 閿欒鍝嶅簲宸插湪 ForwardCountTokens 涓鐞?
		return
	}
}

// InterceptType 琛ㄧず璇锋眰鎷︽埅绫诲瀷
type InterceptType int

const (
	InterceptTypeNone InterceptType = iota
	InterceptTypeWarmup
	InterceptTypeSuggestionMode
	InterceptTypeMaxTokensOneHaiku
)

// isHaikuModel 妫€鏌ユā鍨嬪悕绉版槸鍚﹀寘鍚?"haiku"锛堝ぇ灏忓啓涓嶆晱鎰燂級
func isHaikuModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "haiku")
}

// isMaxTokensOneHaikuRequest 妫€鏌ユ槸鍚︿负 max_tokens=1 + haiku 妯″瀷鐨勬帰娴嬭姹?// 杩欑被璇锋眰鐢ㄤ簬 Claude Code 楠岃瘉 API 杩為€氭€э紙娴佸紡/闈炴祦寮忓潎浼氬嚭鐜帮紝濡?cc-switch v3.9.0 璧风殑鍋ュ悍妫€鏌ユ帰娴嬩负娴佸紡锛?// 鏉′欢锛歮ax_tokens == 1 涓?model 鍖呭惈 "haiku"
func isMaxTokensOneHaikuRequest(model string, maxTokens int) bool {
	return maxTokens == 1 && isHaikuModel(model)
}

// detectInterceptType 妫€娴嬭姹傛槸鍚﹂渶瑕佹嫤鎴紝杩斿洖鎷︽埅绫诲瀷
// 鍙傛暟璇存槑锛?//   - body: 璇锋眰浣撳瓧鑺?//   - model: 璇锋眰鐨勬ā鍨嬪悕绉?//   - maxTokens: max_tokens 鍊?//   - isClaudeCodeClient: 鏄惁宸查€氳繃 Claude Code 瀹㈡埛绔牎楠?
func detectInterceptType(body []byte, model string, maxTokens int, isClaudeCodeClient bool) InterceptType {
	// 浼樺厛妫€鏌?max_tokens=1 + haiku 鎺㈡祴璇锋眰锛堟祦寮?闈炴祦寮忓潎閫傜敤锛?
	if isClaudeCodeClient && isMaxTokensOneHaikuRequest(model, maxTokens) {
		return InterceptTypeMaxTokensOneHaiku
	}

	// 蹇€熸鏌ワ細濡傛灉涓嶅寘鍚换浣曞叧閿瓧锛岀洿鎺ヨ繑鍥?
	bodyStr := string(body)
	hasSuggestionMode := strings.Contains(bodyStr, "[SUGGESTION MODE:")
	hasWarmupKeyword := strings.Contains(bodyStr, "title") || strings.Contains(bodyStr, "Warmup")

	if !hasSuggestionMode && !hasWarmupKeyword {
		return InterceptTypeNone
	}

	// 瑙ｆ瀽璇锋眰锛堝彧瑙ｆ瀽涓€娆★級
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return InterceptTypeNone
	}

	// 妫€鏌?SUGGESTION MODE锛堟渶鍚庝竴鏉?user 娑堟伅锛?
	if hasSuggestionMode && len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if lastMsg.Role == "user" && len(lastMsg.Content) > 0 &&
			lastMsg.Content[0].Type == "text" &&
			strings.HasPrefix(lastMsg.Content[0].Text, "[SUGGESTION MODE:") {
			return InterceptTypeSuggestionMode
		}
	}

	// 妫€鏌?Warmup 璇锋眰
	if hasWarmupKeyword {
		// 妫€鏌?messages 涓殑鏍囬鎻愮ず妯″紡
		for _, msg := range req.Messages {
			for _, content := range msg.Content {
				if content.Type == "text" {
					if strings.Contains(content.Text, "Please write a 5-10 word title for the following conversation:") ||
						content.Text == "Warmup" {
						return InterceptTypeWarmup
					}
				}
			}
		}
		// 妫€鏌?system 涓殑鏍囬鎻愬彇妯″紡
		for _, sys := range req.System {
			if strings.Contains(sys.Text, "nalyze if this message indicates a new conversation topic. If it does, extract a 2-3 word title") {
				return InterceptTypeWarmup
			}
		}
	}

	return InterceptTypeNone
}

// sendMockInterceptStream 鍙戦€佹祦寮?mock 鍝嶅簲锛堢敤浜庤姹傛嫤鎴級
func sendMockInterceptStream(c *gin.Context, model string, interceptType InterceptType) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 鏍规嵁鎷︽埅绫诲瀷鍐冲畾鍝嶅簲鍐呭
	var msgID string
	var outputTokens int
	var textDeltas []string

	switch interceptType {
	case InterceptTypeSuggestionMode:
		msgID = generateRealisticMsgID()
		outputTokens = 1
		textDeltas = []string{""}
	default: // InterceptTypeWarmup
		msgID = generateRealisticMsgID()
		outputTokens = 2
		textDeltas = []string{"New", " Conversation"}
	}

	// Build message_start event 鈥?field order matches real Anthropic API response.
	messageStartJSON := `{"type":"message_start","message":{"model":` + strconv.Quote(model) + `,"id":` + strconv.Quote(msgID) + `,"type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}`

	// Build events
	events := []string{
		`event: message_start` + "\n" + `data: ` + string(messageStartJSON),
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	}

	// Add text deltas
	for _, text := range textDeltas {
		deltaJSON := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + strconv.Quote(text) + `}}`
		events = append(events, `event: content_block_delta`+"\n"+`data: `+string(deltaJSON))
	}

	// Add final events
	messageDeltaJSON := `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null,"stop_details":null},"usage":{"output_tokens":` + strconv.Itoa(outputTokens) + `}}`

	events = append(events,
		`event: content_block_stop`+"\n"+`data: {"index":0,"type":"content_block_stop"}`,
		`event: message_delta`+"\n"+`data: `+string(messageDeltaJSON),
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`,
	)

	for _, event := range events {
		_, _ = c.Writer.WriteString(event + "\n\n")
		c.Writer.Flush()
		time.Sleep(20 * time.Millisecond)
	}
}

// generateRealisticMsgID 鐢熸垚浠跨湡鐨勬秷鎭?ID锛坢sg_01XXXXXXX 鏍煎紡锛?// 鏍煎紡涓?Anthropic API 瀹樻柟鍝嶅簲涓€鑷达細msg_01 + 22 浣?Base62 闅忔満瀛楃
func generateRealisticMsgID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const idLen = 22
	randomBytes := make([]byte, idLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("msg_01%d", time.Now().UnixNano())
	}
	b := make([]byte, idLen)
	for i := range b {
		b[i] = charset[int(randomBytes[i])%len(charset)]
	}
	return "msg_01" + string(b)
}

// sendMockInterceptResponse 鍙戦€侀潪娴佸紡 mock 鍝嶅簲锛堢敤浜庤姹傛嫤鎴級
func sendMockInterceptResponse(c *gin.Context, model string, interceptType InterceptType) {
	var msgID, text, stopReason string
	var outputTokens int

	switch interceptType {
	case InterceptTypeSuggestionMode:
		msgID = generateRealisticMsgID()
		text = ""
		outputTokens = 1
		stopReason = "end_turn"
	case InterceptTypeMaxTokensOneHaiku:
		msgID = generateRealisticMsgID()
		text = "#"
		outputTokens = 1
		stopReason = "max_tokens" // max_tokens=1 鎺㈡祴璇锋眰鐨?stop_reason 搴斾负 max_tokens
	default: // InterceptTypeWarmup
		msgID = generateRealisticMsgID()
		text = "New Conversation"
		outputTokens = 2
		stopReason = "end_turn"
	}

	// 鏋勫缓瀹屾暣鐨勫搷搴旀牸寮忥紙涓?Anthropic API 瀹樻柟鍝嶅簲鏍煎紡涓€鑷达級
	response := gin.H{
		"model":         model,
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"content":       []gin.H{{"type": "text", "text": text}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"stop_details":  nil,
		"usage": gin.H{
			"input_tokens":                10,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
			"cache_creation": gin.H{
				"ephemeral_5m_input_tokens": 0,
				"ephemeral_1h_input_tokens": 0,
			},
			"output_tokens": outputTokens,
		},
	}

	c.JSON(http.StatusOK, response)
}

// extractQuotaResetSeconds 浠?quota 閿欒鐨?metadata 涓彁鍙?window_resets_at 骞惰绠?// 璺濋噸缃墿浣欑鏁般€俧allback 璺緞蹇呴』杩斿洖 鈮? 绉掞紝閬垮厤瀹㈡埛绔珛鍗抽噸璇曟棤闄愬惊鐜€?
func extractQuotaResetSeconds(err error) int {
	const fallback = 60
	appErr := pkgerrors.FromError(err)
	if appErr == nil {
		return fallback
	}
	raw, ok := appErr.Metadata["window_resets_at"]
	if !ok || raw == "" {
		return fallback
	}
	resetAt, parseErr := time.Parse(time.RFC3339, raw)
	if parseErr != nil {
		logger.L().With(
			zap.String("component", "handler.gateway.billing"),
			zap.String("raw", raw),
			zap.Error(parseErr),
		).Warn("quota.invalid_window_resets_at_format")
		return fallback
	}
	secs := time.Until(resetAt).Seconds()
	if secs <= 0 {
		// reset 鏃堕棿宸茶繃锛歝ache 涓?DB 搴旇姝ｅ湪鑷剤锛岃繑鍥?fallback 璁╁鎴风鎸夊父瑙勮妭濂忛€€閬匡紝
		// 閬垮厤杩斿洖 1 绉掑鑷村鎴风绔嬪嵆閲嶈瘯浠嶈Е鍙戦檺棰濈殑閫€閬垮惊鐜€?
		return fallback
	}
	return int(math.Ceil(secs))
}

func billingErrorDetails(err error) (status int, code, message string, retryAfter int) {
	if errors.Is(err, service.ErrBillingServiceUnavailable) {
		msg := pkgerrors.Message(err)
		if msg == "" {
			msg = "Billing service temporarily unavailable. Please retry later."
		}
		return http.StatusServiceUnavailable, "billing_service_error", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit1dExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	// 鐢ㄦ埛 RPM 瓒呴檺缁熶竴鏄犲皠涓?HTTP 429锛涗繚鐣欎笌鍏跺畠 rate_limit 涓€鑷寸殑閿欒鐮佷究浜庡鎴风鍒嗙被銆?
	// 杩斿洖 Retry-After 绉掓暟锛堝綋鍓嶅垎閽熷墿浣欑鏁帮級锛岃 SDK 鑷姩閫€閬裤€?
	if errors.Is(err, service.ErrUserRPMExceeded) {
		msg := pkgerrors.Message(err)
		retrySeconds := 60 - int(time.Now().Unix()%60)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, retrySeconds
	}
	if errors.Is(err, service.ErrUserPlatformDailyQuotaExhausted) ||
		errors.Is(err, service.ErrUserPlatformWeeklyQuotaExhausted) ||
		errors.Is(err, service.ErrUserPlatformMonthlyQuotaExhausted) {
		// 涓?RPM 瓒呴檺涓€鑷存槧灏?429 + Retry-After锛岃 SDK 鑷姩閫€閬匡紙鑰岄潪 403 鐩存帴澶辫触锛夈€?
		// 閿欒鐮佺敤 rate_limit_exceeded 涓?OpenAI 鍏煎瀹㈡埛绔竴鑷达紱缁嗗垎绫诲瀷鐢?ErrCode + window_resets_at metadata 鍖哄垎銆?
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, extractQuotaResetSeconds(err)
	}
	msg := pkgerrors.Message(err)
	if msg == "" {
		logger.L().With(
			zap.String("component", "handler.gateway.billing"),
			zap.Error(err),
		).Warn("gateway.billing_error_missing_message")
		msg = "Billing error"
	}
	return http.StatusForbidden, "billing_error", msg, 0
}

func (h *GatewayHandler) metadataBridgeEnabled() bool {
	if h == nil || h.cfg == nil {
		return true
	}
	return h.cfg.Gateway.OpenAIWS.MetadataBridgeEnabled
}

func (h *GatewayHandler) maybeLogCompatibilityFallbackMetrics(reqLog *zap.Logger) {
	if reqLog == nil {
		return
	}
	if gatewayCompatibilityMetricsLogCounter.Add(1)%gatewayCompatibilityMetricsLogInterval != 0 {
		return
	}
	metrics := service.SnapshotOpenAICompatibilityFallbackMetrics()
	reqLog.Info("gateway.compatibility_fallback_metrics",
		zap.Int64("session_hash_legacy_read_fallback_total", metrics.SessionHashLegacyReadFallbackTotal),
		zap.Int64("session_hash_legacy_read_fallback_hit", metrics.SessionHashLegacyReadFallbackHit),
		zap.Int64("session_hash_legacy_dual_write_total", metrics.SessionHashLegacyDualWriteTotal),
		zap.Float64("session_hash_legacy_read_hit_rate", metrics.SessionHashLegacyReadHitRate),
		zap.Int64("metadata_legacy_fallback_total", metrics.MetadataLegacyFallbackTotal),
	)
}

func (h *GatewayHandler) submitUsageRecordTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if sink, ok := gatewayruntime.UsageSinkFromContext(parent); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = gatewayruntime.WithUsageSink(ctx, sink)
		task(ctx)
		return
	}
	if h.usageRecordWorkerPool != nil {
		if mode := h.usageRecordWorkerPool.Submit(task); mode != service.UsageRecordSubmitModeDroppedStopped {
			return
		}
		// 姹犲凡鍋滄锛堣繘绋嬪叧鍋滅獥鍙ｏ級锛氳璐逛换鍔′笉鑳介潤榛樹涪澶憋紝闄嶇骇涓哄唴鑱斿悓姝ユ墽琛屻€?
		// 鏄惧紡閰嶇疆鐨?drop/sample 婧㈠嚭涓㈠純浠嶆寜閰嶇疆璇箟淇濈暀銆?
		logger.L().With(
			zap.String("component", "handler.gateway.messages"),
		).Warn("gateway.usage_record_task_stopped_sync_fallback")
	}
	// 鍥為€€璺緞锛歸orker 姹犳湭娉ㄥ叆鎴栧凡鍋滄鏃跺悓姝ユ墽琛岋紝閬垮厤閫€鍥炲埌鏃犵晫 goroutine 妯″紡銆?
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.messages"),
				zap.Any("panic", recovered),
			).Error("gateway.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

// getUserMsgQueueMode 鑾峰彇褰撳墠璇锋眰鐨?UMQ 妯″紡
// 杩斿洖 "serialize" | "throttle" | ""
func (h *GatewayHandler) getUserMsgQueueMode(account *service.Account, parsed *service.ParsedRequest) string {
	if h.userMsgQueueHelper == nil {
		return ""
	}
	// 浠呴€傜敤浜?Anthropic OAuth/SetupToken 璐﹀彿
	if !account.IsAnthropicOAuthOrSetupToken() {
		return ""
	}
	if !service.IsRealUserMessage(parsed) {
		return ""
	}
	// 璐﹀彿绾фā寮忎紭鍏堬紝fallback 鍒板叏灞€閰嶇疆
	mode := account.GetUserMsgQueueMode()
	if mode == "" {
		mode = h.cfg.Gateway.UserMessageQueue.GetEffectiveMode()
	}
	return mode
}
