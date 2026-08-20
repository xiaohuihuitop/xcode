//go:build unit

package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

type cancelReadCloser struct{}

func (cancelReadCloser) Read([]byte) (int, error) { return 0, context.Canceled }
func (cancelReadCloser) Close() error             { return nil }

type failingGinWriter struct {
	gin.ResponseWriter
	failAfter int
	writes    int
}

func newOpenAIWSV2TestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}

func (w *failingGinWriter) Write(payload []byte) (int, error) {
	if w.writes >= w.failAfter {
		return 0, errors.New("write failed")
	}
	w.writes++
	return w.ResponseWriter.Write(payload)
}

type stubGatewayCache struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
}

func (c *stubGatewayCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	if id, ok := c.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (c *stubGatewayCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	if c.sessionBindings == nil {
		c.sessionBindings = make(map[string]int64)
	}
	c.sessionBindings[sessionHash] = accountID
	return nil
}

func (c *stubGatewayCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *stubGatewayCache) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	if c.sessionBindings == nil {
		return nil
	}
	if c.deletedSessions == nil {
		c.deletedSessions = make(map[string]int)
	}
	c.deletedSessions[sessionHash]++
	delete(c.sessionBindings, sessionHash)
	return nil
}
