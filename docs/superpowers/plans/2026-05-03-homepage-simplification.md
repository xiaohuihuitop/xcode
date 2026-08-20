# Homepage Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `/home` 默认页面改为简约双卡片首页，保留站点名和一句简介，并提供套餐入口与控制台入口。

**Architecture:** 保留 `home_content` 覆盖逻辑，只收敛默认首页分支。通过 `HomeView.vue` 复用现有 `authStore` 和 `appStore` 状态，使用已有路由守卫承接 `/login`、`/redeem`、`/dashboard`、`/admin/dashboard` 跳转，不新增鉴权分支。

**Tech Stack:** Vue 3、TypeScript、Pinia、Vue Router、Vitest、Vue Test Utils

---

### Task 1: 为首页入口逻辑补定向测试

**Files:**
- Create: `frontend/src/views/__tests__/HomeView.spec.ts`
- Modify: `frontend/package.json`
- Test: `frontend/src/views/__tests__/HomeView.spec.ts`

- [ ] **Step 1: 写失败测试，覆盖默认首页关键入口行为**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'

const checkAuthMock = vi.fn()
const fetchPublicSettingsMock = vi.fn()

const authState = {
  isAuthenticated: false,
  isAdmin: false,
  user: { email: 'user@example.com' }
}

const appState = {
  publicSettingsLoaded: true,
  siteName: 'Sub2API',
  siteLogo: '',
  docUrl: '',
  cachedPublicSettings: {
    site_name: 'Sub2API',
    site_logo: '',
    site_subtitle: '简单、直接、可用',
    home_content: ''
  }
}

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    ...authState,
    checkAuth: checkAuthMock
  }),
  useAppStore: () => ({
    ...appState,
    fetchPublicSettings: fetchPublicSettingsMock
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/components/common/LocaleSwitcher.vue', () => ({
  default: { template: '<div class="locale-switcher-stub" />' }
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: {
    props: ['name'],
    template: '<span class="icon-stub">{{ name }}</span>'
  }
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<div>login</div>' } },
      { path: '/dashboard', component: { template: '<div>dashboard</div>' } },
      { path: '/admin/dashboard', component: { template: '<div>admin dashboard</div>' } },
      { path: '/redeem', component: { template: '<div>redeem</div>' } }
    ]
  })
}

