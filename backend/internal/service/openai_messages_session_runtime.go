package service

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// openAICompatSessionResponseKeyForAPIKey builds the same stable namespace as
// the legacy Gin path, but takes the scalar API key id supplied by the runtime
// request instead of reading it from a transport context.
func openAICompatSessionResponseKeyForAPIKey(account *Account, apiKeyID int64, promptCacheKey string) string {
	key := strings.TrimSpace(promptCacheKey)
	if account == nil || key == "" {
		return ""
	}
	return strings.Join([]string{
		strconv.FormatInt(account.ID, 10),
		strconv.FormatInt(apiKeyID, 10),
		key,
	}, "\x00")
}

func (s *OpenAIGatewayService) getOpenAICompatSessionResponseIDRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) string {
	binding, ok := s.openAICompatSessionBindingRuntime(account, apiKeyID, promptCacheKey)
	if !ok || binding.ContinuationDisabled {
		return ""
	}
	if strings.TrimSpace(binding.ResponseID) == "" {
		return ""
	}
	return strings.TrimSpace(binding.ResponseID)
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionResponseIDRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
	responseID string,
) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKeyForAPIKey(account, apiKeyID, promptCacheKey)
	id := strings.TrimSpace(responseID)
	if key == "" || id == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		ResponseID: id,
		ExpiresAt:  time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if raw, ok := s.openaiCompatSessionResponses.Load(key); ok {
		if existing, ok := raw.(openAICompatSessionResponseBinding); ok {
			if existing.ContinuationDisabled {
				existing.ResponseID = ""
				existing.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
				s.openaiCompatSessionResponses.Store(key, existing)
				return
			}
			binding.TurnState = existing.TurnState
		}
	}
	s.openaiCompatSessionResponses.Store(key, binding)
}

func (s *OpenAIGatewayService) deleteOpenAICompatSessionResponseIDRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKeyForAPIKey(account, apiKeyID, promptCacheKey)
	if key == "" {
		return
	}
	raw, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return
	}
	binding, ok := raw.(openAICompatSessionResponseBinding)
	if !ok {
		s.openaiCompatSessionResponses.Delete(key)
		return
	}
	binding.ResponseID = ""
	if strings.TrimSpace(binding.TurnState) == "" && !binding.ContinuationDisabled {
		s.openaiCompatSessionResponses.Delete(key)
		return
	}
	binding.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
	s.openaiCompatSessionResponses.Store(key, binding)
}

func (s *OpenAIGatewayService) disableOpenAICompatSessionContinuationRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKeyForAPIKey(account, apiKeyID, promptCacheKey)
	if key == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		ContinuationDisabled: true,
		ExpiresAt:            time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if raw, ok := s.openaiCompatSessionResponses.Load(key); ok {
		if existing, ok := raw.(openAICompatSessionResponseBinding); ok {
			binding.TurnState = existing.TurnState
		}
	}
	s.openaiCompatSessionResponses.Store(key, binding)
}

func (s *OpenAIGatewayService) isOpenAICompatSessionContinuationDisabledRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) bool {
	binding, ok := s.openAICompatSessionBindingRuntime(account, apiKeyID, promptCacheKey)
	return ok && binding.ContinuationDisabled
}

func (s *OpenAIGatewayService) getOpenAICompatSessionTurnStateRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) string {
	binding, ok := s.openAICompatSessionBindingRuntime(account, apiKeyID, promptCacheKey)
	if !ok {
		return ""
	}
	return strings.TrimSpace(binding.TurnState)
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionTurnStateRuntime(
	_ context.Context,
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
	turnState string,
) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKeyForAPIKey(account, apiKeyID, promptCacheKey)
	state := strings.TrimSpace(turnState)
	if key == "" || state == "" {
		return
	}
	binding := openAICompatSessionResponseBinding{
		TurnState: state,
		ExpiresAt: time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	if raw, ok := s.openaiCompatSessionResponses.Load(key); ok {
		if existing, ok := raw.(openAICompatSessionResponseBinding); ok {
			binding.ResponseID = existing.ResponseID
			binding.ContinuationDisabled = existing.ContinuationDisabled
		}
	}
	s.openaiCompatSessionResponses.Store(key, binding)
}

func (s *OpenAIGatewayService) openAICompatSessionBindingRuntime(
	account *Account,
	apiKeyID int64,
	promptCacheKey string,
) (openAICompatSessionResponseBinding, bool) {
	if s == nil {
		return openAICompatSessionResponseBinding{}, false
	}
	key := openAICompatSessionResponseKeyForAPIKey(account, apiKeyID, promptCacheKey)
	if key == "" {
		return openAICompatSessionResponseBinding{}, false
	}
	raw, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return openAICompatSessionResponseBinding{}, false
	}
	binding, ok := raw.(openAICompatSessionResponseBinding)
	if !ok {
		s.openaiCompatSessionResponses.Delete(key)
		return openAICompatSessionResponseBinding{}, false
	}
	if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
		s.openaiCompatSessionResponses.Delete(key)
		return openAICompatSessionResponseBinding{}, false
	}
	return binding, true
}
