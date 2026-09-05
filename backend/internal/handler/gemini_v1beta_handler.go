package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// geminiCLITmpDirRegex 鐢ㄤ簬浠?Gemini CLI 璇锋眰浣撲腑鎻愬彇 tmp 鐩綍鐨勫搱甯屽€?// 鍖归厤鏍煎紡: /Users/xxx/.gemini/tmp/[64浣嶅崄鍏繘鍒跺搱甯宂
var geminiCLITmpDirRegex = regexp.MustCompile(`/\.gemini/tmp/([A-Fa-f0-9]{64})`)

// GeminiV1BetaListModels proxies:
// GET /v1beta/models
func (h *GatewayHandler) GeminiV1BetaListModels(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	// 妫€鏌ュ钩鍙帮細浼樺厛浣跨敤寮哄埗骞冲彴锛?antigravity 璺敱锛夛紝鍚﹀垯瑕佹眰 gemini 鍒嗙粍
	forcePlatform, hasForcePlatform := middleware.GetForcePlatformFromContext(c)
	if !hasForcePlatform && effectiveAPIKeyPlatform(c, apiKey) != service.PlatformGemini {
		googleError(c, http.StatusBadRequest, "API key is not authorized for a Gemini platform")
		return
	}

	// 寮哄埗 antigravity 妯″紡锛氳繑鍥?antigravity 鏀寔鐨勬ā鍨嬪垪琛?
	if forcePlatform == service.PlatformAntigravity {
		c.JSON(http.StatusOK, antigravity.FallbackGeminiModelsList())
		return
	}

	account, err := h.geminiCompatService.SelectAccountForAIStudioEndpoints(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()))
	if err != nil {
		// 娌℃湁 gemini 璐︽埛锛屾鏌ユ槸鍚︽湁 antigravity 璐︽埛鍙敤
		hasAntigravity, _ := h.geminiCompatService.HasAntigravityAccounts(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()))
		if hasAntigravity {
			// antigravity 璐︽埛浣跨敤闈欐€佹ā鍨嬪垪琛?
			c.JSON(http.StatusOK, gemini.FallbackModelsList())
			return
		}
		markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts: "+err.Error())
		return
	}

	res, err := h.geminiCompatService.ForwardAIStudioGET(c.Request.Context(), account, "/v1beta/models")
	if err != nil {
		googleError(c, http.StatusBadGateway, err.Error())
		return
	}
	if shouldFallbackGeminiModels(res) {
		c.JSON(http.StatusOK, gemini.FallbackModelsList())
		return
	}
	writeUpstreamResponse(c, res)
}

// GeminiV1BetaGetModel proxies:
// GET /v1beta/models/{model}
func (h *GatewayHandler) GeminiV1BetaGetModel(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	// 妫€鏌ュ钩鍙帮細浼樺厛浣跨敤寮哄埗骞冲彴锛?antigravity 璺敱锛夛紝鍚﹀垯瑕佹眰 gemini 鍒嗙粍
	forcePlatform, hasForcePlatform := middleware.GetForcePlatformFromContext(c)
	if !hasForcePlatform && effectiveAPIKeyPlatform(c, apiKey) != service.PlatformGemini {
		googleError(c, http.StatusBadRequest, "API key is not authorized for a Gemini platform")
		return
	}

	modelName := strings.TrimSpace(c.Param("model"))
	if modelName == "" {
		googleError(c, http.StatusBadRequest, "Missing model in URL")
		return
	}
	// 妯″瀷鍚嶄細琚嫾杩涗笂娓?URL 鐨?path锛屽厛鍦ㄥ叆鍙ｆ牎楠岀墖娈靛悎瑙勬€э紝
	// 瑙?service/upstream_path_guard.go銆?
	if !service.IsSafeGeminiModelPathSegment(modelName) {
		googleError(c, http.StatusBadRequest, "Invalid model in URL")
		return
	}
	if resolvedModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok && strings.TrimSpace(resolvedModel) != "" {
		modelName = strings.TrimSpace(resolvedModel)
	}

	// 寮哄埗 antigravity 妯″紡锛氳繑鍥?antigravity 妯″瀷淇℃伅
	if forcePlatform == service.PlatformAntigravity {
		c.JSON(http.StatusOK, antigravity.FallbackGeminiModel(modelName))
		return
	}

	account, err := h.geminiCompatService.SelectAccountForAIStudioEndpoints(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()))
	if err != nil {
		// 娌℃湁 gemini 璐︽埛锛屾鏌ユ槸鍚︽湁 antigravity 璐︽埛鍙敤
		hasAntigravity, _ := h.geminiCompatService.HasAntigravityAccounts(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()))
		if hasAntigravity {
			// antigravity 璐︽埛浣跨敤闈欐€佹ā鍨嬩俊鎭?
			c.JSON(http.StatusOK, gemini.FallbackModel(modelName))
			return
		}
		markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts: "+err.Error())
		return
	}

	res, err := h.geminiCompatService.ForwardAIStudioGET(c.Request.Context(), account, "/v1beta/models/"+modelName)
	if err != nil {
		googleError(c, http.StatusBadGateway, err.Error())
		return
	}
	if shouldFallbackGeminiModel(modelName, res) {
		c.JSON(http.StatusOK, gemini.FallbackModel(modelName))
		return
	}
	writeUpstreamResponse(c, res)
}

