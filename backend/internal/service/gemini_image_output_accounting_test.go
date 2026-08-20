package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageOutputAccountingUsesActualImagesAndMaxChunk(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	beginGeminiImageOutputObservation(c)
	observeGeminiImageOutputs(c, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"a"}}]}}]}`))
	observeGeminiImageOutputs(c, []byte(`{"candidates":[{"content":{"parts":[{"inline_data":{"mime_type":"image/png","data":"a"}},{"inlineData":{"mimeType":"image/jpeg","data":"b"}}]}}]}`))

	require.Equal(t, 2, resolveGeminiImageCount(c, "custom-client-model", "custom-upstream-model"))
}

func TestGeminiImageOutputAccountingResetsBetweenAttempts(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	beginGeminiImageOutputObservation(c)
	observeGeminiImageOutputs(c, []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"a"}}]}}]}`))
	require.Equal(t, 1, resolveGeminiImageCount(c, "custom", "custom"))

	beginGeminiImageOutputObservation(c)
	require.Zero(t, resolveGeminiImageCount(c, "custom", "custom"))
}