describe('HomeView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    authState.isAuthenticated = false
    authState.isAdmin = false
    authState.user = { email: 'user@example.com' }
    appState.publicSettingsLoaded = true
    appState.cachedPublicSettings.home_content = ''
    appState.cachedPublicSettings.site_name = 'Sub2API'
    appState.cachedPublicSettings.site_subtitle = '简单、直接、可用'
  })

  it('未登录时展示站点名、简介、套餐和登录控制台入口', async () => {
    const router = createTestRouter()
    const wrapper = mount(HomeView, {
      global: { plugins: [router] }
    })
    await router.isReady()

    expect(wrapper.text()).toContain('Sub2API')
    expect(wrapper.text()).toContain('简单、直接、可用')
    expect(wrapper.text()).toContain('套餐')
    expect(wrapper.text()).toContain('控制台')
    expect(wrapper.text()).toContain('购买套餐')
    expect(wrapper.text()).toContain('兑换套餐')
    expect(wrapper.text()).toContain('登录控制台')
  })

  it('未登录时控制台入口指向 /login，兑换入口指向 /redeem', async () => {
    const router = createTestRouter()
    const wrapper = mount(HomeView, {
      global: { plugins: [router] }
    })
    await router.isReady()

    const links = wrapper.findAll('a')
    const hrefs = links.map((link) => link.attributes('href'))
    expect(hrefs).toContain('/login')
    expect(hrefs).toContain('/redeem')
    expect(hrefs).toContain('https://pay.ldxp.cn/shop/FED14QEA')
  })

  it('普通用户已登录时控制台入口指向 /dashboard', async () => {
    authState.isAuthenticated = true
    const router = createTestRouter()
    const wrapper = mount(HomeView, {
      global: { plugins: [router] }
    })
    await router.isReady()

    expect(wrapper.text()).toContain('进入控制台')
    expect(wrapper.html()).toContain('/dashboard')
  })

  it('管理员已登录时控制台入口指向 /admin/dashboard', async () => {
    authState.isAuthenticated = true
    authState.isAdmin = true
    const router = createTestRouter()
    const wrapper = mount(HomeView, {
      global: { plugins: [router] }
    })
    await router.isReady()

    expect(wrapper.text()).toContain('进入控制台')
    expect(wrapper.html()).toContain('/admin/dashboard')
  })

  it('home_content 非空时仍优先使用覆盖内容', async () => {
    appState.cachedPublicSettings.home_content = '<div class="custom-home">custom</div>'
    const router = createTestRouter()
    const wrapper = mount(HomeView, {
      global: { plugins: [router] }
    })
    await router.isReady()

    expect(wrapper.find('.custom-home').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('购买套餐')
  })
})
```

- [ ] **Step 2: 运行测试并确认当前失败**

Run: `pnpm --dir frontend vitest run src/views/__tests__/HomeView.spec.ts`

Expected: FAIL，报错原因应为当前首页还没有“套餐 / 控制台”双卡片结构或文案不匹配。

- [ ] **Step 3: 如测试目录缺失则创建，不修改 package.json 脚本**

```ts
// 不改 package.json。直接沿用已有:
// "test:run": "vitest run"
```

- [ ] **Step 4: 再次运行单测路径，确保测试文件可被 Vitest 收集**

Run: `pnpm --dir frontend vitest run src/views/__tests__/HomeView.spec.ts --reporter=basic`

Expected: FAIL，并显示 5 个用例被正确收集。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/__tests__/HomeView.spec.ts
git commit -m "test(frontend): 补充首页入口测试"
```

### Task 2: 重写默认首页为简约双卡片结构

**Files:**
- Modify: `frontend/src/views/HomeView.vue`
- Test: `frontend/src/views/__tests__/HomeView.spec.ts`

- [ ] **Step 1: 先保留 home_content 覆盖分支，只替换默认首页模板**

```vue
<template>
  <div v-if="homeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div v-else class="min-h-screen bg-stone-50 text-stone-900">
    <header class="border-b border-stone-200/80 bg-white/80 backdrop-blur">
      <nav class="mx-auto flex max-w-5xl items-center justify-between px-6 py-4">
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center overflow-hidden rounded-xl bg-stone-100">
            <img
              v-if="siteLogo"
              :src="siteLogo"
              alt="Logo"
              class="h-full w-full object-contain"
            />
            <span v-else class="text-sm font-semibold text-stone-500">
              {{ siteName.charAt(0) }}
            </span>
          </div>
          <div class="text-sm font-semibold tracking-tight text-stone-900">
            {{ siteName }}
          </div>
        </div>

        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <button
            @click="toggleTheme"
            class="rounded-xl border border-stone-200 bg-white p-2 text-stone-500 transition hover:text-stone-900"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <main class="px-6 py-16 sm:py-24">
      <div class="mx-auto flex max-w-5xl flex-col gap-12">
        <section class="mx-auto max-w-2xl text-center">
          <p class="text-sm font-medium uppercase tracking-[0.24em] text-stone-500">
            {{ siteName }}
          </p>
          <h1 class="mt-4 text-4xl font-semibold tracking-tight text-stone-950 sm:text-5xl">
            {{ siteName }}
          </h1>
          <p class="mt-4 text-base leading-7 text-stone-600 sm:text-lg">
            {{ siteSubtitle }}
          </p>
        </section>

        <section class="grid gap-6 md:grid-cols-2">
          <article class="rounded-3xl border border-stone-200 bg-white p-8 shadow-sm">
            <div class="mb-6">
              <p class="text-sm font-medium uppercase tracking-[0.2em] text-stone-500">Plan</p>
              <h2 class="mt-3 text-2xl font-semibold text-stone-950">套餐</h2>
              <p class="mt-3 text-sm leading-6 text-stone-600">
                购买新套餐，或使用兑换码激活套餐。
              </p>
            </div>
            <div class="flex flex-col gap-3">
              <a
                href="https://pay.ldxp.cn/shop/FED14QEA"
                target="_blank"
                rel="noopener noreferrer"
                class="inline-flex items-center justify-center rounded-2xl bg-stone-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-stone-800"
              >
                购买套餐
              </a>
              <RouterLink
                to="/redeem"
                class="inline-flex items-center justify-center rounded-2xl border border-stone-200 bg-stone-50 px-5 py-3 text-sm font-medium text-stone-900 transition hover:bg-stone-100"
              >
                兑换套餐
              </RouterLink>
            </div>
          </article>

          <article class="rounded-3xl border border-stone-200 bg-white p-8 shadow-sm">
            <div class="mb-6">
              <p class="text-sm font-medium uppercase tracking-[0.2em] text-stone-500">Console</p>
              <h2 class="mt-3 text-2xl font-semibold text-stone-950">控制台</h2>
              <p class="mt-3 text-sm leading-6 text-stone-600">
                登录后进入后台查看额度、密钥和使用情况。
              </p>
            </div>
            <RouterLink
              :to="dashboardEntryPath"
              class="inline-flex w-full items-center justify-center rounded-2xl bg-emerald-600 px-5 py-3 text-sm font-medium text-white transition hover:bg-emerald-500"
            >
              {{ isAuthenticated ? '进入控制台' : '登录控制台' }}
            </RouterLink>
          </article>
        </section>
      </div>
    </main>

    <footer class="border-t border-stone-200/80 px-6 py-6">
      <div class="mx-auto max-w-5xl text-center text-sm text-stone-500">
        &copy; {{ currentYear }} {{ siteName }}
      </div>
    </footer>
  </div>
</template>
```

