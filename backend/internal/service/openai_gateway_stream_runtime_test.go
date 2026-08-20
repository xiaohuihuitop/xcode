//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIStreamFailoverErrorExchangeRecordsRuntimeFacts(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	account := &Account{ID: 17, Platform: PlatformOpenAI, Name: "oauth-17", Type: AccountTypeOAuth}

	err := (&OpenAIGatewayService{}).newOpenAIStreamFailoverErrorExchange(
		exchange,
		account,
		false,
		"upstream-stream-17",
		[]byte(`{"response":{"error":{"code":"rate_limit_exceeded","message":"slow down"}}}`),
		"slow down",
		http.Header{"X-Request-Id": []string{"upstream-stream-17"}},
	)

	require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	require.Equal(t, "upstream-stream-17", err.ResponseHeaders.Get("X-Request-Id"))
	require.Equal(t, http.StatusTooManyRequests, exchange.state[OpsUpstreamStatusCodeKey])
	require.Equal(t, "slow down", exchange.state[OpsUpstreamErrorMessageKey])
	events, ok := exchange.state[OpsUpstreamErrorsKey].([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, int64(17), events[0].AccountID)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "slow down", events[0].Message)
}

func TestOpenAIStreamFailedErrorPassthroughRuleExchangeUsesRuntimeState(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	code := http.StatusBadRequest
	passthrough := newTestService([]*model.ErrorPassthroughRule{{
		ID: 1, Enabled: true, Platforms: []string{PlatformOpenAI}, MatchMode: model.MatchModeAny,
		Keywords: []string{"quota denied"}, ResponseCode: &code, PassthroughBody: true,
	}})
	exchange.SetState(errorPassthroughServiceContextKey, passthrough)

	status, errType, message, matched := applyOpenAIStreamFailedErrorPassthroughRuleExchange(
		exchange,
		PlatformOpenAI,
		[]byte(`{"error":{"message":"quota denied"}}`),
		"quota denied",
	)

	require.True(t, matched)
	require.Equal(t, "upstream_error", errType)
	require.Equal(t, "quota denied", message)
	require.NotEqual(t, http.StatusBadGateway, status)
	require.NotNil(t, exchange)
}

func TestNewOpenAIFirstOutputTimeoutErrorExchangeRecordsFailover(t *testing.T) {
	exchange := newRuntimeExchangeTestDouble(t, nil)
	account := &Account{ID: 18, Platform: PlatformOpenAI, Name: "oauth-18"}

	err := (&OpenAIGatewayService{}).newOpenAIFirstOutputTimeoutErrorExchange(
		context.Background(), exchange, account, contextStartForTest(), "gpt-5.6", "high", time.Second, "semantic_output", http.Header{"X-Request-Id": []string{"timeout-18"}},
	)

	require.Equal(t, http.StatusGatewayTimeout, err.StatusCode)
	events, ok := exchange.state[OpsUpstreamErrorsKey].([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "first_output_timeout", events[0].Kind)
	require.Equal(t, "timeout-18", events[0].UpstreamRequestID)
}

func contextStartForTest() time.Time {
	return time.Now().Add(-10 * time.Millisecond)
}
