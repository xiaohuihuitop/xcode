package sub2api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
)

var ErrExecutorUnavailable = errors.New("sub2api openai executor is unavailable")

// AccountSelection is a transport-neutral account lease. The account entity
// remains owned by the Service Port; the Driver only sees the scalar ID and a
// closure that performs one upstream attempt.
type AccountSelection struct {
	ID          int64
	Platform    string
	AccountType string
	Forward     func(context.Context, v1.Request) (ForwardResult, error)
	Release     func()
}

// OpenAIRuntimePort is the explicit boundary between the Sub2API Driver and
// the existing account/OAuth/scheduling service. It exposes no Handler,
// Gin, Ent or ProductCore types.
type OpenAIRuntimePort interface {
	Select(context.Context, v1.Request, map[int64]struct{}, string) (AccountSelection, error)
	MaxSwitches() int
}

type scheduleReporter interface {
	ReportScheduleResult(context.Context, int64, string, bool, *int)
}

type switchRecorder interface {
	RecordAccountSwitch(context.Context)
}

type oauth429FailoverStopper interface {
	ShouldStopOAuth429Failover(context.Context, AccountSelection, int, int, *FailoverState) bool
}

type FailoverState struct {
	OAuth429FollowupPending bool
}

type ForwardResult struct {
	StatusCode       int
	ResponseHeaders  map[string][]string
	Body             []byte
	Streamed         bool
	AccountID        int64
	UpstreamEndpoint string
	UpstreamModel    string
	Usage            v1.UsageFacts
}

// RetryError carries only retry policy and safe status metadata across the
// Driver port. Cause is retained for diagnostics and unwrapping, but no
// credential or request body is added here.
type RetryError struct {
	StatusCode               int
	RetryNextAccount         bool
	RetrySameAccount         bool
	ReportSchedule           bool
	FirstTokenMs             *int
	SafeToFailoverAfterWrite bool
	ClientErrorType          string
	ClientErrorMessage       string
	Cause                    error
}

