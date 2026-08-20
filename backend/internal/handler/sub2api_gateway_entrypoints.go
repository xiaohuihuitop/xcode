package handler

import (
	"bytes"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func shouldPreserveDirectImageRejection(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	if _, ok := service.GatewayPlatformAssetContextFromContext(c.Request.Context()); ok {
		return false
	}
	if c.Request.Method != http.MethodPost || c.Request.Body == nil {
		return false
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	model := gjson.GetBytes(body, "model").String()
	return service.IsGPTImageGenerationModel(model)
}