// GeminiV1BetaModels proxies Gemini native REST endpoints like:
// POST /v1beta/models/{model}:generateContent
// POST /v1beta/models/{model}:streamGenerateContent?alt=sse
func (h *GatewayHandler) GeminiV1BetaModels(c *gin.Context) {
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointGeminiNative)
}

func (h *GatewayHandler) legacyGeminiV1BetaModels(c *gin.Context) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		googleError(c, http.StatusUnauthorized, "Invalid API key")
		return
	}
	authSubject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		googleError(c, http.StatusInternalServerError, "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gemini_v1beta.models",
		zap.Int64("user_id", authSubject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
	)

	// 妫€鏌ュ钩鍙帮細浼樺厛浣跨敤寮哄埗骞冲彴锛?antigravity 璺敱锛屼腑闂翠欢宸茶缃?request.Context锛夛紝鍚﹀垯瑕佹眰 gemini 鍒嗙粍
	if !middleware.HasForcePlatform(c) {
		if effectiveAPIKeyPlatform(c, apiKey) != service.PlatformGemini {
			googleError(c, http.StatusBadRequest, "API key is not authorized for a Gemini platform")
			return
		}
	}

	modelName, action, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
	if err != nil {
		googleError(c, http.StatusNotFound, err.Error())
		return
	}
	// URL 閲岀殑妯″瀷鍚嶆渶缁堜細琚嫾杩涗笂娓?/v1beta/models/{model}:{action}锛?
	// 鍏堝湪鍏ュ彛鏍￠獙鐗囨鍚堣鎬э紝瑙?service/upstream_path_guard.go銆?
	if !service.IsSafeGeminiModelPathSegment(modelName) {
		googleError(c, http.StatusBadRequest, "Invalid model in URL")
		return
	}
	if resolvedModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context()); ok && strings.TrimSpace(resolvedModel) != "" {
		modelName = strings.TrimSpace(resolvedModel)
	}

	stream := action == "streamGenerateContent"
	reqLog = reqLog.With(zap.String("model", modelName), zap.String("action", action), zap.Bool("stream", stream))

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			googleError(c, http.StatusRequestEntityTooLarge, buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		googleError(c, http.StatusBadRequest, "Failed to read request body")
		return
	}
	if len(body) == 0 {
		googleError(c, http.StatusBadRequest, "Request body is empty")
		return
	}

	setOpsRequestContext(c, modelName, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, authSubject, service.ContentModerationProtocolGemini, modelName, body); decision != nil && !decision.AllowNextStage {
		googleSecurityAuditError(c, decision)
		return
	}

	// 瑙ｆ瀽娓犻亾绾фā鍨嬫槧灏?
	modelMapping := h.gatewayService.ResolvePlatformModelMapping(c.Request.Context(), modelName)
	reqModel := modelName
	if modelMapping.Mapped {
		modelName = modelMapping.MappedModel
	}

	// Get subscription (may be nil)
	subscription, _ := middleware.GetSubscriptionFromContext(c)

	// For Gemini native API, do not send Claude-style ping frames.
	geminiConcurrency := NewConcurrencyHelper(h.concurrencyHelper.concurrencyService, SSEPingFormatNone, 0)

	// 1) user concurrency slot
	streamStarted := false
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	userReleaseFunc, err := geminiConcurrency.AcquireUserSlotWithWait(c, authSubject.UserID, authSubject.Concurrency, stream, &streamStarted)
	if err != nil {
		reqLog.Warn("gemini.user_slot_acquire_failed", zap.Error(err))
		googleError(c, http.StatusTooManyRequests, err.Error())
		return
	}
	// 纭繚璇锋眰鍙栨秷鏃朵篃浼氶噴鏀炬Ы浣嶏紝閬垮厤闀胯繛鎺ヨ鍔ㄤ腑鏂€犳垚娉勬紡
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2) billing eligibility check (after wait)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gemini.billing_eligibility_check_failed", zap.Error(err))
		status, _, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		googleError(c, status, message)
		return
	}

	// 3) select account (sticky session based on request body)
	// 浼樺厛浣跨敤 Gemini CLI 鐨勪細璇濇爣璇嗭紙privileged-user-id + tmp 鐩綍鍝堝笇锛?
	sessionHash := extractGeminiCLISessionHash(c, body)
	if sessionHash == "" {
		// Fallback: 浣跨敤閫氱敤鐨勪細璇濆搱甯岀敓鎴愰€昏緫锛堥€傜敤浜庡叾浠栧鎴风锛?
		parsedReq, _ := service.ParseGatewayRequest(service.NewRequestBodyRef(body), domain.PlatformGemini)
		if parsedReq != nil {
			parsedReq.SessionContext = &service.SessionContext{
				ClientIP:  ip.GetClientIP(c),
				UserAgent: c.GetHeader("User-Agent"),
				APIKeyID:  apiKey.ID,
			}
		}
		sessionHash = h.gatewayService.GenerateSessionHash(parsedReq)
	}
	sessionKey := sessionHash
	if sessionHash != "" {
		sessionKey = "gemini:" + sessionHash
	}

	// 鏌ヨ绮樻€т細璇濈粦瀹氱殑璐﹀彿 ID锛堢敤浜庢娴嬭处鍙峰垏鎹級
	var sessionBoundAccountID int64
	if sessionKey != "" {
		sessionBoundAccountID, _ = h.gatewayService.GetCachedSessionAccountID(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey)
		if sessionBoundAccountID > 0 {
			prefetchedPlatformNamespaceID := int64(0)
			if service.PlatformSchedulingID(c.Request.Context()) != nil {
				prefetchedPlatformNamespaceID = *service.PlatformSchedulingID(c.Request.Context())
			}
			ctx := service.WithPrefetchedStickySession(c.Request.Context(), sessionBoundAccountID, prefetchedPlatformNamespaceID, h.metadataBridgeEnabled())
			c.Request = c.Request.WithContext(ctx)
		}
	}

	// === Gemini 鍐呭鎽樿浼氳瘽 Fallback 閫昏緫 ===
	// 褰撳師鏈変細璇濇爣璇嗘棤鏁堟椂锛坰essionBoundAccountID == 0锛夛紝灏濊瘯鍩轰簬鍐呭鎽樿閾惧尮閰?
	var geminiDigestChain string
	var geminiPrefixHash string
	var geminiSessionUUID string
	var matchedDigestChain string
	useDigestFallback := sessionBoundAccountID == 0

	if useDigestFallback {
		// 瑙ｆ瀽 Gemini 璇锋眰浣?
		var geminiReq antigravity.GeminiRequest
		if err := json.Unmarshal(body, &geminiReq); err == nil && len(geminiReq.Contents) > 0 {
			// 鐢熸垚鎽樿閾?
			geminiDigestChain = service.BuildGeminiDigestChain(&geminiReq)
			if geminiDigestChain != "" {
				// 鐢熸垚鍓嶇紑 hash
				userAgent := c.GetHeader("User-Agent")
				clientIP := ip.GetClientIP(c)
				platform := effectiveAPIKeyPlatform(c, apiKey)
				geminiPrefixHash = service.GenerateGeminiPrefixHash(
					authSubject.UserID,
					apiKey.ID,
					clientIP,
					userAgent,
					platform,
					modelName,
				)

				// 鏌ユ壘浼氳瘽
				foundUUID, foundAccountID, foundMatchedChain, found := h.gatewayService.FindGeminiSession(
					c.Request.Context(),
					derefPlatformID(service.PlatformSchedulingID(c.Request.Context())),
					geminiPrefixHash,
					geminiDigestChain,
				)
				if found {
					matchedDigestChain = foundMatchedChain
					sessionBoundAccountID = foundAccountID
					geminiSessionUUID = foundUUID
					reqLog.Info("gemini.digest_fallback_matched",
						zap.String("session_uuid_prefix", safeShortPrefix(foundUUID, 8)),
						zap.Int64("account_id", foundAccountID),
						zap.String("digest_chain", truncateDigestChain(geminiDigestChain)),
					)

					// 鍏抽敭锛氬鏋滃師 sessionKey 涓虹┖锛屼娇鐢?prefixHash + uuid 浣滀负 sessionKey
					// 杩欐牱 SelectAccountWithLoadAwareness 鐨勭矘鎬т細璇濋€昏緫浼氫紭鍏堜娇鐢ㄥ尮閰嶅埌鐨勮处鍙?
					if sessionKey == "" {
						sessionKey = service.GenerateGeminiDigestSessionKey(geminiPrefixHash, foundUUID)
					}
					_ = h.gatewayService.BindStickySession(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, foundAccountID)
				} else {
					// 鐢熸垚鏂扮殑浼氳瘽 UUID
					geminiSessionUUID = uuid.New().String()
					// 涓烘柊浼氳瘽涔熺敓鎴?sessionKey锛堢敤浜庡悗缁姹傜殑绮樻€т細璇濓級
					if sessionKey == "" {
						sessionKey = service.GenerateGeminiDigestSessionKey(geminiPrefixHash, geminiSessionUUID)
					}
				}
			}
		}
	}

	// 鍒ゆ柇鏄惁鐪熺殑缁戝畾浜嗙矘鎬т細璇濓細鏈?sessionKey 涓斿凡缁忕粦瀹氬埌鏌愪釜璐﹀彿
	hasBoundSession := sessionKey != "" && sessionBoundAccountID > 0
	cleanedForUnknownBinding := false

	fs := NewFailoverState(h.maxAccountSwitchesGemini, hasBoundSession)

	// 鍗曡处鍙峰垎缁勬彁鍓嶈缃?SingleAccountRetry 鏍囪锛岃 Service 灞傞娆?503 灏变笉璁炬ā鍨嬮檺娴佹爣璁般€?
	// 閬垮厤鍗曡处鍙峰垎缁勬敹鍒?503 (MODEL_CAPACITY_EXHAUSTED) 鏃惰 29s 闄愭祦锛屽鑷村悗缁姹傝繛缁揩閫熷け璐ャ€?
	if h.gatewayService.IsSingleAntigravityPlatformAccount(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context())) {
		ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)
	}

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, modelName, fs.FailedAccountIDs, "", int64(0)) // Gemini 涓嶄娇鐢ㄤ細璇濋檺鍒?
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, modelName, modelName, service.PlatformGemini)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available Gemini accounts: " + err.Error()
				}
				googleError(c, cls.Status, message)
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
				h.handleGeminiFailoverExhausted(c, fs.LastFailoverErr)
				return
			}
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 妫€娴嬭处鍙峰垏鎹細濡傛灉绮樻€т細璇濈粦瀹氱殑璐﹀彿涓庡綋鍓嶉€夋嫨鐨勮处鍙蜂笉鍚岋紝娓呴櫎 thoughtSignature
		// 娉ㄦ剰锛欸emini 鍘熺敓 API 鐨?thoughtSignature 涓庡叿浣撲笂娓歌处鍙峰己鐩稿叧锛涜法璐﹀彿閫忎紶浼氬鑷?400銆?
		if sessionBoundAccountID > 0 && sessionBoundAccountID != account.ID {
			reqLog.Info("gemini.sticky_session_account_switched",
				zap.Int64("from_account_id", sessionBoundAccountID),
				zap.Int64("to_account_id", account.ID),
				zap.Bool("clean_thought_signature", true),
			)
			body = service.CleanGeminiNativeThoughtSignatures(body)
			sessionBoundAccountID = account.ID
		} else if sessionKey != "" && sessionBoundAccountID == 0 && !cleanedForUnknownBinding && bytes.Contains(body, []byte(`"thoughtSignature"`)) {
			// 鏃犵紦瀛樼粦瀹氫絾璇锋眰閲屽凡鏈?thoughtSignature锛氬父瑙佷簬缂撳瓨涓㈠け/TTL 杩囨湡鍚庯紝瀹㈡埛绔户缁惡甯︽棫绛惧悕銆?
			// 涓洪伩鍏嶇涓€娆¤浆鍙戝氨 400锛岃繖閲屽仛涓€娆＄‘瀹氭€ф竻鐞嗭紝璁╂柊璐﹀彿閲嶆柊鐢熸垚绛惧悕閾捐矾銆?
			reqLog.Info("gemini.sticky_session_binding_missing",
				zap.Bool("clean_thought_signature", true),
			)
			body = service.CleanGeminiNativeThoughtSignatures(body)
			cleanedForUnknownBinding = true
			sessionBoundAccountID = account.ID
		} else if sessionBoundAccountID == 0 {
			// 璁板綍鏈璇锋眰涓娆￠€夋嫨鍒扮殑璐﹀彿锛屼究浜庡悓涓€璇锋眰鍐?failover 鏃舵娴嬪垏鎹€?
			sessionBoundAccountID = account.ID
		}

		// 4) account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				googleError(c, http.StatusServiceUnavailable, "No available Gemini accounts")
				return
			}
			accountWaitCounted := false
			canWait, err := geminiConcurrency.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
			if err != nil {
				reqLog.Warn("gemini.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			} else if !canWait {
				reqLog.Info("gemini.account_wait_queue_full",
					zap.Int64("account_id", account.ID),
					zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
				)
				googleError(c, http.StatusTooManyRequests, "Too many pending requests, please retry later")
				return
			}
			if err == nil && canWait {
				accountWaitCounted = true
			}
			defer func() {
				if accountWaitCounted {
					geminiConcurrency.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				}
			}()

			accountReleaseFunc, err = geminiConcurrency.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				stream,
				&streamStarted,
			)
			if err != nil {
				reqLog.Warn("gemini.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				googleError(c, http.StatusTooManyRequests, err.Error())
				return
			}
			if accountWaitCounted {
				geminiConcurrency.DecrementAccountWaitCount(c.Request.Context(), account.ID)
				accountWaitCounted = false
			}
			if err := h.gatewayService.BindStickySession(c.Request.Context(), service.PlatformSchedulingID(c.Request.Context()), sessionKey, account.ID); err != nil {
				reqLog.Warn("gemini.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		// 璐﹀彿妲戒綅/绛夊緟璁℃暟闇€瑕佸湪瓒呮椂鎴栨柇寮€鏃跺畨鍏ㄥ洖鏀?
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		// 5) forward (鏍规嵁骞冲彴鍒嗘祦)
		var result *service.ForwardResult
		requestCtx := c.Request.Context()
		if fs.SwitchCount > 0 {
			requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
		}
		sessionPlatformID := derefPlatformID(service.PlatformSchedulingID(c.Request.Context()))
		if account.Platform == service.PlatformAntigravity && account.Type != service.AccountTypeAPIKey {
			result, err = h.antigravityGatewayService.ForwardGemini(
				requestCtx,
				c,
				account,
				modelName,
				action,
				stream,
				body,
				hasBoundSession,
				service.WithForwardGeminiSession(sessionPlatformID, sessionKey),
			)
		} else {
			result, err = h.geminiCompatService.ForwardNative(requestCtx, c, account, modelName, action, stream, body)
		}
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				failoverAction := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, effectiveSameAccountRetryLimit(failoverErr, account), failoverErr)
				switch failoverAction {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					h.handleGeminiFailoverExhausted(c, fs.LastFailoverErr)
					return
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
			}
			// ForwardNative already wrote the response
			reqLog.Error("gemini.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			return
		}

		// 鎹曡幏璇锋眰淇℃伅锛堢敤浜庡紓姝ヨ褰曪紝閬垮厤鍦?goroutine 涓闂?gin.Context锛?
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)

		// 淇濆瓨 Gemini 鍐呭鎽樿浼氳瘽锛堢敤浜?Fallback 鍖归厤锛?
		if useDigestFallback && geminiDigestChain != "" && geminiPrefixHash != "" {
			if err := h.gatewayService.SaveGeminiSession(
				c.Request.Context(),
				derefPlatformID(service.PlatformSchedulingID(c.Request.Context())),
				geminiPrefixHash,
				geminiDigestChain,
				geminiSessionUUID,
				account.ID,
				matchedDigestChain,
			); err != nil {
				reqLog.Warn("gemini.digest_session_save_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}

		// 浣跨敤閲忚褰曢€氳繃鏈夌晫 worker 姹犳彁浜わ紝閬垮厤璇锋眰鐑矾寰勫垱寤烘棤鐣?goroutine銆?
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		// ForceCacheBilling 鎻愬墠鎷嶆垚鏍囬噺锛岄伩鍏?worker 闂寘淇濇椿 failover 鐘舵€侀噷鐨勫搷搴斾綋銆?
		forceCacheBilling := fs.ForceCacheBilling
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := recordGatewayLongContextUsage(ctx, h.gatewayService, &service.RecordUsageLongContextInput{
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
				RequestPayloadHash:      requestPayloadHash,
				LongContextThreshold:    200000,
				LongContextMultiplier:   2.0,
				ForceCacheBilling:       forceCacheBilling,
				APIKeyService:           h.apiKeyService,
				SessionID:               sessionID,
				ModelRoutingUsageFields: clientRequestedUsageFields(c, modelMapping, reqModel, result.UpstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.gemini_v1beta.models"),
					zap.Int64("user_id", authSubject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("platform_namespace_id", service.PlatformSchedulingID(c.Request.Context())),
					zap.String("model", modelName),
					zap.Int64("account_id", account.ID),
				).Error("gemini.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("gemini.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", fs.SwitchCount),
		)
		return
	}
}

func parseGeminiModelAction(rest string) (model string, action string, err error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", &pathParseError{"missing path"}
	}

	// Standard: {model}:{action}
	if i := strings.Index(rest, ":"); i > 0 && i < len(rest)-1 {
		return rest[:i], rest[i+1:], nil
	}

	// Fallback: {model}/{action}
	if i := strings.Index(rest, "/"); i > 0 && i < len(rest)-1 {
		return rest[:i], rest[i+1:], nil
	}

	return "", "", &pathParseError{"invalid model action path"}
}

func (h *GatewayHandler) handleGeminiFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError) {
	if failoverErr == nil {
		googleError(c, http.StatusBadGateway, "Upstream request failed")
		return
	}

	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody

	// 鍏堟鏌ラ€忎紶瑙勫垯
	if h.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.errorPassthroughService.MatchRule(service.PlatformGemini, statusCode, responseBody); rule != nil {
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

			googleError(c, respCode, msg)
			return
		}
	}

	// 璁板綍鍘熷涓婃父鐘舵€佺爜锛屼互渚?ops 閿欒鏃ュ織鎹曡幏鐪熷疄鐨勪笂娓搁敊璇?
	upstreamMsg := service.ExtractUpstreamErrorMessage(responseBody)
	service.SetOpsUpstreamError(c, statusCode, upstreamMsg, "")

	// 浣跨敤榛樿鐨勯敊璇槧灏?
	status, message := mapGeminiUpstreamError(statusCode)
	googleError(c, status, message)
}

func mapGeminiUpstreamError(statusCode int) (int, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "Upstream request failed"
	}
}

type pathParseError struct{ msg string }

func (e *pathParseError) Error() string { return e.msg }

func googleError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
}

