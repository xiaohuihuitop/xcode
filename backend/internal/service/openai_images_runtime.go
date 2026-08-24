package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/tidwall/gjson"
)

// openAIImagesExchangeSink is the response surface needed by the image
// protocol. It intentionally mirrors gatewayruntime.HTTPExchange without
// exposing the request side, so the response state machine can be shared by
// API-key and OAuth transports.
type openAIImagesExchangeSink interface {
	Header() http.Header
	WriteHeader(int)
	Write([]byte) (int, error)
	Flush()
	Written() bool
	Size() int
}

type runtimeOpenAIImagesSink struct {
	exchange gatewayruntime.HTTPExchange
}

func (s runtimeOpenAIImagesSink) Header() http.Header {
	if s.exchange == nil {
		return make(http.Header)
	}
	return s.exchange.Header()
}

func (s runtimeOpenAIImagesSink) WriteHeader(status int) {
	if s.exchange == nil || s.exchange.Written() {
		return
	}
	s.exchange.SetState(gatewayruntime.HTTPExchangeStatusStateKey, status)
	s.exchange.WriteHeader(status)
}

func (s runtimeOpenAIImagesSink) Write(body []byte) (int, error) {
	if s.exchange == nil {
		return 0, ErrRuntimeExchangeUnavailable
	}
	return s.exchange.Write(body)
}

func (s runtimeOpenAIImagesSink) Flush() {
	if s.exchange != nil {
		s.exchange.Flush()
	}
}

func (s runtimeOpenAIImagesSink) Written() bool {
	return s.exchange != nil && s.exchange.Written()
}

func (s runtimeOpenAIImagesSink) Size() int {
	if s.exchange == nil {
		return 0
	}
	return s.exchange.Size()
}

func (s *OpenAIGatewayService) ForwardImagesExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	if exchange == nil || exchange.Request() == nil || account == nil {
		return nil, ErrRuntimeExchangeUnavailable
	}
	if parsed == nil {
		return nil, fmt.Errorf("parsed images request is required")
	}
	if ctx == nil {
		ctx = exchange.Request().Context()
	}
	switch account.Type {
	case AccountTypeAPIKey:
		return s.forwardOpenAIImagesAPIKeyExchange(ctx, exchange, account, body, parsed, channelMappedModel)
	case AccountTypeOAuth:
		return s.forwardOpenAIImagesOAuthExchange(ctx, exchange, account, parsed, channelMappedModel)
	default:
		return nil, fmt.Errorf("openai images exchange does not support account type: %s", account.Type)
	}
}

func (s *OpenAIGatewayService) forwardOpenAIImagesOAuthExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	if parsed.Stream {
		return s.forwardOpenAIImagesOAuthStreamingExchange(ctx, exchange, account, parsed, channelMappedModel)
	}
	return s.forwardOpenAIImagesOAuthNonStreamingExchange(ctx, exchange, account, parsed, channelMappedModel)
}

