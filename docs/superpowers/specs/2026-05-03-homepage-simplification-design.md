# Homepage Simplification Design

**Date:** 2026-05-03

## Goal

将 `/home` 从当前偏宣传型、信息较多的首页，收敛为一个简约的双卡片入口页。新首页只保留站点识别、一句简介，以及两个核心入口：

- 套餐
- 控制台

## Scope

本次只修改前端首页呈现与首页入口行为，不改后端接口，不改支付业务流程，不改兑换页本身，不改登录表单本身，不改控制台页面结构。

### In Scope

- 重写 `/home` 默认页面的布局和视觉结构
- 保留动态站点信息读取：`siteName`、`siteLogo`、`siteSubtitle`
- 提供“购买套餐”外链按钮
- 提供“兑换套餐”站内跳转按钮
- 提供“控制台”入口按钮，并根据登录状态跳到正确页面
- 保留当前首页对 `home_content` 的兼容逻辑

### Out of Scope

- `/redeem` 页面内容和交互
- `/login` 页面视觉重构
- `/dashboard`、`/admin/dashboard` 页面行为修改
- 套餐详情、价格表、FAQ、公告、文档区等营销内容
- 后端路由或鉴权逻辑改写

## Current State

当前 [HomeView.vue](/C:/Users/xiaohuihui/Desktop/sub的页面/frontend/src/views/HomeView.vue) 默认首页包含：

- 大面积背景装饰
- Hero 区
- 终端动画
- 功能标签
- 功能卡片
- 支持平台展示
- 页脚文档/GitHub 链接

这类结构更像产品宣传首页，但不符合本次“简约、以动作入口为主”的目标。

## Desired UX

首页应在首屏直接回答三个问题：

1. 这是什么站点
2. 我怎么买套餐或兑换套餐
3. 我怎么进入控制台

用户不需要滚动，不需要阅读多段介绍，不需要先理解产品特性再决定下一步。

## Information Architecture

新首页默认状态保留四个区域：

1. 顶部轻量导航
2. 中部站点标题与一句简介
3. 双卡片核心入口区
4. 极简页脚

### Top Bar

- 左侧显示站点 logo 与站点名
- 右侧仅保留：
  - 语言切换
  - 明暗主题切换

以下内容移除：

- 文档入口
- GitHub 强入口
- 当前右上角单独的登录/仪表盘胶囊按钮

### Hero Copy

- 主标题：`siteName`
- 副标题：`siteSubtitle`

副标题继续复用现有公开设置；若缺失，则沿用当前默认文案逻辑。

### Core Cards

首屏中部放置两张对称卡片，桌面端左右并排，移动端上下排列。

#### Card 1: 套餐

内容：

- 标题：`套餐`
- 一句简短说明：引导购买或兑换
- 按钮 A：`购买套餐`
- 按钮 B：`兑换套餐`

行为：

- `购买套餐` 使用外链打开 `https://pay.ldxp.cn/shop/FED14QEA`
- `兑换套餐` 跳转 `/redeem`

不在首页展示任何价格、规格或套餐明细。

#### Card 2: 控制台

内容：

- 标题：`控制台`
- 一句简短说明：进入账号后台进行管理
- 主按钮：根据登录状态动态变化

行为：

- 未登录：
  - 按钮文案为 `登录控制台`
  - 跳转 `/login`
- 已登录普通用户：
  - 按钮文案为 `进入控制台`
  - 跳转 `/dashboard`
- 已登录管理员：
  - 按钮文案为 `进入控制台`
  - 跳转 `/admin/dashboard`

## Authentication Behavior

本次不新增新的鉴权分支，直接复用现有逻辑：

- [router/index.ts](/C:/Users/xiaohuihui/Desktop/sub的页面/frontend/src/router/index.ts) 已定义 `/redeem` 为受保护页面
- 未登录访问 `/redeem` 会自动跳转 `/login?redirect=/redeem`
- [LoginView.vue](/C:/Users/xiaohuihui/Desktop/sub的页面/frontend/src/views/auth/LoginView.vue) 已支持登录后按 `redirect` 回跳
- 已登录访问 `/login` 时，路由守卫会自动跳转到用户或管理员控制台

因此“如果已经登录了就直接跳控制页面”的需求已经被现有路由守卫覆盖，不需要额外在首页之外再实现一套逻辑。

## Visual Direction

视觉方向为“干净、轻量、居中、两步可达”。

### Keep

- 站点 logo
- 站点名
- 一句简介
- 明暗主题支持
- 语言切换支持

### Remove

- 终端动画
- 功能卖点区
- 平台支持区
- 复杂背景光斑
- 过重的营销型视觉装饰

### Layout Principles

- 内容宽度收紧，避免首页过于空旷
- 双卡片在视觉上权重相等
- 每张卡片仅保留一层标题、一层说明、一组按钮
- 移动端优先保证按钮易点按、卡片堆叠清晰

## Technical Design

优先采用最小改动策略。

### Primary File

- 修改 [HomeView.vue](/C:/Users/xiaohuihui/Desktop/sub的页面/frontend/src/views/HomeView.vue)

### Reuse Existing State

继续复用：

- `authStore.isAuthenticated`
- `authStore.isAdmin`
- `appStore.cachedPublicSettings`
- `siteName`
- `siteLogo`
- `siteSubtitle`

### Route and Link Handling

- 控制台入口继续使用站内路由
- 兑换入口继续使用站内路由
- 购买入口使用外链 `<a>`，带 `target="_blank"` 和 `rel="noopener noreferrer"`

### Preserve Home Content Override

首页当前支持 `home_content` 两种覆盖模式：

- URL iframe 模式
- HTML 注入模式

这部分必须保留。只有在 `home_content` 为空时，才渲染新的简约首页。

## Error Handling

本次首页为静态入口页，无新增接口请求。

已有的 `authStore.checkAuth()` 和 `appStore.fetchPublicSettings()` 保持不变。若公开设置加载较慢，首页继续依赖现有默认值回退，不增加新的 loading 态。

## Testing Strategy

采用 Level 0 到 Level 1 的定向验证。

### Functional Checks

- `/home` 渲染新双卡片首页
- 未登录点击 `登录控制台` 跳转 `/login`
- 已登录状态显示 `进入控制台`
- 管理员状态下控制台按钮目标为 `/admin/dashboard`
- `购买套餐` 指向指定外链
- `兑换套餐` 指向 `/redeem`
- `home_content` 非空时仍优先覆盖默认首页

### Technical Checks

- 运行前端类型检查或构建验证
- 如已有合适的首页/导航测试，可补一个定向测试

## Acceptance Criteria

满足以下条件即可视为本次设计完成：

1. `/home` 默认界面改为简约双卡片结构
2. 页面保留站点名和一句简介
3. 页面包含套餐入口和控制台入口
4. 套餐入口包含购买外链与兑换入口
5. 控制台入口根据登录状态跳转正确页面
6. 已登录访问 `/login` 仍按现有逻辑直接跳控制台
7. `home_content` 覆盖逻辑不被破坏
