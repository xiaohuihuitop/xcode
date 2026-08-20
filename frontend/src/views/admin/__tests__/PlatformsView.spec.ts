import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PlatformsView from '../PlatformsView.vue'
import { adminAPI } from '@/api/admin'

const { showError, showSuccess } = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.platforms.errors.PLATFORM_IN_USE') return 'localized platform in use'
        return params ? `${key}:${JSON.stringify(params)}` : key
      },
    }),
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    platforms: {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      previewDelete: vi.fn(),
      remove: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

const DataTableStub = defineComponent({
  props: {
    columns: { type: Array, required: true },
    data: { type: Array, required: true },
  },
  template: `
    <div data-test="platform-table">
      <div v-for="row in data" :key="row.id" data-test="platform-row">
        <span v-for="column in columns" :key="column.key">
          <slot :name="\`cell-\${column.key}\`" :row="row" :value="row[column.key]" />
        </span>
      </div>
    </div>
  `,
})

const mountView = () => mount(PlatformsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      DataTable: DataTableStub,
      Icon: true,
      PlatformIcon: true,
      PlatformPoolDialog: { template: '<div />' },
      ConfirmDialog: defineComponent({
        props: ['show', 'title', 'message', 'confirmDisabled'],
        emits: ['confirm', 'cancel'],
        template: `
          <div v-if="show" data-test="platform-delete-dialog">
            <p data-test="platform-delete-message">{{ message }}</p>
            <button data-test="confirm-platform-delete" :disabled="confirmDisabled" @click="$emit('confirm')">confirm</button>
          </div>
        `,
      }),
    },
  },
})