func (s *OpenAIGatewayService) forwardOpenAIImagesOAuthNonStreamingExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	requestModel := strings.TrimSpace(parsed.Model)
	if mapped := strings.TrimSpace(channelMappedModel); mapped != "" {
		requestModel = mapped
	}
	if requestModel == "" {
		requestModel = "gpt-image-2"
	}
	if err := validateOpenAIImagesModel(requestModel); err != nil {
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	token, _, err := s.GetAccessToken(upstreamCtx, account)
	if err != nil {
		return nil, err
	}
	responsesBody, err := buildOpenAIImagesResponsesRequest(parsed, requestModel)
	if err != nil {
		return nil, err
	}
	upstreamReq, err := s.buildOpenAIImagesOAuthRequestFromHeaders(
		upstreamCtx,
		exchange.Request().Header,
		account,
		responsesBody,
		token,
		parsed.StickySessionSeed(),
	)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "request_error", Message: safeErr,
		})
		writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
			writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
		}
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if !agentIdentityTaskRecoveryWasTried(ctx) && s.isAgentIdentityAccount(ctx, account) && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
			expectedTaskID := account.GetCredential("task_id")
			if err := s.recoverAgentIdentityTask(ctx, account, expectedTaskID); err != nil {
				return nil, fmt.Errorf("agent identity task recovery failed: %w", err)
			}
			return s.forwardOpenAIImagesOAuthNonStreamingExchange(markAgentIdentityTaskRecoveryTried(ctx), exchange, account, parsed, channelMappedModel)
		}
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			shouldDisable := s.handleFailoverSideEffects(upstreamCtx, resp, account, respBody, requestModel)
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "failover", Message: upstreamMsg,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: resp.StatusCode, ResponseBody: respBody,
				RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleOpenAIImagesErrorResponseExchange(upstreamCtx, exchange, resp, respBody, account, requestModel)
	}
	usage := OpenAIUsage{}
	forEachOpenAISSEDataPayload(string(respBody), func(data []byte) {
		s.parseOpenAIImagesSSEUsageBytes(data, &usage)
	})
	results, createdAt, usageRaw, firstMeta, _, err := collectOpenAIImagesFromResponsesBody(respBody)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		if upstreamErr := extractOpenAIImagesUpstreamError(respBody); upstreamErr != nil {
			writeRuntimeOpenAIImagesError(exchange, upstreamErr.clientStatusCode(), upstreamErr.clientErrorType(), upstreamErr.clientMessage())
			return nil, upstreamErr
		}
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: respBody, RetryableOnSameAccount: true}
	}
	if strings.TrimSpace(firstMeta.Model) == "" {
		firstMeta.Model = requestModel
	}
	responseBody, err := buildOpenAIImagesAPIResponse(results, createdAt, usageRaw, firstMeta, parsed.ResponseFormat)
	if err != nil {
		return nil, err
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	exchange.Header().Set("Content-Type", "application/json; charset=utf-8")
	exchange.WriteHeader(resp.StatusCode)
	if _, err := exchange.Write(responseBody); err != nil {
		return nil, err
	}
	return &OpenAIForwardResult{
		RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: requestModel, UpstreamModel: requestModel,
		Stream: parsed.Stream, ResponseHeaders: resp.Header.Clone(), Duration: time.Since(startTime),
		ImageCount: len(results), ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size,
		ImageOutputSizes: openAIResponsesImageResultSizes(results),
	}, nil
}

func (s *OpenAIGatewayService) forwardOpenAIImagesOAuthStreamingExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	requestModel := strings.TrimSpace(parsed.Model)
	if mapped := strings.TrimSpace(channelMappedModel); mapped != "" {
		requestModel = mapped
	}
	if requestModel == "" {
		requestModel = "gpt-image-2"
	}
	if err := validateOpenAIImagesModel(requestModel); err != nil {
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	token, _, err := s.GetAccessToken(upstreamCtx, account)
	if err != nil {
		return nil, err
	}
	responsesBody, err := buildOpenAIImagesResponsesRequest(parsed, requestModel)
	if err != nil {
		return nil, err
	}
	upstreamReq, err := s.buildOpenAIImagesOAuthRequestFromHeaders(upstreamCtx, exchange.Request().Header, account, responsesBody, token, parsed.StickySessionSeed())
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "request_error", Message: safeErr,
		})
		writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		respBody := s.readUpstreamErrorBody(resp)
		if !agentIdentityTaskRecoveryWasTried(ctx) && s.isAgentIdentityAccount(ctx, account) && isAgentIdentityTaskInvalidHTTPResponse(resp.StatusCode, respBody) {
			expectedTaskID := account.GetCredential("task_id")
			if err := s.recoverAgentIdentityTask(ctx, account, expectedTaskID); err != nil {
				return nil, fmt.Errorf("agent identity task recovery failed: %w", err)
			}
			return s.forwardOpenAIImagesOAuthStreamingExchange(markAgentIdentityTaskRecoveryTried(ctx), exchange, account, parsed, channelMappedModel)
		}
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			shouldDisable := s.handleFailoverSideEffects(upstreamCtx, resp, account, respBody, requestModel)
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "failover", Message: upstreamMsg,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: resp.StatusCode, ResponseBody: respBody,
				RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleOpenAIImagesErrorResponseExchange(upstreamCtx, exchange, resp, respBody, account, requestModel)
	}

	sink := runtimeOpenAIImagesSink{exchange: exchange}
	usage, imageCount, imageOutputSizes, firstTokenMs, streamErr := s.handleOpenAIImagesOAuthStreamingResponseSink(
		resp, sink, startTime, parsed.ResponseFormat, openAIImagesStreamPrefix(parsed), requestModel,
	)
	result := &OpenAIForwardResult{
		RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: requestModel, UpstreamModel: requestModel,
		Stream: true, ResponseHeaders: resp.Header.Clone(), Duration: time.Since(startTime), FirstTokenMs: firstTokenMs,
		ImageCount: imageCount, ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size, ImageOutputSizes: imageOutputSizes,
	}
	if streamErr != nil {
		return result, streamErr
	}
	return result, nil
}

