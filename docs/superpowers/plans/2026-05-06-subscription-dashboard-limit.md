# Subscription Dashboard Limit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将用户端订阅展示统一到仪表盘，并限制用户最多拥有 4 个有效订阅。

**Architecture:** 前端通过复用现有订阅页的展示片段，把订阅进度条下沉到仪表盘卡片；路由层移除独立订阅页入口并将旧地址重定向到仪表盘。后端在订阅服务统一增加有效订阅数量上限校验，确保兑换、购买和后台分配都遵守同一规则。

**Tech Stack:** Vue 3, Pinia, Vue Router, TypeScript, Go service layer, Vitest, Go test

---

### Task 1: 补充失败测试

**Files:**
- Modify: `backend/internal/service/subscription_assign_idempotency_test.go`
- Modify: `frontend/src/router/__tests__/guards.spec.ts`
- Modify: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Modify: `frontend/src/views/auth/__tests__/WechatCallbackView.spec.ts`

- [ ] 为订阅服务补充“4 个有效订阅上限”和“续期不受上限影响”的失败测试
- [ ] 为前端补充“/subscriptions 不再作为独立入口”的测试
- [ ] 运行对应测试，确认先失败

### Task 2: 实现后端有效订阅上限

**Files:**
- Modify: `backend/internal/service/subscription_service.go`

- [ ] 增加统一的有效订阅计数常量、错误和辅助方法
- [ ] 在 `AssignSubscription`、`AssignOrExtendSubscription`、`BulkAssignSubscription` 的新建分支接入上限校验
- [ ] 保证已有同组订阅续期不受上限影响
- [ ] 运行后端定向测试，确认通过

### Task 3: 实现用户端展示收敛

**Files:**
- Modify: `frontend/src/components/user/dashboard/UserDashboardStats.vue`
- Modify: `frontend/src/views/user/DashboardView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/components/common/SubscriptionProgressMini.vue`
- Delete or leave unused: `frontend/src/views/user/SubscriptionsView.vue`

- [ ] 仪表盘订阅卡片补进度条和重置时间展示
- [ ] `/subscriptions` 改为重定向到 `/dashboard`
- [ ] 去掉侧边栏和迷你组件中的“我的订阅”入口
- [ ] 调整历史跳转目标到 `/dashboard`

### Task 4: 验证与收尾

**Files:**
- No code changes expected unless修复验证发现的问题

- [ ] 运行前端 Vitest 定向测试
- [ ] 运行后端 Go 定向测试
- [ ] 运行前端构建或类型检查
- [ ] 检查 git diff，确认没有无关改动