func writeUpstreamResponse(c *gin.Context, res *service.UpstreamHTTPResult) {
	if res == nil {
		googleError(c, http.StatusBadGateway, "Empty upstream response")
		return
	}
	for k, vv := range res.Headers {
		// Avoid overriding content-length and hop-by-hop headers.
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") || strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vv {
			c.Writer.Header().Add(k, v)
		}
	}
	contentType := res.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(res.StatusCode, contentType, res.Body)
}

func shouldFallbackGeminiModels(res *service.UpstreamHTTPResult) bool {
	if res == nil {
		return true
	}
	if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusForbidden {
		return false
	}
	if strings.Contains(strings.ToLower(res.Headers.Get("Www-Authenticate")), "insufficient_scope") {
		return true
	}
	if strings.Contains(strings.ToLower(string(res.Body)), "insufficient authentication scopes") {
		return true
	}
	if strings.Contains(strings.ToLower(string(res.Body)), "access_token_scope_insufficient") {
		return true
	}
	return false
}

func shouldFallbackGeminiModel(modelName string, res *service.UpstreamHTTPResult) bool {
	if shouldFallbackGeminiModels(res) {
		return true
	}
	if res == nil || res.StatusCode != http.StatusNotFound {
		return false
	}
	return gemini.HasFallbackModel(modelName)
}