func (s *OpenAIGatewayService) handleOpenAIImagesOAuthStreamingResponseSink(
	resp *http.Response,
	sink openAIImagesExchangeSink,
	startTime time.Time,
	responseFormat string,
	streamPrefix string,
	fallbackModel string,
) (OpenAIUsage, int, []string, *int, error) {
	responseheaders.WriteFilteredHeaders(sink.Header(), resp.Header, s.responseHeaderFilter)
	sink.Header().Set("Content-Type", "text/event-stream")
	sink.Header().Set("Cache-Control", "no-cache")
	sink.Header().Set("Connection", "keep-alive")
	sink.WriteHeader(resp.StatusCode)
	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "b64_json"
	}
	usage := OpenAIUsage{}
	imageCount := 0
	var imageOutputSizes []string
	var firstTokenMs *int
	emitted := make(map[string]struct{})
	pendingResults := make([]openAIResponsesImageResult, 0, 1)
	pendingSeen := make(map[string]struct{})
	streamMeta := openAIResponsesImageResult{Model: strings.TrimSpace(fallbackModel)}
	var createdAt int64
	clientDisconnected := false
	var sseData openAISSEDataAccumulator
	var processDataErr error
	processDataDone := false

	writeEvent := func(eventName string, payload []byte) {
		if clientDisconnected {
			return
		}
		if err := writeOpenAIImagesStreamEventSink(sink, eventName, payload); err != nil {
			clientDisconnected = true
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] OAuth images stream client disconnected, continue draining upstream for billing")
		}
	}
	processData := func(dataBytes []byte) {
		if processDataDone || processDataErr != nil {
			return
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		s.parseOpenAIImagesSSEUsageBytes(dataBytes, &usage)
		if !gjson.ValidBytes(dataBytes) {
			return
		}
		if meta, eventCreatedAt, ok := extractOpenAIResponsesImageMetaFromLifecycleEvent(dataBytes); ok {
			mergeOpenAIResponsesImageMeta(&streamMeta, meta)
			if eventCreatedAt > 0 {
				createdAt = eventCreatedAt
			}
		}
		switch gjson.GetBytes(dataBytes, "type").String() {
		case "response.image_generation_call.partial_image":
			b64 := strings.TrimSpace(gjson.GetBytes(dataBytes, "partial_image_b64").String())
			if b64 == "" {
				return
			}
			eventName := streamPrefix + ".partial_image"
			partialMeta := streamMeta
			mergeOpenAIResponsesImageMeta(&partialMeta, openAIResponsesImageResult{
				OutputFormat: strings.TrimSpace(gjson.GetBytes(dataBytes, "output_format").String()),
				Background:   strings.TrimSpace(gjson.GetBytes(dataBytes, "background").String()),
			})
			writeEvent(eventName, buildOpenAIImagesStreamPartialPayload(
				eventName, b64, gjson.GetBytes(dataBytes, "partial_image_index").Int(), format, createdAt, partialMeta,
			))
		case "response.output_item.done":
			img, itemID, ok, extractErr := extractOpenAIImageFromResponsesOutputItemDone(dataBytes)
			if extractErr != nil {
				writeEvent("error", buildOpenAIImagesStreamErrorBody(extractErr.Error()))
				processDataErr, processDataDone = extractErr, true
				return
			}
			if !ok {
				return
			}
			mergeOpenAIResponsesImageMeta(&streamMeta, img)
			mergeOpenAIResponsesImageMeta(&img, streamMeta)
			key := openAIResponsesImageResultKey(itemID, img)
			if _, exists := emitted[key]; exists {
				return
			}
			if _, exists := pendingSeen[key]; exists {
				return
			}
			pendingSeen[key] = struct{}{}
			pendingResults = append(pendingResults, img)
		case "response.completed":
			results, _, usageRaw, firstMeta, extractErr := extractOpenAIImagesFromResponsesCompleted(dataBytes)
			if extractErr != nil {
				writeEvent("error", buildOpenAIImagesStreamErrorBody(extractErr.Error()))
				processDataErr, processDataDone = extractErr, true
				return
			}
			mergeOpenAIResponsesImageMeta(&streamMeta, firstMeta)
			finalResults := make([]openAIResponsesImageResult, 0, len(results)+len(pendingResults))
			finalSeen := make(map[string]struct{})
			for _, img := range results {
				mergeOpenAIResponsesImageMeta(&img, streamMeta)
				appendOpenAIResponsesImageResultDedup(&finalResults, finalSeen, "", img)
			}
			for _, img := range pendingResults {
				mergeOpenAIResponsesImageMeta(&img, streamMeta)
				appendOpenAIResponsesImageResultDedup(&finalResults, finalSeen, "", img)
			}
			if len(finalResults) == 0 {
				outputErr := fmt.Errorf("upstream did not return image output")
				writeEvent("error", buildOpenAIImagesStreamErrorBody(outputErr.Error()))
				processDataErr, processDataDone = outputErr, true
				return
			}
			eventName := streamPrefix + ".completed"
			for _, img := range finalResults {
				key := openAIResponsesImageResultKey("", img)
				if _, exists := emitted[key]; exists {
					continue
				}
				writeEvent(eventName, buildOpenAIImagesStreamCompletedPayload(eventName, img, format, createdAt, usageRaw))
				emitted[key] = struct{}{}
			}
			imageCount = len(emitted)
			imageOutputSizes = openAIResponsesImageResultSizes(finalResults)
			processDataDone = true
		case "error", "response.failed":
			if upstreamErr := openAIImagesUpstreamErrorFromSSEPayload(dataBytes); upstreamErr != nil {
				writeEvent("error", buildOpenAIImagesStreamErrorBodyFromUpstream(upstreamErr))
				processDataErr, processDataDone = upstreamErr, true
			}
		}
	}
	processLine := func(line []byte) {
		if len(line) == 0 {
			return
		}
		sseData.AddLine(strings.TrimRight(string(line), "\r\n"), processData)
	}
	streamInterval := s.openAIImageStreamDataInterval()
	keepaliveInterval := s.openAIImageStreamKeepaliveInterval()
	if streamInterval <= 0 && keepaliveInterval <= 0 {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			processLine(line)
			if processDataErr != nil || processDataDone || err == io.EOF {
				break
			}
			if err != nil {
				return usage, imageCount, imageOutputSizes, firstTokenMs, err
			}
		}
	} else {
		type readEvent struct {
			line []byte
			err  error
		}
		events := make(chan readEvent, 16)
		done := make(chan struct{})
		sendEvent := func(event readEvent) bool {
			select {
			case events <- event:
				return true
			case <-done:
				return false
			}
		}
		var lastReadAt int64
		atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
		go func() {
			defer close(events)
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				if len(line) > 0 {
					atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
				}
				if len(line) > 0 && !sendEvent(readEvent{line: line}) {
					return
				}
				if err == io.EOF {
					return
				}
				if err != nil {
					_ = sendEvent(readEvent{err: err})
					return
				}
			}
		}()
		defer close(done)
		var intervalTicker *time.Ticker
		if streamInterval > 0 {
			intervalTicker = time.NewTicker(streamInterval)
			defer intervalTicker.Stop()
		}
		var intervalCh <-chan time.Time
		if intervalTicker != nil {
			intervalCh = intervalTicker.C
		}
		var keepaliveTicker *time.Ticker
		if keepaliveInterval > 0 {
			keepaliveTicker = time.NewTicker(keepaliveInterval)
			defer keepaliveTicker.Stop()
		}
		var keepaliveCh <-chan time.Time
		if keepaliveTicker != nil {
			keepaliveCh = keepaliveTicker.C
		}
		for {
			select {
			case event, ok := <-events:
				if !ok {
					processDataDone = true
				}
				if ok && event.err != nil {
					return usage, imageCount, imageOutputSizes, firstTokenMs, event.err
				}
				if ok {
					processLine(event.line)
				}
				if processDataErr != nil || processDataDone || !ok {
					break
				}
				continue
			case <-intervalCh:
				lastRead := time.Unix(0, atomic.LoadInt64(&lastReadAt))
				if time.Since(lastRead) >= streamInterval {
					return usage, imageCount, imageOutputSizes, firstTokenMs, fmt.Errorf("image stream data interval timeout")
				}
				continue
			case <-keepaliveCh:
				if clientDisconnected {
					continue
				}
				if _, err := sink.Write([]byte(":\n\n")); err != nil {
					clientDisconnected = true
					continue
				}
				sink.Flush()
			}
			if processDataErr != nil || processDataDone {
				break
			}
		}
	}
	sseData.Flush(processData)
	if processDataErr != nil {
		return usage, imageCount, imageOutputSizes, firstTokenMs, processDataErr
	}
	if imageCount == 0 && len(pendingResults) > 0 {
		eventName := streamPrefix + ".completed"
		for _, img := range pendingResults {
			mergeOpenAIResponsesImageMeta(&img, streamMeta)
			writeEvent(eventName, buildOpenAIImagesStreamCompletedPayload(eventName, img, format, createdAt, nil))
		}
		imageCount = len(pendingResults)
		imageOutputSizes = openAIResponsesImageResultSizes(pendingResults)
	}
	if imageCount == 0 {
		return usage, 0, nil, firstTokenMs, fmt.Errorf("stream disconnected before image generation completed")
	}
	return usage, imageCount, imageOutputSizes, firstTokenMs, nil
}

