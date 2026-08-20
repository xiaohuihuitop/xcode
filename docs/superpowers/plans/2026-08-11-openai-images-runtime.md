# OpenAI Images Runtime 纯化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 OpenAI Images 的生产 runtime 从 Gin 兼容桥迁移到纯 `gatewayruntime.HTTPExchange`，保持现有业务行为。

**Architecture:** 复用现有 `OpenAIGatewayService` 的调度、凭据和响应解析逻辑，新增 exchange 版本的请求构造、同步响应、SSE 响应及错误写回函数。`ForwardImagesRuntime` 直接进入纯实现；旧 Gin Forward 仅作为未迁移旧入口的兼容代码保留。

**Tech Stack:** Go 1.26、`net/http`、现有 `gatewayruntime.HTTPExchange`、现有 OpenAI Images 解析器和 `testing`/`testify`。

---

### Task 1: 建立 runtime 成功路径基线

**Files:**
- Modify: `backend/internal/service/gateway_runtime_exchange_test.go`
- Modify: `backend/internal/service/gateway_runtime_exchange.go`

- [x] **Step 1: 写 API Key JSON exchange 测试**

使用现有 `runtimeExchangeTestDouble` 和 `httpUpstreamRecorder`，构造 `gpt-image-1` generations 请求，断言 upstream 路径为 `/v1/images/generations`，exchange 状态为 200，响应包含图片数据，结果包含图片数量和模型。

- [x] **Step 2: 运行测试确认当前实现仍经 Gin 桥**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run TestForwardImagesRuntimeUsesPureHTTPExchange -count=1
```

预期：测试先能复现旧路径；随后增加静态断言或 runtime seam，确保实现替换后不会回到 `withRuntimeGinContext`。

- [x] **Step 3: 保持测试中的 exchange 请求头和响应状态可观测**

复用 `HTTPExchange.Request()`、`Header()`、`WriteHeader()`、`Write()`、`Flush()`、`Written()` 和 `Size()`，不扩展公共 contract。

### Task 2: 抽取 OpenAI Images 请求构造

**Files:**
- Modify: `backend/internal/service/openai_images.go`

- [x] **Step 1: 增加 headers 版本构造器**

新增：

```go
func (s *OpenAIGatewayService) buildOpenAIImagesRequestFromHeaders(
    ctx context.Context,
    headers http.Header,
    account *Account,
    body []byte,
    contentType string,
    token string,
    endpoint string,
) (*http.Request, error)
```

将认证头、允许透传头、UA、Content-Type 和账号覆盖逻辑全部迁移到该函数；旧 `buildOpenAIImagesRequest` 只保留一层 Gin 头部适配并调用新函数。

- [x] **Step 2: 为 JSON 和 multipart 请求补充头部/模型重写断言**

断言上游收到正确的模型、Content-Type、认证头和允许透传头；不允许透传的客户端头不得进入 upstream。

- [x] **Step 3: 运行 OpenAI Images 请求构造测试**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'OpenAIImages.*(Request|Multipart|Model)' -count=1
```

### Task 3: 抽取非流式 exchange 响应与错误写回

**Files:**
- Create: `backend/internal/service/openai_images_runtime.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_images_responses.go`

- [x] **Step 1: 写纯 exchange 非流式响应测试**

覆盖普通 generations JSON 与 OAuth Responses JSON 转换，断言响应头过滤、状态码、响应体、usage、图片数量和输出尺寸。

- [x] **Step 2: 实现 exchange 非流式响应函数**

新增 `handleOpenAIImagesNonStreamingResponseExchange` 和 OAuth 对应函数。读取上游响应时使用现有受限读取逻辑的 context/limit 版本；通过 exchange 写入状态、过滤头和 body，不能访问 Gin writer。

- [x] **Step 3: 实现 exchange 错误响应函数**

把 `OpenAIImagesUpstreamError` 转换为 JSON 错误体写入 exchange；保留 passthrough 规则、账号冷却和 `UpstreamFailoverError` 判定。写入前检查 `exchange.Written()`，避免重复响应。

- [x] **Step 4: 运行非流式和错误测试**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'OpenAIImages|ForwardImagesRuntime' -count=1
```

### Task 4: 抽取 SSE exchange 响应

**Files:**
- Modify: `backend/internal/service/openai_images_runtime.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_images_responses.go`

- [x] **Step 1: 写 API Key 与 OAuth SSE 测试**

使用包含 lifecycle、partial image、completed、usage 的 SSE body，断言 exchange 收到事件、Flush 被调用、首字节延迟存在、图片数量/尺寸和 usage 正确；再覆盖客户端写入失败后仍继续读取上游的计费语义。

- [x] **Step 2: 实现 exchange SSE writer**

把现有 SSE 状态机的输出目标换成 exchange：事件写入用 `exchange.Write`，刷新用 `exchange.Flush`，keepalive 用 `exchange.Write([]byte(":\\n\\n"))`。保留上游读取超时、错误事件、重复图片去重和完成事件逻辑。

- [x] **Step 3: 运行 SSE 定向测试**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'OpenAIImages.*(Stream|SSE|Keepalive)' -count=1
```

### Task 5: 接入 runtime 并锁定无 Gin 回流

**Files:**
- Modify: `backend/internal/service/gateway_runtime_exchange.go`
- Modify: `backend/internal/service/gateway_runtime_exchange_test.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [x] **Step 1: 将 `ForwardImagesRuntime` 改为直接调用 exchange 实现**

runtime wrapper 只校验 exchange/request，然后调用新纯方法；不得调用 `withRuntimeGinContext` 或 `ForwardImages`。

- [x] **Step 2: 增加静态 guard**

将 OpenAI Images runtime wrapper 加入 adapter purity guard，扫描生产代码中 `ForwardImagesRuntime` 所在路径不得出现 Gin carrier 调用。

- [x] **Step 3: 运行 handler/service/architecture 回归**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/handler ./internal/architecture -run 'OpenAIImages|GeminiMediaExecutor|RuntimeBoundary|Sub2APIAdapter' -count=1 -timeout=25m
```

### Task 6: 格式化、记忆更新和交付前检查

**Files:**
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/superpowers/plans/2026-08-11-sub2api-adapter-purification.md`

- [x] **Step 1: 更新状态**

记录 OpenAI Images 已纯化、验证命令和仍待迁移的 OpenAI Chat/Responses/Messages/Live，以及暂缓的其他渠道。

- [x] **Step 2: 格式化与 diff 检查**

```powershell
Get-ChildItem backend/internal/service -Filter '*openai_images*runtime*.go' | ForEach-Object { & 'C:\Program Files\Go\bin\gofmt.exe' -w $_.FullName }
git diff --check
```

- [x] **Step 3: 输出真实验证结果**

不得将未运行的完整仓库测试、GitHub workflow、Docker 构建或服务器验证写成通过；本阶段不执行 commit、tag、push 或部署。

## 执行结果

- Task 1-5 已完成：OpenAI Images 的 API Key/OAuth、JSON/multipart、同步/SSE、错误透传与 failover 已进入纯 `HTTPExchange` runtime。
- Task 6 的代码格式、diff 检查和 service/handler/architecture unit 回归已完成；本阶段没有执行 Git 发布或服务器部署。
