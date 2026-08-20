package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type adminPaymentSettingRepoStub struct {
	values map[string]string
}

func (s *adminPaymentSettingRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	value, err := s.GetValue(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (s *adminPaymentSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (s *adminPaymentSettingRepoStub) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *adminPaymentSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = s.values[key]
	}
	return values, nil
}

func (s *adminPaymentSettingRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := s.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *adminPaymentSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}

func (*adminPaymentSettingRepoStub) Delete(context.Context, string) error { return nil }

func TestSanitizeAdminPaymentOrderForResponseAddsCurrency(t *testing.T) {
	now := time.Now()
	order := &dbent.PaymentOrder{
		ID:          1,
		UserID:      2,
		Amount:      100,
		PayAmount:   108,
		FeeRate:     8,
		OutTradeNo:  "sub2_202606250001",
		PaymentType: "stripe",
		OrderType:   "subscription",
		Status:      "COMPLETED",
		ExpiresAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"currency":       "USD",
		},
	}

	got := sanitizeAdminPaymentOrderForResponse(order)
	if got == nil {
		t.Fatal("expected sanitized order")
	}
	if got.Currency != "USD" {
		t.Fatalf("expected currency USD, got %q", got.Currency)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal sanitized order: %v", err)
	}
	if strings.Contains(string(body), "provider_snapshot") {
		t.Fatalf("expected provider_snapshot to be omitted, got %s", string(body))
	}
}

func TestAdminSubscriptionPlansForResponseKeepsPlanBillingFields(t *testing.T) {
	planWeekly := 88.0
	now := time.Now()
	plans := []*dbent.SubscriptionPlan{
		{
			ID:             11,
			Name:           "All models",
			Description:    "Composite access",
			Price:          19.99,
			Currency:       "CNY",
			ValidityDays:   30,
			ValidityUnit:   "days",
			Features:       "OpenAI\nClaude\nGemini\nGrok",
			ProductName:    "Sub2API",
			ForSale:        true,
			SortOrder:      1,
			RateMultiplier: 0.75,
			WeeklyLimitUsd: &planWeekly,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	got := adminSubscriptionPlansForResponse(plans)

	if len(got) != 1 {
		t.Fatalf("expected one plan, got %d", len(got))
	}
	if got[0].RateMultiplier != 0.75 {
		t.Fatalf("expected plan rate multiplier to be included, got %v", got[0].RateMultiplier)
	}
	if got[0].WeeklyLimitUSD == nil || *got[0].WeeklyLimitUSD != planWeekly {
		t.Fatalf("expected weekly limit to be included, got %#v", got[0].WeeklyLimitUSD)
	}
	// 投影必须保留 ent 原始响应的全部套餐字段：currency 丢失曾导致编辑保存时
	// 静默清空套餐货币（PlanEditDialog 回传空串 → SetCurrency("")）。
	if got[0].Currency != "CNY" {
		t.Fatalf("expected currency to be preserved, got %q", got[0].Currency)
	}
	if !got[0].CreatedAt.Equal(now) || !got[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected created_at/updated_at to be preserved, got %v / %v", got[0].CreatedAt, got[0].UpdatedAt)
	}
	body, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal plan response: %v", err)
	}
	if strings.Contains(string(body), "group_platform") || strings.Contains(string(body), "supported_model_scopes") {
		t.Fatalf("expected no routing metadata in plan response, got %s", body)
	}
}

func TestAdminSubscriptionPlanResponsePreservesZeroRateMultiplier(t *testing.T) {
	result := AdminSubscriptionPlanResult{
		ID:             1,
		Name:           "Free trial",
		Description:    "zero multiplier is valid",
		Price:          0.01,
		ValidityDays:   1,
		ValidityUnit:   "days",
		RateMultiplier: 0,
	}

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal subscription plan result: %v", err)
	}
	if !strings.Contains(string(body), `"rate_multiplier":0`) {
		t.Fatalf("expected zero rate multiplier to be serialized, got %s", body)
	}
}

func TestPaymentHandlerUpdatesGlobalBalanceRateMultiplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminPaymentSettingRepoStub{}
	handler := NewPaymentHandler(nil, service.NewPaymentConfigService(nil, repo, nil))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/payment/balance-rate-multiplier", strings.NewReader(`{"rate_multiplier":0}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateGlobalBalanceRateMultiplier(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if repo.values[service.SettingKeyGlobalBalanceRateMultiplier] != "0" {
		t.Fatalf("stored global balance multiplier = %q, want 0", repo.values[service.SettingKeyGlobalBalanceRateMultiplier])
	}
	if !strings.Contains(recorder.Body.String(), `"rate_multiplier":0`) {
		t.Fatalf("expected multiplier response, got %s", recorder.Body.String())
	}
}

func TestPaymentHandlerRejectsNegativeGlobalBalanceRateMultiplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewPaymentHandler(nil, service.NewPaymentConfigService(nil, &adminPaymentSettingRepoStub{}, nil))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/payment/balance-rate-multiplier", strings.NewReader(`{"rate_multiplier":-0.1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateGlobalBalanceRateMultiplier(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("negative multiplier status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