func writeOpenAIImagesStreamEventSink(sink openAIImagesExchangeSink, eventName string, payload []byte) error {
	if strings.TrimSpace(eventName) != "" {
		if _, err := sink.Write([]byte("event: " + eventName + "\n")); err != nil {
			return err
		}
	}
	if _, err := sink.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := sink.Write(payload); err != nil {
		return err
	}
	if _, err := sink.Write([]byte("\n\n")); err != nil {
		return err
	}
	sink.Flush()
	return nil
}

func (s *OpenAIGatewayService) buildOpenAIImagesOAuthRequestFromHeaders(
	ctx context.Context,
	headers http.Header,
	account *Account,
	body []byte,
	token string,
	promptCacheKey string,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, fmt.Errorf("build openai authentication headers: %w", err)
	}
	for key, values := range authHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Host = "chatgpt.com"
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, account); err != nil {
		return nil, fmt.Errorf("resolve chatgpt account headers: %w", err)
	}
	for key, values := range headers {
		if !openaiAllowedHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Del("conversation_id")
	req.Header.Del("session_id")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	originator := strings.TrimSpace(headers.Get("originator"))
	if originator == "" {
		originator = "opencode"
	}
	req.Header.Set("originator", originator)
	req.Header.Set("accept", "text/event-stream")
	if promptCacheKey != "" {
		isolated := isolateOpenAISessionID(0, promptCacheKey)
		req.Header.Set("session_id", isolated)
		req.Header.Set("conversation_id", isolated)
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		req.Header.Set("user-agent", customUA)
	}
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		req.Header.Set("user-agent", codexCLIUserAgent)
	}
	s.overrideBrowserUserAgent(ctx, account, req)
	enforceCodexIdentityHeadersWithUA(req.Header, account.GetOpenAIUserAgent())
	req.Header.Set("content-type", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

func (s *OpenAIGatewayService) forwardOpenAIImagesAPIKeyExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	account *Account,
	body []byte,
	parsed *OpenAIImagesRequest,
	channelMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	requestModel := strings.TrimSpace(parsed.Model)
	if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		requestModel = platformModel
	} else if mapped := strings.TrimSpace(channelMappedModel); mapped != "" {
		requestModel = mapped
	}
	if err := validateOpenAIImagesModel(requestModel); err != nil {
		return nil, err
	}
	upstreamModel := resolveOpenAIForwardModelWithContext(ctx, account, requestModel, "")
	if err := validateOpenAIImagesModel(upstreamModel); err != nil {
		return nil, err
	}
	forwardBody, forwardContentType, err := rewriteOpenAIImagesModel(body, parsed.ContentType, upstreamModel)
	if err != nil {
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	token, _, err := s.GetAccessToken(upstreamCtx, account)
	if err != nil {
		return nil, err
	}
	upstreamReq, err := s.buildOpenAIImagesRequestFromHeaders(
		upstreamCtx,
		exchange.Request().Header,
		account,
		forwardBody,
		forwardContentType,
		token,
		parsed.Endpoint,
	)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setRuntimeOpsUpstreamError(exchange, 0, safeErr, "")
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "request_error", Message: safeErr,
		})
		writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		respBody := s.readUpstreamErrorBody(resp)
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			shouldDisable := s.handleFailoverSideEffects(upstreamCtx, resp, account, respBody, upstreamModel)
			appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: safeUpstreamURL(upstreamReq.URL.String()), Kind: "failover", Message: upstreamMsg,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: resp.StatusCode, ResponseBody: respBody,
				RetryableOnSameAccount: !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleOpenAIImagesErrorResponseExchange(upstreamCtx, exchange, resp, respBody, account, upstreamModel)
	}

	sink := runtimeOpenAIImagesSink{exchange: exchange}
	result := &OpenAIForwardResult{
		RequestID: resp.Header.Get("x-request-id"), Model: requestModel, UpstreamModel: upstreamModel,
		Stream: parsed.Stream, ResponseHeaders: resp.Header.Clone(), Duration: time.Since(startTime),
		ImageCount: parsed.N, ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size,
	}
	if parsed.Stream && isEventStreamResponse(resp.Header) {
		usage, imageCount, outputSizes, firstTokenMs, streamErr := s.handleOpenAIImagesStreamingResponseSink(resp, sink, startTime)
		result.Usage = usage
		result.ImageCount = imageCount
		result.ImageOutputSizes = outputSizes
		result.FirstTokenMs = firstTokenMs
		result.Duration = time.Since(startTime)
		if streamErr != nil {
			return result, streamErr
		}
		return result, nil
	}
	usage, imageCount, outputSizes, bodyErr := s.handleOpenAIImagesNonStreamingResponseExchange(resp, exchange)
	if bodyErr != nil {
		return nil, bodyErr
	}
	result.Usage = usage
	if imageCount > 0 {
		result.ImageCount = imageCount
	}
	result.ImageOutputSizes = outputSizes
	result.Duration = time.Since(startTime)
	return result, nil
}

