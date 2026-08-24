package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

const openAICompatReasoningAPIKeyIDStateKey = "openai_compat_reasoning_api_key_id"

type openAICompatReasoningCacheEntry struct {
	Text      string
	ExpiresAt time.Time
}

func openAICompatReasoningCacheKey(account *Account, apiKeyID int64, itemID string) string {
	if account == nil || strings.TrimSpace(itemID) == "" ||
		(account.Type == AccountTypeAPIKey && apiKeyID <= 0) {
		return ""
	}
	return strings.Join([]string{
		strconv.FormatInt(account.ID, 10),
		strconv.FormatInt(apiKeyID, 10),
		strings.TrimSpace(itemID),
	}, "\x00")
}

func (s *OpenAIGatewayService) reasoningCacheTTL() time.Duration {
	if s == nil {
		return time.Hour
	}
	if ttl := s.openAIWSResponseStickyTTL(); ttl > 0 {
		return ttl
	}
	return time.Hour
}

func (s *OpenAIGatewayService) cacheReasoningContent(account *Account, apiKeyID int64, itemID, text string) {
	if s == nil {
		return
	}
	key := openAICompatReasoningCacheKey(account, apiKeyID, itemID)
	text = strings.TrimSpace(text)
	if key == "" || text == "" {
		return
	}
	s.openaiCompatReasoningCache.Store(key, openAICompatReasoningCacheEntry{
		Text:      text,
		ExpiresAt: time.Now().Add(s.reasoningCacheTTL()),
	})
}

func (s *OpenAIGatewayService) lookupReasoningContent(account *Account, apiKeyID int64, itemID string) string {
	if s == nil {
		return ""
	}
	key := openAICompatReasoningCacheKey(account, apiKeyID, itemID)
	if key == "" {
		return ""
	}
	raw, ok := s.openaiCompatReasoningCache.Load(key)
	if !ok {
		return ""
	}
	entry, ok := raw.(openAICompatReasoningCacheEntry)
	if !ok || (!entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt)) {
		s.openaiCompatReasoningCache.Delete(key)
		return ""
	}
	return entry.Text
}

func reasoningCacheAPIKeyID(exchange interface{ State(string) (any, bool) }) int64 {
	if exchange == nil {
		return 0
	}
	raw, ok := exchange.State(openAICompatReasoningAPIKeyIDStateKey)
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func reasoningCacheAPIKeyIDFromGin(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	return getAPIKeyIDFromContext(c)
}

func (s *OpenAIGatewayService) cacheResponsesRequestReasoning(req *apicompat.ResponsesRequest, account *Account, apiKeyID int64) {
	if req == nil || len(req.Input) == 0 {
		return
	}
	var items []json.RawMessage
	if err := json.Unmarshal(req.Input, &items); err != nil {
		return
	}
	for _, raw := range items {
		id, text, ok := apicompat.ExtractResponsesReasoningItem(raw)
		if ok {
			s.cacheReasoningContent(account, apiKeyID, id, text)
		}
	}
}

func (s *OpenAIGatewayService) cacheResponsesOutputReasoning(resp *apicompat.ResponsesResponse, account *Account, apiKeyID int64) {
	if resp == nil {
		return
	}
	for _, item := range resp.Output {
		if item.Type != "reasoning" || strings.TrimSpace(item.ID) == "" {
			continue
		}
		var parts []string
		for _, summary := range item.Summary {
			if text := strings.TrimSpace(summary.Text); text != "" {
				parts = append(parts, text)
			}
		}
		s.cacheReasoningContent(account, apiKeyID, item.ID, strings.Join(parts, "\n"))
	}
}

func (s *OpenAIGatewayService) responsesToChatCompletionsRequestWithReasoningCache(
	req *apicompat.ResponsesRequest,
	account *Account,
	apiKeyID int64,
) (*apicompat.ChatCompletionsRequest, error) {
	s.cacheResponsesRequestReasoning(req, account, apiKeyID)
	return apicompat.ResponsesToChatCompletionsRequestWithOptions(req, &apicompat.ResponsesToChatOptions{
		ReasoningContentByID: func(itemID string) string {
			return s.lookupReasoningContent(account, apiKeyID, itemID)
		},
	})
}
