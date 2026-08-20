//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGinHTTPExchangePreservesResponseAndStateSurface(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	exchange := NewGinHTTPExchange(c)

	exchange.SetState("actual_endpoint", "/v1/responses")
	got, ok := exchange.State("actual_endpoint")
	require.True(t, ok)
	require.Equal(t, "/v1/responses", got)

	exchange.Header().Set("Content-Type", "application/json")
	exchange.WriteHeader(http.StatusAccepted)
	_, err := exchange.Write([]byte(`{"ok":true}`))
	exchange.Flush()

	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.True(t, exchange.Written())
	require.Greater(t, exchange.Size(), 0)
}