func (s *OpenAIGatewayService) handleOpenAIImagesNonStreamingResponseExchange(
	resp *http.Response,
	exchange gatewayruntime.HTTPExchange,
) (OpenAIUsage, int, []string, error) {
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			setRuntimeOpsUpstreamError(exchange, http.StatusBadGateway, "upstream response too large", "")
			writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream response too large")
		}
		return OpenAIUsage{}, 0, nil, err
	}
	responseheaders.WriteFilteredHeaders(exchange.Header(), resp.Header, s.responseHeaderFilter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	exchange.Header().Set("Content-Type", contentType)
	exchange.WriteHeader(resp.StatusCode)
	if _, err := exchange.Write(body); err != nil {
		return OpenAIUsage{}, 0, nil, err
	}
	usage, _ := extractOpenAIUsageFromJSONBytes(body)
	return usage, extractOpenAIImageCountFromJSONBytes(body), collectOpenAIResponseImageOutputSizesFromJSONBytes(body), nil
}

func writeRuntimeOpenAIImagesError(exchange gatewayruntime.HTTPExchange, status int, errType, message string) {
	if exchange == nil || exchange.Written() {
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, _ = exchange.Write([]byte(fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, errType, message)))
}

func (s *OpenAIGatewayService) handleOpenAIImagesErrorResponseExchange(
	ctx context.Context,
	exchange gatewayruntime.HTTPExchange,
	resp *http.Response,
	body []byte,
	account *Account,
	requestedModel string,
) (*OpenAIForwardResult, error) {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setRuntimeOpsUpstreamError(exchange, resp.StatusCode, upstreamMsg, upstreamDetail)
	if status, errType, errMsg, matched := applyErrorPassthroughRuleExchange(
		exchange,
		account.Platform,
		resp.StatusCode,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		upErr := &OpenAIImagesUpstreamError{
			StatusCode: status, ErrorType: errType, Message: errMsg,
			UpstreamRequestID: strings.TrimSpace(resp.Header.Get("x-request-id")),
		}
		writeRuntimeOpenAIImagesUpstreamError(exchange, upErr)
		return nil, upErr
	}
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			Kind: "http_error", Message: upstreamMsg, Detail: upstreamDetail,
		})
		upErr := &OpenAIImagesUpstreamError{
			StatusCode: http.StatusInternalServerError, ErrorType: "upstream_error", Message: "Upstream gateway error",
			UpstreamRequestID: strings.TrimSpace(resp.Header.Get("x-request-id")),
		}
		writeRuntimeOpenAIImagesUpstreamError(exchange, upErr)
		return nil, upErr
	}
	shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, requestedModel)
	kind := "http_error"
	if shouldDisable {
		kind = "failover"
	}
	appendRuntimeOpsUpstreamError(exchange, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
		Kind: kind, Message: upstreamMsg, Detail: upstreamDetail,
	})
	if shouldDisable {
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: body}
	}
	upErr := openAIImagesUpstreamErrorFromHTTP(resp.StatusCode, resp.Header, body)
	writeRuntimeOpenAIImagesUpstreamError(exchange, upErr)
	return nil, upErr
}