func (e *RetryError) Error() string {
	if e == nil {
		return "sub2api upstream retry error"
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return fmt.Sprintf("sub2api upstream retry error: %d", e.StatusCode)
}

func (e *RetryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type OpenAIExecutor struct {
	Port       OpenAIRuntimePort
	retryDelay func(context.Context) error
}

const sameAccountRetryLimit = 2

// Dispatch lets the endpoint executor participate directly as a Driver when
// a single endpoint family is composed into the local RuntimeBridge.
func (e OpenAIExecutor) Dispatch(ctx context.Context, request v1.Request, sink runtimebridge.EventSink) (v1.Result, error) {
	return e.Execute(ctx, request, sink)
}

func (e OpenAIExecutor) Execute(ctx context.Context, request v1.Request, sink runtimebridge.EventSink) (v1.Result, error) {
	if sink == nil || e.Port == nil {
		return v1.Result{}, ErrExecutorUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := request.Validate(); err != nil {
		return v1.Result{}, err
	}
	model := strings.TrimSpace(request.Platform.RequestedModel)
	if model == "" {
		model = requestModel(request.Payload)
	}
	if model == "" {
		err := errors.New("model is required")
		return e.fail(ctx, request, sink, http.StatusBadRequest, err, nil)
	}
	capability := endpointCapability(request.Endpoint)
	excluded := make(map[int64]struct{})
	sameAccountRetries := make(map[int64]int)
	attemptedAccountIDs := make([]int64, 0, 2)
	maxSwitches := e.Port.MaxSwitches()
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	var lastErr error
	var failoverState FailoverState
	for switchCount := 0; switchCount <= maxSwitches; switchCount++ {
		if err := ctx.Err(); err != nil {
			return e.fail(ctx, request, sink, http.StatusRequestTimeout, err, attemptedAccountIDs)
		}
		selection, selectErr := e.Port.Select(ctx, request, excluded, capability)
		if selectErr != nil || selection.ID <= 0 || selection.Forward == nil {
			lastErr = selectErr
			if lastErr == nil {
				lastErr = errors.New("sub2api account selection is unavailable")
			}
			break
		}
		attemptedAccountIDs = append(attemptedAccountIDs, selection.ID)
		exchange, _ := runtimebridge.LocalExchangeFromContext(ctx)
		writtenBeforeForward := 0
		if exchange != nil {
			writtenBeforeForward = exchange.Size()
		}
		result, forwardErr := selection.Forward(ctx, request)
		if selection.Release != nil {
			selection.Release()
		}
		if forwardErr == nil && result.StatusCode >= http.StatusOK && result.StatusCode < http.StatusBadRequest {
			result = normalizeForwardResult(request, selection.ID, model, result)
			usage := result.Usage
			usage.TerminalStatus = firstNonEmpty(usage.TerminalStatus, "success")
			usage.AccountID = selection.ID
			if usage.Endpoint == "" {
				usage.Endpoint = string(request.Endpoint)
			}
			if usage.RequestedModel == "" {
				usage.RequestedModel = model
			}
			if usage.UpstreamModel == "" {
				usage.UpstreamModel = firstNonEmpty(result.UpstreamModel, request.Platform.UpstreamModel)
			}
			if reporter, ok := e.Port.(scheduleReporter); ok {
				var firstTokenMs *int
				if usage.FirstTokenMilliseconds > 0 {
					value := int(usage.FirstTokenMilliseconds)
					firstTokenMs = &value
				}
				reporter.ReportScheduleResult(ctx, selection.ID, model, true, firstTokenMs)
			}
			if err := sink.Publish(ctx, v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: &usage}); err != nil {
				return v1.Result{}, err
			}
			result.Usage = usage
			result.AccountID = selection.ID
			return toContractResult(result), nil
		}

		lastErr = forwardErr
		retry, ok := forwardErr.(*RetryError)
		if retry != nil && retry.ReportSchedule {
			if reporter, reporterOK := e.Port.(scheduleReporter); reporterOK {
				reporter.ReportScheduleResult(ctx, selection.ID, model, false, retry.FirstTokenMs)
			}
		}
		if !ok || retry == nil || !retry.RetryNextAccount {
			break
		}
		if exchange != nil && exchange.Size() != writtenBeforeForward && !retry.SafeToFailoverAfterWrite {
			break
		}
		if retry.RetrySameAccount && sameAccountRetries[selection.ID] < sameAccountRetryLimit {
			sameAccountRetries[selection.ID]++
			if err := e.waitSameAccountRetry(ctx); err != nil {
				return e.fail(ctx, request, sink, http.StatusRequestTimeout, err, attemptedAccountIDs)
			}
			continue
		}
		excluded[selection.ID] = struct{}{}
		if recorder, ok := e.Port.(switchRecorder); ok {
			recorder.RecordAccountSwitch(ctx)
		}
		if stopper, ok := e.Port.(oauth429FailoverStopper); ok && stopper.ShouldStopOAuth429Failover(ctx, selection, retry.StatusCode, switchCount+1, &failoverState) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("sub2api upstream request failed")
	}
	status := http.StatusBadGateway
	if retry, ok := lastErr.(*RetryError); ok && retry.StatusCode > 0 {
		status = retry.StatusCode
	}
	return e.fail(ctx, request, sink, status, lastErr, attemptedAccountIDs)
}

func (e OpenAIExecutor) fail(ctx context.Context, request v1.Request, sink runtimebridge.EventSink, status int, err error, attemptedAccountIDs []int64) (v1.Result, error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = 499
	}
	if exchange, ok := runtimebridge.LocalExchangeFromContext(ctx); ok && !exchange.Written() {
		_ = writeOpenAIError(exchange, request.Endpoint, status, err)
	}
	runtimeErr := &v1.RuntimeError{Category: "upstream_error", Message: safeRuntimeErrorMessage(err, status), UpstreamStatus: status, AttemptedAccountIDs: append([]int64(nil), attemptedAccountIDs...)}
	if status >= 400 && status < 500 {
		runtimeErr.Category = "request_error"
	}
	if publishErr := sink.Publish(ctx, v1.Event{Sequence: 1, Kind: v1.EventRuntimeFailed, Error: runtimeErr}); publishErr != nil {
		return v1.Result{StatusCode: status}, errors.Join(err, publishErr)
	}
	return v1.Result{StatusCode: status}, err
}

func safeRuntimeErrorMessage(err error, status int) string {
	if status == 499 {
		return "Client closed request"
	}
	if status >= 500 {
		return "Upstream request failed"
	}
	if err == nil {
		return http.StatusText(status)
	}
	if retry, ok := err.(*RetryError); ok && strings.TrimSpace(retry.ClientErrorMessage) != "" {
		return retry.ClientErrorMessage
	}
	return err.Error()
}

func (e OpenAIExecutor) waitSameAccountRetry(ctx context.Context) error {
	if e.retryDelay != nil {
		return e.retryDelay(ctx)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeForwardResult(request v1.Request, accountID int64, model string, result ForwardResult) ForwardResult {
	result.AccountID = accountID
	if result.StatusCode == 0 {
		result.StatusCode = http.StatusOK
	}
	if result.UpstreamModel == "" {
		result.UpstreamModel = request.Platform.UpstreamModel
	}
	if result.Usage.Adapter == "" {
		result.Usage.Adapter = request.Platform.RuntimeAdapter
	}
	if result.Usage.RequestedModel == "" {
		result.Usage.RequestedModel = model
	}
	return result
}

func toContractResult(result ForwardResult) v1.Result {
	return v1.Result{
		StatusCode:       result.StatusCode,
		ResponseHeaders:  cloneHeaders(result.ResponseHeaders),
		Body:             append([]byte(nil), result.Body...),
		Streamed:         result.Streamed,
		AccountID:        result.AccountID,
		UpstreamEndpoint: result.UpstreamEndpoint,
		UpstreamModel:    result.UpstreamModel,
		Usage:            result.Usage,
	}
}

func endpointCapability(endpoint v1.Endpoint) string {
	switch endpoint {
	case v1.EndpointResponses:
		return "responses"
	case v1.EndpointMessages:
		return "messages"
	case v1.EndpointEmbeddings:
		return "embeddings"
	case v1.EndpointAlphaSearch:
		return "alpha_search"
	case v1.EndpointImages:
		return "images"
	default:
		return "chat_completions"
	}
}

func requestModel(payload []byte) string {
	var body struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Model)
}

func writeOpenAIError(exchange gatewayruntime.HTTPExchange, endpoint v1.Endpoint, status int, err error) error {
	if exchange == nil || exchange.Written() {
		return nil
	}
	errType, message := runtimeClientError(err, status)
	bodyValue := map[string]any{"error": map[string]string{"type": errType, "message": message}}
	if endpoint == v1.EndpointMessages {
		bodyValue = map[string]any{"type": "error", "error": map[string]string{"type": errType, "message": message}}
	} else if endpoint == v1.EndpointCountTokens {
		bodyValue = map[string]any{"type": "error", "error": map[string]string{"type": errType, "message": message}}
	}
	body, marshalErr := json.Marshal(bodyValue)
	if marshalErr != nil {
		return marshalErr
	}
	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(status)
	_, writeErr := exchange.Write(body)
	return writeErr
}

func runtimeClientError(err error, status int) (string, string) {
	errType := "upstream_error"
	message := safeRuntimeErrorMessage(err, status)
	if retry, ok := err.(*RetryError); ok {
		if strings.TrimSpace(retry.ClientErrorType) != "" {
			errType = retry.ClientErrorType
		}
		if strings.TrimSpace(retry.ClientErrorMessage) != "" {
			message = retry.ClientErrorMessage
		}
	}
	return errType, message
}

func cloneHeaders(headers map[string][]string) map[string][]string {
	if headers == nil {
		return nil
	}
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ EndpointExecutor = (*OpenAIExecutor)(nil)