// extractGeminiCLISessionHash 浠?Gemini CLI 璇锋眰涓彁鍙栦細璇濇爣璇嗐€?// 缁勫悎 x-gemini-api-privileged-user-id header 鍜岃姹備綋涓殑 tmp 鐩綍鍝堝笇銆?//
// 浼氳瘽鏍囪瘑鐢熸垚绛栫暐锛?//  1. 浠庤姹備綋涓彁鍙?tmp 鐩綍鍝堝笇锛?4浣嶅崄鍏繘鍒讹級
//  2. 浠?header 涓彁鍙?privileged-user-id锛圲UID锛?//  3. 缁勫悎涓よ€呯敓鎴?SHA256 鍝堝笇浣滀负鏈€缁堢殑浼氳瘽鏍囪瘑
//
// 濡傛灉鎵句笉鍒?tmp 鐩綍鍝堝笇锛岃繑鍥炵┖瀛楃涓诧紙涓嶄娇鐢ㄧ矘鎬т細璇濓級銆?//
// extractGeminiCLISessionHash extracts session identifier from Gemini CLI requests.
// Combines x-gemini-api-privileged-user-id header with tmp directory hash from request body.
func extractGeminiCLISessionHash(c *gin.Context, body []byte) string {
	// 1. 浠庤姹備綋涓彁鍙?tmp 鐩綍鍝堝笇
	match := geminiCLITmpDirRegex.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	tmpDirHash := string(match[1])

	// 2. 鎻愬彇 privileged-user-id
	privilegedUserID := strings.TrimSpace(c.GetHeader("x-gemini-api-privileged-user-id"))

	// 3. 缁勫悎鐢熸垚鏈€缁堢殑 session hash
	if privilegedUserID != "" {
		// 缁勫悎涓や釜鏍囪瘑绗︼細privileged-user-id + tmp 鐩綍鍝堝笇
		combined := privilegedUserID + ":" + tmpDirHash
		hash := sha256.Sum256([]byte(combined))
		return hex.EncodeToString(hash[:])
	}

	// 濡傛灉娌℃湁 privileged-user-id锛岀洿鎺ヤ娇鐢?tmp 鐩綍鍝堝笇
	return tmpDirHash
}

func truncateDigestChain(chain string) string {
	if len(chain) <= 50 {
		return chain
	}
	return chain[:50] + "..."
}

func safeShortPrefix(value string, n int) string {
	if n <= 0 || len(value) <= n {
		return value
	}
	return value[:n]
}

// derefPlatformID 瀹夊叏瑙ｅ紩鐢?*int64锛宯il 杩斿洖 0
func derefPlatformID(platformID *int64) int64 {
	if platformID == nil {
		return 0
	}
	return *platformID
}
