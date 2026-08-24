import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ModelPricingView from '../ModelPricingView.vue'

const { modelPricing, showError, showSuccess } = vi.hoisted(() => ({
  modelPricing: {
    catalog: vi.fn(),
    upsertPlatformSale: vi.fn(),
    list: vi.fn(),
    getById: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
  },
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({ adminAPI: { modelPricing } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.modelPricing.officialPrice': '官方公开价',
    'admin.modelPricing.salePrice': '平台售价',
    'admin.modelPricing.official': '继承',
    'admin.modelPricing.custom': '自定义',
    'admin.modelPricing.unavailable': '不可用',
    'admin.modelPricing.editSale': '编辑平台售价',
    'admin.modelPricing.advancedRules': '高级规则',
    'admin.modelPricing.createRule': '添加高级规则',
    'admin.modelPricing.sourceTypeBundledCatalog': '内置公开目录',
    'admin.modelPricing.saved': '模型价格已保存',
    'common.save': '保存',
  }
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => messages[key] ?? key }),
  }
})

const inheritedRow = {
  platform_id: 7,
  platform_code: 'codex',
  platform_name: 'Codex Platform',
  account_platform: 'openai',
  model_pattern: 'gpt-5.6-sol',
  upstream_model: 'gpt-5.6-sol-upstream',
  billing_mode: 'token',
  official_pricing: {
    input_price: 0.000005,
    output_price: 0.000015,
    cache_write_price: null,
    cache_read_price: 0,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
  },
  official_source: {
    source_type: 'bundled_catalog',
    source_name: 'OpenAI public pricing',
    source_url: 'https://example.com/pricing',
    matched_model: 'gpt-5.6-sol',
    updated_at: '2026-08-24T12:00:00Z',
  },
  sale_pricing: {
    input_price: 0.000005,
    output_price: 0.000015,
    cache_write_price: null,
    cache_read_price: 0,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
  },
  sale_source: 'official',
  override: null,
  intervals: [],
}

const customRow = {
  ...inheritedRow,
  model_pattern: 'gpt-custom',
  upstream_model: 'gpt-custom-upstream',
  sale_pricing: {
    ...inheritedRow.sale_pricing,
    input_price: 0.000007,
    intervals: [
      { min_tokens: 0, max_tokens: 100, input_price: 0.000007, sort_order: 0 },
      { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
    ],
  },
  sale_source: 'custom',
  override: {
    id: 12,
    adapter: 'codex',
    model_pattern: 'gpt-custom',
    billing_mode: 'token',
    input_price: 0.000007,
    output_price: null,
    cache_write_price: null,
    cache_read_price: 0,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [
      { min_tokens: 0, max_tokens: 100, input_price: 0.000007, sort_order: 0 },
      { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
    ],
    status: 'active',
  },
  intervals: [
    { min_tokens: 0, max_tokens: 100, input_price: 0.000007, sort_order: 0 },
    { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
  ],
}

const AppLayoutStub = defineComponent({ template: '<div><slot /></div>' })

function mountView() {
  return mount(ModelPricingView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: true,
      },
    },
  })
}

describe('ModelPricingView', () => {
  beforeEach(() => {
    for (const mock of Object.values(modelPricing)) mock.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    modelPricing.catalog.mockResolvedValue([inheritedRow, customRow])
    modelPricing.list.mockResolvedValue([customRow.override])
    modelPricing.upsertPlatformSale.mockResolvedValue(customRow.override)
  })

  it('renders platform catalog pricing, source, and sale states', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(modelPricing.catalog).toHaveBeenCalledWith({})
    expect(wrapper.findAll('[data-testid="catalog-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('Codex Platform')
    expect(wrapper.text()).toContain('gpt-5.6-sol-upstream')
    expect(wrapper.text()).toContain('官方公开价')
    expect(wrapper.text()).toContain('平台售价')
    expect(wrapper.text()).toContain('$5')
    expect(wrapper.text()).toContain('$7')
    expect(wrapper.text()).toContain('OpenAI public pricing')
    expect(wrapper.text()).toContain('继承')
    expect(wrapper.text()).toContain('自定义')
  })

  it('opens the platform sale editor and saves token values in per-token units', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    expect(editor.text()).toContain('Codex Platform')
    expect(editor.text()).toContain('gpt-custom')

    await editor.get('[data-price-field="input_price"] [data-testid="custom-price-input"]').setValue('6')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.upsertPlatformSale).toHaveBeenCalledWith(expect.objectContaining({
      platform_id: 7,
      model_pattern: 'gpt-custom',
      input_price: 0.000006,
    }))
    expect(modelPricing.catalog).toHaveBeenCalledTimes(2)
  })

  it('preserves an explicit zero instead of treating it as inherited', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-5.6-sol"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    await editor.get('[data-price-field="input_price"] [data-mode="zero"]').trigger('click')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.upsertPlatformSale).toHaveBeenCalledWith(expect.objectContaining({
      input_price: 0,
    }))
  })

  it('blocks platform sale submission while intervals are invalid', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    await editor.get('[data-testid="min-tokens-1"]').setValue('50')

    expect(editor.get<HTMLButtonElement>('[data-testid="sale-save"]').element.disabled).toBe(true)
    await editor.get('[data-testid="sale-save"]').trigger('click')
    expect(modelPricing.upsertPlatformSale).not.toHaveBeenCalled()
  })

  it('keeps the advanced wildcard and adapter rule workflow available', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="advanced-toggle"]').trigger('click')

    expect(modelPricing.list).toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="advanced-rule-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('gpt-custom')
    await wrapper.get('[data-testid="create-advanced-rule"]').trigger('click')
    expect(wrapper.find('[data-testid="advanced-editor"]').exists()).toBe(true)
  })
})
