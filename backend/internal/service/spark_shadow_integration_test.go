//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSparkShadowIntegration 是 spark-shadow 功能的端到端集成测试。
//
// 覆盖三个核心属性：
//
//  1. 凭据轮换读透（脱钩命门）——母账号 access_token 轮换后，影子通过
//     resolveCredentialAccount / GetAccessToken 立即反映新值，零脱钩。
//
//  2. 路由不变量——影子资格由 quota_dimension 和 Codex Spark 模型别名决定；
//     不再依赖 credentials.model_mapping，普通账号不因旧配置获得特殊资格。
//
//  3. 母账号健康度联动——母不可调度（Status=error 或 Schedulable=false）
//     时，parentHealthyForShadow 对影子返回 false。
//
// 复用的接缝：
//   - newStubCredRepo（credential_shadow_test.go，同包无 tag 始终编译）
//   - resolveCredentialAccount（credential_shadow.go）
//   - OpenAIGatewayService.GetAccessToken（openai_gateway_service.go，openAITokenProvider=nil 降级路径）
//   - 路由资格由 IsModelSupported 决定（spark_routing.go 已移除类型门）
//   - parentHealthyForShadow（spark_routing.go）
func TestSparkShadowIntegration(t *testing.T) {
	ctx := context.Background()
	pid := int64(100)

	// 共享母账号：Credentials 为 map（引用型），可原地轮换而无需重建 stub。
	parent := &Account{
		ID:          100,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token": "T1",
		},
	}
	// 影子账号：不持凭据（与生产语义一致），QuotaDimensionSpark 标记 spark 维度。
	shadow := &Account{
		ID:              200,
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &pid,
		QuotaDimension:  QuotaDimensionSpark,
		Status:          StatusActive,
		Schedulable:     true,
	}

	// repo：stubCredRepo（credential_shadow_test.go）存 *Account 指针，
	// Credentials map 变更直接可见，无需重建 stub。
	repo := newStubCredRepo(parent)

	// ──────────────────────────────────────────────────────────────────────
	// 属性 1：凭据轮换读透（脱钩命门）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("credential_readthrough_initial_T1", func(t *testing.T) {
		// 影子无凭据，resolveCredentialAccount 必须透传到母账号。
		got, err := resolveCredentialAccount(ctx, repo, shadow)
		require.NoError(t, err)
		require.Equal(t, int64(100), got.ID, "解析结果应为母账号")
		require.Equal(t, "T1", got.GetOpenAIAccessToken(),
			"初始应读到 T1")
	})

	t.Run("credential_readthrough_after_rotation_T2", func(t *testing.T) {
		// 模拟 refresh_token 轮换：原地更新母账号凭据。
		// 影子不持凭据、无本地缓存，下次解析必须见到新值。
		parent.Credentials["access_token"] = "T2"

		got, err := resolveCredentialAccount(ctx, repo, shadow)
		require.NoError(t, err)
		require.Equal(t, "T2", got.GetOpenAIAccessToken(),
			"轮换后影子必须立即反映母账号新 token（零脱钩）")
	})

	t.Run("get_access_token_e2e_reads_through_T3", func(t *testing.T) {
		// 端到端：经 OpenAIGatewayService.GetAccessToken 验证全路径读透。
		// openAITokenProvider=nil → 降级到直接读 account.GetOpenAIAccessToken()。
		parent.Credentials["access_token"] = "T3"

		svc := &OpenAIGatewayService{
			accountRepo: repo,
		}
		token, tokenType, err := svc.GetAccessToken(ctx, shadow)
		require.NoError(t, err)
		require.Equal(t, "T3", token,
			"GetAccessToken(影子) 必须返回母账号当前 token")
		require.Equal(t, "oauth", tokenType)
	})

	t.Run("normal_account_returns_its_own_token", func(t *testing.T) {
		// 对照组：普通账号（非影子）直接返回自身凭据，不经 resolveCredentialAccount。
		ordinary := &Account{
			ID:          300,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"access_token": "ordinary-token",
			},
		}
		svc := &OpenAIGatewayService{
			accountRepo: newStubCredRepo(ordinary),
		}
		token, _, err := svc.GetAccessToken(ctx, ordinary)
		require.NoError(t, err)
		require.Equal(t, "ordinary-token", token)
	})

	// ──────────────────────────────────────────────────────────────────────
	// 属性 2：路由不变量（路由资格由 IsModelSupported 决定）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("routing_invariant", func(t *testing.T) {
		// Spark 影子资格来自结构化 quota_dimension，而不是凭证中的旧模型配置。
		sparkModel := "gpt-5.3-codex-spark"
		normalModel := "gpt-5.3-codex"

		pid := int64(1)
		sparkShadow := &Account{ID: 2, ParentAccountID: &pid, QuotaDimension: QuotaDimensionSpark, Platform: PlatformOpenAI}
		require.True(t, sparkShadow.IsModelSupported(sparkModel), "影子配 spark → 接 spark")
		require.False(t, sparkShadow.IsModelSupported(normalModel), "影子（仅 spark mapping）→ 拒非 spark")

		normalAccount := &Account{ID: 3, Platform: PlatformOpenAI}
		require.True(t, normalAccount.IsModelSupported(sparkModel), "普通账号由 Platform 规则决定，不读取旧 mapping")
	})

	// ──────────────────────────────────────────────────────────────────────
	// 属性 3：母账号健康度联动（parentHealthyForShadow）
	// ──────────────────────────────────────────────────────────────────────

	t.Run("parent_health_propagated_to_shadow", func(t *testing.T) {
		// 恢复母账号健康状态（属性 1/2 测试可能改过）
		parent.Status = StatusActive
		parent.Schedulable = true

		lookup := func(id int64) *Account {
			if id == parent.ID {
				return parent
			}
			return nil
		}

		// 母健康 → 影子健康
		require.True(t, parentHealthyForShadow(shadow, lookup),
			"健康母账号时影子应健康")

		// 母 Status=error(凭据不可用)→ 影子不健康
		parent.Status = StatusError
		require.False(t, parentHealthyForShadow(shadow, lookup),
			"Status=error 母账号时影子应不健康")

		// F1 决策 A:母 Schedulable=false (Status=active) 是手动调度暂停,不连坐影子(凭据仍可用)
		parent.Status = StatusActive
		parent.Schedulable = false
		require.True(t, parentHealthyForShadow(shadow, lookup),
			"母账号手动暂停不应连坐影子(凭据仍可用)")

		// F1 核心:母 global 限流(RateLimitResetAt 未来)不连坐 spark 影子
		parent.Schedulable = true
		resetAt := time.Now().Add(1 * time.Hour)
		parent.RateLimitResetAt = &resetAt
		require.True(t, parentHealthyForShadow(shadow, lookup),
			"母账号 global 限流不应连坐 spark 影子")
		parent.RateLimitResetAt = nil

		// 对照组：非影子账号 parentHealthyForShadow 始终 true，不调用 lookup
		parent.Schedulable = true
		lookupNotCalled := func(_ int64) *Account {
			t.Error("非影子账号不应调用 lookup")
			return nil
		}
		require.True(t, parentHealthyForShadow(parent, lookupNotCalled),
			"普通账号应直接返回 true")
	})
}