- [ ] **Step 2: 精简 script，移除默认首页不再需要的状态和常量**

```ts
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const isDark = ref(document.documentElement.classList.contains('dark'))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardEntryPath = computed(() => {
  if (!isAuthenticated.value) {
    return '/login'
  }
  return isAdmin.value ? '/admin/dashboard' : '/dashboard'
})
const currentYear = computed(() => new Date().getFullYear())

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>
```

- [ ] **Step 3: 删除终端动画和旧首页专属样式，仅保留必要 scoped 样式或全部改用 Tailwind**

```vue
<style scoped>
</style>
```

- [ ] **Step 4: 运行首页单测，确认新结构满足入口行为**

Run: `pnpm --dir frontend vitest run src/views/__tests__/HomeView.spec.ts`

Expected: PASS，5 个用例全部通过。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/HomeView.vue frontend/src/views/__tests__/HomeView.spec.ts
git commit -m "feat(frontend): 精简首页入口布局"
```

### Task 3: 执行定向验证并确认不破坏现有前端质量门禁

**Files:**
- Modify: `frontend/src/views/HomeView.vue`（仅当验证失败需要修复时）
- Test: `frontend/src/views/__tests__/HomeView.spec.ts`

- [ ] **Step 1: 运行前端类型检查**

Run: `pnpm --dir frontend typecheck`

Expected: PASS，无 TypeScript 报错。

- [ ] **Step 2: 运行首页定向单测**

Run: `pnpm --dir frontend vitest run src/views/__tests__/HomeView.spec.ts`

Expected: PASS，首页关键入口逻辑通过。

- [ ] **Step 3: 运行前端构建验证**

Run: `pnpm --dir frontend build`

Expected: PASS，Vite 构建成功。

- [ ] **Step 4: 若任一步失败，只修复与首页改造直接相关的问题后重跑失败项**

```ts
// 允许修复范围：
// - HomeView.vue
// - HomeView.spec.ts
// - 因首页改造引发的直接类型或模板问题
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/HomeView.vue frontend/src/views/__tests__/HomeView.spec.ts
git commit -m "test(frontend): 完成首页改造验证"
```