func applyErrorPassthroughRuleExchange(
	exchange gatewayruntime.HTTPExchange,
	platform string,
	upstreamStatus int,
	responseBody []byte,
	defaultStatus int,
	defaultErrType string,
	defaultErrMsg string,
) (status int, errType string, errMsg string, matched bool) {
	status, errType, errMsg = defaultStatus, defaultErrType, defaultErrMsg
	if exchange == nil {
		return status, errType, errMsg, false
	}
	value, ok := exchange.State(errorPassthroughServiceContextKey)
	if !ok {
		return status, errType, errMsg, false
	}
	svc, ok := value.(*ErrorPassthroughService)
	if !ok || svc == nil {
		return status, errType, errMsg, false
	}
	rule := svc.MatchRule(platform, upstreamStatus, responseBody)
	if rule == nil {
		return status, errType, errMsg, false
	}
	status = upstreamStatus
	if !rule.PassthroughCode && rule.ResponseCode != nil {
		status = *rule.ResponseCode
	}
	errMsg = ExtractUpstreamErrorMessage(responseBody)
	if !rule.PassthroughBody && rule.CustomMessage != nil {
		errMsg = *rule.CustomMessage
	}
	if rule.SkipMonitoring {
		exchange.SetState(OpsSkipPassthroughKey, true)
	}
	return status, "upstream_error", errMsg, true
}