describe('PlatformsView', () => {
  beforeEach(() => {
    vi.mocked(adminAPI.platforms.list).mockReset()
    vi.mocked(adminAPI.platforms.previewDelete).mockReset()
    vi.mocked(adminAPI.platforms.remove).mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('renders platforms from the platform-level endpoint contract', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([
      {
        id: 1,
        code: 'codex',
        name: 'Codex',
        account_platform: 'openai',
        status: 'active',
        endpoint_capabilities: ['chat_completions', 'responses'],
        model_rules: [{ id: 1, model_pattern: 'gpt-*', upstream_model: 'gpt-5.6', enabled: true }],
      },
      {
        id: 2,
        code: 'glm',
        name: 'GLM',
        account_platform: 'openai',
        status: 'active',
        endpoint_capabilities: ['chat_completions'],
        model_rules: [{ id: 2, model_pattern: 'glm*', upstream_model: '', enabled: true }],
      },
    ])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('[data-test="platform-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('codex')
    expect(wrapper.text()).toContain('gpt-*')
    expect(wrapper.text()).toContain('gpt-5.6')
    expect(wrapper.text()).toContain('Chat Completions')
    expect(wrapper.text()).toContain('Responses')
  })

  it('shows a retryable error state when the platform list fails', async () => {
    vi.mocked(adminAPI.platforms.list).mockRejectedValue(new Error('offline'))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="platform-load-error"]').exists()).toBe(true)
    expect(showError).toHaveBeenCalled()
  })

  it('deletes a platform only after explicit confirmation', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([{
      id: 7, code: 'unused', name: 'Unused', account_platform: 'openai', status: 'disabled',
      endpoint_capabilities: [], model_rules: [],
    }])
    vi.mocked(adminAPI.platforms.remove).mockResolvedValue({
      platform_id: 7,
      cleaned: { accounts: 0, api_keys: 0, usage_logs: 3, audits: 0, ops: 2, configs: 0, can_delete: true },
    })
    vi.mocked(adminAPI.platforms.previewDelete).mockResolvedValue({
      accounts: 0, api_keys: 0, usage_logs: 3, audits: 0, ops: 2, configs: 0, can_delete: true,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')
    await flushPromises()
    expect(adminAPI.platforms.previewDelete).toHaveBeenCalledWith(7)
    expect(adminAPI.platforms.remove).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="platform-delete-message"]').text()).toContain('"usage_logs":3')
    expect(wrapper.get('[data-test="platform-delete-message"]').text()).toContain('"ops":2')
    expect(wrapper.get<HTMLButtonElement>('[data-test="confirm-platform-delete"]').element.disabled).toBe(false)
    await wrapper.get('[data-test="confirm-platform-delete"]').trigger('click')
    await flushPromises()

    expect(adminAPI.platforms.remove).toHaveBeenCalledWith(7)
    expect(showSuccess).toHaveBeenCalledWith('admin.platforms.deleted')
  })

  it('blocks deletion when accounts or API keys are still attached', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([{
      id: 7, code: 'used', name: 'Used', account_platform: 'openai', status: 'disabled',
      endpoint_capabilities: [], model_rules: [],
    }])
    vi.mocked(adminAPI.platforms.previewDelete).mockResolvedValue({
      accounts: 1, api_keys: 2, usage_logs: 30, audits: 4, ops: 5, configs: 6, can_delete: false,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="platform-delete-message"]').text()).toContain('"accounts":1')
    expect(wrapper.get('[data-test="platform-delete-message"]').text()).toContain('"api_keys":2')
    expect(wrapper.get<HTMLButtonElement>('[data-test="confirm-platform-delete"]').element.disabled).toBe(true)
    await wrapper.get('[data-test="confirm-platform-delete"]').trigger('click')
    expect(adminAPI.platforms.remove).not.toHaveBeenCalled()
  })

  it('does not open the confirmation dialog when preview fails', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([{
      id: 7, code: 'unused', name: 'Unused', account_platform: 'openai', status: 'disabled',
      endpoint_capabilities: [], model_rules: [],
    }])
    vi.mocked(adminAPI.platforms.previewDelete).mockRejectedValue(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="platform-delete-dialog"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalled()
  })

  it('disables delete commands while the impact preview is loading', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([{
      id: 7, code: 'unused', name: 'Unused', account_platform: 'openai', status: 'disabled',
      endpoint_capabilities: [], model_rules: [],
    }])
    let resolvePreview!: (value: Awaited<ReturnType<typeof adminAPI.platforms.previewDelete>>) => void
    vi.mocked(adminAPI.platforms.previewDelete).mockReturnValue(new Promise((resolve) => {
      resolvePreview = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')

    expect(wrapper.get<HTMLButtonElement>('[data-test="delete-platform-7"]').element.disabled).toBe(true)
    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')
    expect(adminAPI.platforms.previewDelete).toHaveBeenCalledTimes(1)

    resolvePreview({
      accounts: 0, api_keys: 0, usage_logs: 0, audits: 0, ops: 0, configs: 0, can_delete: true,
    })
    await flushPromises()

    expect(wrapper.get<HTMLButtonElement>('[data-test="delete-platform-7"]').element.disabled).toBe(false)
    expect(wrapper.find('[data-test="platform-delete-dialog"]').exists()).toBe(true)
  })

  it('shows the localized safe-delete conflict instead of removing references', async () => {
    vi.mocked(adminAPI.platforms.list).mockResolvedValue([{
      id: 7, code: 'used', name: 'Used', account_platform: 'openai', status: 'active',
      endpoint_capabilities: ['responses'], model_rules: [],
    }])
    vi.mocked(adminAPI.platforms.remove).mockRejectedValue({
      reason: 'PLATFORM_IN_USE', metadata: { accounts: '1', api_keys: '2', usage_logs: '3', audits: '4', ops: '5', configs: '6' },
    })
    vi.mocked(adminAPI.platforms.previewDelete).mockResolvedValue({
      accounts: 0, api_keys: 0, usage_logs: 3, audits: 4, ops: 5, configs: 6, can_delete: true,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-platform-7"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="confirm-platform-delete"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('localized platform in use')
  })
})
