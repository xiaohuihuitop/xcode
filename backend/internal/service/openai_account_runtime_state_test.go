//go:build unit

package service

import (
	"net/http"
	"testing"
)

func TestShouldStopOpenAIOAuth429FailoverRuntimeCarriesFollowupState(t *testing.T) {
	svc := &OpenAIGatewayService{}
	pending := false
	account := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth}
	if svc.ShouldStopOpenAIOAuth429FailoverRuntime(account, http.StatusTooManyRequests, 2, &pending) {
		t.Fatal("first Grok OAuth 429 should arm follow-up instead of stopping")
	}
	if !pending {
		t.Fatal("Grok OAuth 429 did not preserve follow-up state")
	}
	if !svc.ShouldStopOpenAIOAuth429FailoverRuntime(account, http.StatusBadGateway, 3, &pending) {
		t.Fatal("follow-up failure should consume the preserved stop state")
	}
}