func writeRuntimeOpenAIImagesUpstreamError(exchange gatewayruntime.HTTPExchange, upstreamErr *OpenAIImagesUpstreamError) {
	if exchange == nil || exchange.Written() || upstreamErr == nil {
		return
	}
	errorObject := map[string]any{
		"type":    upstreamErr.clientErrorType(),
		"message": upstreamErr.clientMessage(),
	}
	if code := strings.TrimSpace(upstreamErr.Code); code != "" {
		errorObject["code"] = code
	}
	if param := strings.TrimSpace(upstreamErr.Param); param != "" {
		errorObject["param"] = param
	}
	body, err := json.Marshal(map[string]any{"error": errorObject})
	if err != nil {
		writeRuntimeOpenAIImagesError(exchange, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(upstreamErr.clientStatusCode())
	_, _ = exchange.Write(body)
}

func (s *OpenAIGatewayService) handleOpenAIImagesStreamingResponseSink(
	resp *http.Response,
	sink openAIImagesExchangeSink,
	startTime time.Time,
) (OpenAIUsage, int, []string, *int, error) {
	responseheaders.WriteFilteredHeaders(sink.Header(), resp.Header, s.responseHeaderFilter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/event-stream"
	}
	sink.Header().Set("Content-Type", contentType)
	sink.WriteHeader(resp.StatusCode)

	usage := OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	clientDisconnected := false
	lastDownstreamWriteAt := time.Now()
	var fallbackBody bytes.Buffer
	fallbackBytes := int64(0)
	fallbackLimit := resolveUpstreamResponseReadLimit(s.cfg)
	seenSSEData := false
	fallbackTooLarge := false
	var sseData openAISSEDataAccumulator
	processSSEData := func(dataBytes []byte) {
		seenSSEData = true
		fallbackBody.Reset()
		fallbackBytes = 0
		mergeOpenAIUsage(&usage, dataBytes)
		imageCounter.AddSSEData(dataBytes)
	}
	flushSSEEvent := func() { sseData.Flush(processSSEData) }
	processLine := func(line []byte) {
		if len(line) == 0 {
			return
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if !clientDisconnected {
			if _, writeErr := sink.Write(line); writeErr != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Images stream client disconnected, continue draining upstream for billing")
			} else {
				sink.Flush()
				lastDownstreamWriteAt = time.Now()
			}
		}
		trimmedLine := strings.TrimRight(string(line), "\r\n")
		if _, ok := extractOpenAISSEDataLine(trimmedLine); ok || strings.TrimSpace(trimmedLine) == "" {
			sseData.AddLine(trimmedLine, processSSEData)
			return
		}
		if !seenSSEData && !fallbackTooLarge {
			fallbackBytes += int64(len(line))
			if fallbackBytes <= fallbackLimit {
				_, _ = fallbackBody.Write(line)
			} else {
				fallbackTooLarge = true
				fallbackBody.Reset()
			}
		}
	}
	finalizeFallbackBody := func() {
		if seenSSEData || fallbackBody.Len() == 0 {
			return
		}
		body := bytes.TrimSpace(fallbackBody.Bytes())
		if len(body) == 0 {
			return
		}
		mergeOpenAIUsage(&usage, body)
		imageCounter.AddJSONResponse(body)
	}
	streamInterval := s.openAIImageStreamDataInterval()
	keepaliveInterval := s.openAIImageStreamKeepaliveInterval()
	if streamInterval <= 0 && keepaliveInterval <= 0 {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			processLine(line)
			if err == io.EOF {
				break
			}
			if err != nil {
				flushSSEEvent()
				return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, err
			}
		}
		flushSSEEvent()
		finalizeFallbackBody()
		return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, nil
	}

	type readEvent struct {
		line []byte
		err  error
	}
	events := make(chan readEvent, 16)
	done := make(chan struct{})
	sendEvent := func(ev readEvent) bool {
		select {
		case events <- ev:
			return true
		case <-done:
			return false
		}
	}
	var lastReadAt int64
	atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
	go func() {
		defer close(events)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				atomic.StoreInt64(&lastReadAt, time.Now().UnixNano())
			}
			if len(line) > 0 && !sendEvent(readEvent{line: line}) {
				return
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				_ = sendEvent(readEvent{err: err})
				return
			}
		}
	}()
	defer close(done)
	var intervalTicker *time.Ticker
	if streamInterval > 0 {
		intervalTicker = time.NewTicker(streamInterval)
		defer intervalTicker.Stop()
	}
	var intervalCh <-chan time.Time
	if intervalTicker != nil {
		intervalCh = intervalTicker.C
	}
	var keepaliveTicker *time.Ticker
	if keepaliveInterval > 0 {
		keepaliveTicker = time.NewTicker(keepaliveInterval)
		defer keepaliveTicker.Stop()
	}
	var keepaliveCh <-chan time.Time
	if keepaliveTicker != nil {
		keepaliveCh = keepaliveTicker.C
	}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				flushSSEEvent()
				finalizeFallbackBody()
				return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, nil
			}
			if ev.err != nil {
				flushSSEEvent()
				return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, ev.err
			}
			processLine(ev.line)
		case <-intervalCh:
			lastRead := time.Unix(0, atomic.LoadInt64(&lastReadAt))
			if time.Since(lastRead) < streamInterval {
				continue
			}
			if clientDisconnected {
				return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, fmt.Errorf("image stream incomplete after timeout")
			}
			return usage, imageCounter.Count(), imageCounter.Sizes(), firstTokenMs, fmt.Errorf("image stream data interval timeout")
		case <-keepaliveCh:
			if clientDisconnected || time.Since(lastDownstreamWriteAt) < keepaliveInterval {
				continue
			}
			if _, writeErr := sink.Write([]byte(":\n\n")); writeErr != nil {
				clientDisconnected = true
				continue
			}
			sink.Flush()
			lastDownstreamWriteAt = time.Now()
		}
	}
}
