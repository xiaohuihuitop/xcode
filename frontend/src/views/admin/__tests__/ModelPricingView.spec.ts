import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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
    'admin.modelPricing.saveFailed': '模型价格保存失败',
    'admin.modelPricing.deleteFailed': '模型价格删除失败',
    'admin.modelPricing.tokenUnit': 'USD / 1M Token',
    'admin.modelPricing.requestUnit': 'USD / 次',
    'admin.modelPricing.imageUnit': 'USD / 图片',
    'admin.modelPricing.perImagePrice': '每张图片价格',
    'admin.modelPricing.effectiveSalePrice': '实际售价',
    'admin.modelPricing.priceDifference': '价差比例',
    'admin.modelPricing.resolvedAfterSave': '保存后重新解析',
    'admin.modelPricing.notAvailable': '暂无',
    'common.save': '保存',
    'common.cancel': '取消',
    'common.saving': '保存中',
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
    input_price: 0.000006,
    intervals: [
      {
        min_tokens: 0,
        max_tokens: 100,
        input_price: 0.000001,
        output_price: 0.000002,
        cache_write_price: 0,
        cache_read_price: null,
        sort_order: 0,
      },
      { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
    ],
  },
  sale_source: 'custom',
  override: {
    id: 12,
    adapter: 'codex',
    model_pattern: 'gpt-custom',
    billing_mode: 'token',
    input_price: 0.000006,
    output_price: null,
    cache_write_price: null,
    cache_read_price: 0,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [
      {
        min_tokens: 0,
        max_tokens: 100,
        input_price: 0.000001,
        output_price: 0.000002,
        cache_write_price: 0,
        cache_read_price: null,
        sort_order: 0,
      },
      { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
    ],
    status: 'active',
  },
  intervals: [
    {
      min_tokens: 0,
      max_tokens: 100,
      input_price: 0.000001,
      output_price: 0.000002,
      cache_write_price: 0,
      cache_read_price: null,
      sort_order: 0,
    },
    { min_tokens: 100, max_tokens: null, input_price: 0.000008, sort_order: 1 },
  ],
}

const imageRow = {
  ...inheritedRow,
  model_pattern: 'image-gen',
  upstream_model: 'image-gen-upstream',
  billing_mode: 'image',
  official_pricing: {
    ...inheritedRow.official_pricing,
    input_price: null,
    output_price: null,
    cache_read_price: null,
    image_input_price: 0.000002,
    image_output_price: 0.000003,
    per_request_price: 0.03,
  },
  sale_pricing: {
    ...inheritedRow.sale_pricing,
    input_price: null,
    output_price: null,
    cache_read_price: null,
    image_input_price: 0.0000025,
    image_output_price: 0.0000035,
    per_request_price: 0.04,
  },
  sale_source: 'custom',
  override: {
    id: 30,
    adapter: 'codex',
    model_pattern: 'image-gen',
    billing_mode: 'image',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: 0.0000025,
    image_output_price: 0.0000035,
    per_request_price: 0.04,
    intervals: [{ min_tokens: 0, max_tokens: null, per_request_price: 0.04, sort_order: 0 }],
    status: 'active',
  },
  intervals: [{ min_tokens: 0, max_tokens: null, per_request_price: 0.04, sort_order: 0 }],
}

function emptyOverride(id: number, adapter: string, modelPattern: string) {
  return {
    id,
    adapter,
    model_pattern: modelPattern,
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    status: 'active',
  }
}

const exactEmptyRow = {
  ...inheritedRow,
  model_pattern: 'gpt-exact',
  upstream_model: 'gpt-exact',
  sale_source: 'custom',
  override: emptyOverride(41, ' CODEX ', ' GPT-EXACT '),
}

const exactTokenRowWithHiddenPrice = {
  ...inheritedRow,
  model_pattern: 'gpt-exact-hidden',
  upstream_model: 'gpt-exact-hidden',
  sale_pricing: {
    ...inheritedRow.sale_pricing,
    input_price: 0.000006,
    per_request_price: 0.04,
    intervals: [],
  },
  sale_source: 'custom',
  override: {
    ...emptyOverride(43, 'codex', 'gpt-exact-hidden'),
    input_price: 0.000006,
    per_request_price: 0.04,
  },
  intervals: [],
}

const wildcardEmptyRow = {
  ...inheritedRow,
  model_pattern: 'gpt-*',
  upstream_model: 'gpt-wildcard-match',
  sale_source: 'custom',
  override: emptyOverride(42, 'codex', 'gpt-*'),
}

function perRequestRow(modelPattern: string, official: number | null, sale: number | null) {
  return {
    ...inheritedRow,
    model_pattern: modelPattern,
    upstream_model: modelPattern,
    billing_mode: 'per_request',
    official_pricing: { ...inheritedRow.official_pricing, per_request_price: official },
    sale_pricing: { ...inheritedRow.sale_pricing, per_request_price: sale },
    sale_source: sale === official ? 'official' : 'custom',
    override: sale === official ? null : {
      ...emptyOverride(50 + modelPattern.length, 'codex', modelPattern),
      billing_mode: 'per_request',
      per_request_price: sale,
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const AppLayoutStub = defineComponent({ template: '<div><slot /></div>' })

const mountedWrappers: Array<{ unmount: () => void }> = []

function mountView() {
  const wrapper = mount(ModelPricingView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: true,
        Teleport: true,
      },
    },
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

describe('ModelPricingView', () => {
  beforeEach(() => {
    for (const mock of Object.values(modelPricing)) mock.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    modelPricing.catalog.mockResolvedValue([inheritedRow, customRow])
    modelPricing.list.mockResolvedValue([customRow.override])
    modelPricing.upsertPlatformSale.mockResolvedValue(customRow.override)
    modelPricing.remove.mockResolvedValue(undefined)
    modelPricing.create.mockResolvedValue(customRow.override)
  })

  afterEach(() => {
    while (mountedWrappers.length) mountedWrappers.pop()?.unmount()
    document.body.classList.remove('modal-open')
    vi.restoreAllMocks()
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
    expect(wrapper.text()).toContain('$6')
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
    expect(modelPricing.list).toHaveBeenCalledTimes(2)
  })

  it('uses per-image request pricing as the image primary price and scales image token fields', async () => {
    modelPricing.catalog.mockResolvedValue([imageRow])
    modelPricing.list.mockResolvedValue([imageRow.override])
    const wrapper = mountView()
    await flushPromises()

    const row = wrapper.get('[data-testid="catalog-row"]')
    expect(row.text()).toContain('$0.04')
    expect(row.text()).not.toContain('$2.5')

    await wrapper.get('[data-testid="edit-sale-7-image-gen"]').trigger('click')
    await flushPromises()
    const editor = wrapper.get('[data-testid="sale-editor"]')
    expect(editor.get('[data-price-field="image_input_price"]').text()).toContain('USD / 1M Token')
    expect(editor.get<HTMLInputElement>('[data-price-field="image_input_price"] [data-testid="custom-price-input"]').element.value).toBe('2.5')
    expect(editor.get('[data-price-field="per_request_price"]').text()).toContain('每张图片价格')
    expect(editor.get('[data-price-field="per_request_price"]').text()).toContain('USD / 图片')
    expect(editor.get('[data-testid="interval-row"] [data-price-field="per_request_price"]').text()).toContain('USD / 图片')

    await editor.get('[data-price-field="image_input_price"] [data-testid="custom-price-input"]').setValue('3')
    await editor.get('[data-price-field="per_request_price"] [data-testid="custom-price-input"]').setValue('0.05')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.upsertPlatformSale).toHaveBeenCalledWith(expect.objectContaining({
      billing_mode: 'image',
      image_input_price: 0.000003,
      image_output_price: 0.0000035,
      per_request_price: 0.05,
      intervals: [expect.objectContaining({ per_request_price: 0.04 })],
    }))
  })

  it('keeps per-request mode labeled in request units', async () => {
    const requestRow = perRequestRow('request-model', 0.02, 0.03)
    modelPricing.catalog.mockResolvedValue([requestRow])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-request-model"]').trigger('click')
    const field = wrapper.get('[data-price-field="per_request_price"]')
    expect(field.text()).toContain('USD / 次')
    expect(field.text()).not.toContain('USD / 图片')
  })

  it('shows image token and per-image prices in image details while intervals remain per image', async () => {
    modelPricing.catalog.mockResolvedValue([imageRow])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="catalog-row"] button').trigger('click')
    const details = wrapper.get('[data-testid="catalog-interval-0"]').element.parentElement?.parentElement
    expect(wrapper.text()).toContain('$2')
    expect(wrapper.text()).toContain('$3')
    expect(wrapper.text()).toContain('USD / 1M Token')
    expect(details?.textContent).toContain('$0.04 USD / 图片')
  })

  it('removes an exact empty platform override to restore inheritance and reloads both datasets', async () => {
    modelPricing.catalog.mockResolvedValue([exactEmptyRow])
    modelPricing.list.mockResolvedValue([exactEmptyRow.override])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-exact"]').trigger('click')
    await wrapper.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.remove).toHaveBeenCalledWith(41)
    expect(modelPricing.upsertPlatformSale).not.toHaveBeenCalled()
    expect(modelPricing.catalog).toHaveBeenCalledTimes(2)
    expect(modelPricing.list).toHaveBeenCalledTimes(2)
  })

  it('never removes a matched wildcard when saving an empty platform draft', async () => {
    modelPricing.catalog.mockResolvedValue([wildcardEmptyRow])
    modelPricing.list.mockResolvedValue([wildcardEmptyRow.override])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-*"]').trigger('click')
    await wrapper.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.remove).not.toHaveBeenCalled()
    expect(modelPricing.upsertPlatformSale).toHaveBeenCalledWith(expect.objectContaining({
      platform_id: 7,
      model_pattern: 'gpt-*',
    }))
  })

  it('removes an exact token override when all visible prices inherit despite a hidden historical price', async () => {
    modelPricing.catalog.mockResolvedValue([exactTokenRowWithHiddenPrice])
    modelPricing.list.mockResolvedValue([exactTokenRowWithHiddenPrice.override])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-exact-hidden"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    await editor.get('[data-price-field="input_price"] [data-mode="inherit"]').trigger('click')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    await flushPromises()

    expect(modelPricing.remove).toHaveBeenCalledWith(43)
    expect(modelPricing.upsertPlatformSale).not.toHaveBeenCalled()
  })

  it('marks exact-override deletion previews as resolved after save', async () => {
    modelPricing.catalog.mockResolvedValue([exactTokenRowWithHiddenPrice])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-exact-hidden"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    await editor.get('[data-price-field="input_price"] [data-mode="inherit"]').trigger('click')

    const effective = editor.get('[data-testid="effective-price-input_price"]')
    const difference = editor.get('[data-testid="draft-difference-input_price"]')
    expect(effective.text()).toContain('保存后重新解析')
    expect(difference.text()).toContain('保存后重新解析')
    expect(effective.text()).not.toContain('$5')
    expect(difference.text()).not.toContain('0%')
  })

  it('keeps the latest filtered catalog when an older unfiltered request resolves later', async () => {
    const oldRequest = deferred<typeof inheritedRow[]>()
    const latestRequest = deferred<typeof customRow[]>()
    modelPricing.catalog
      .mockImplementationOnce(() => oldRequest.promise)
      .mockImplementationOnce(() => latestRequest.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('form input').setValue('custom')
    await wrapper.get('form').trigger('submit')
    latestRequest.resolve([customRow])
    await flushPromises()
    oldRequest.resolve([inheritedRow])
    await flushPromises()

    expect(wrapper.findAll('[data-testid="catalog-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('gpt-custom')
    expect(wrapper.text()).not.toContain('gpt-5.6-sol-upstream')
    expect(wrapper.findAll('form select option')).toHaveLength(1)
  })

  it('ignores an older catalog failure after the latest filtered request succeeds', async () => {
    const oldRequest = deferred<typeof inheritedRow[]>()
    const latestRequest = deferred<typeof customRow[]>()
    modelPricing.catalog
      .mockImplementationOnce(() => oldRequest.promise)
      .mockImplementationOnce(() => latestRequest.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('form input').setValue('custom')
    await wrapper.get('form').trigger('submit')
    latestRequest.resolve([customRow])
    await flushPromises()
    oldRequest.reject(new Error('stale request failed'))
    await flushPromises()

    expect(wrapper.find('[data-testid="catalog-load-error"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('gpt-custom')
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps rules loaded after saving when the initial rules response resolves later', async () => {
    const initialRules = deferred<Array<typeof customRow.override>>()
    const savedRules = deferred<Array<typeof customRow.override>>()
    const savedRule = { ...customRow.override, id: 70, model_pattern: 'gpt-saved-rule' }
    modelPricing.list
      .mockImplementationOnce(() => initialRules.promise)
      .mockImplementationOnce(() => savedRules.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    await wrapper.get('[data-testid="sale-save"]').trigger('click')
    savedRules.resolve([savedRule])
    await flushPromises()
    initialRules.resolve([customRow.override])
    await flushPromises()
    await wrapper.get('[data-testid="advanced-toggle"]').trigger('click')

    expect(wrapper.get('[data-testid="advanced-rule-row"]').text()).toContain('gpt-saved-rule')
    expect(wrapper.get('[data-testid="advanced-rule-row"]').text()).not.toContain('gpt-custom')
  })

  it('ignores an older rules failure after rules loaded by saving succeed', async () => {
    const initialRules = deferred<Array<typeof customRow.override>>()
    const savedRules = deferred<Array<typeof customRow.override>>()
    const savedRule = { ...customRow.override, id: 71, model_pattern: 'gpt-saved-rule' }
    modelPricing.list
      .mockImplementationOnce(() => initialRules.promise)
      .mockImplementationOnce(() => savedRules.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    await wrapper.get('[data-testid="sale-save"]').trigger('click')
    savedRules.resolve([savedRule])
    await flushPromises()
    initialRules.reject(new Error('stale rules failed'))
    await flushPromises()
    await wrapper.get('[data-testid="advanced-toggle"]').trigger('click')

    expect(wrapper.find('[data-testid="rules-load-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="advanced-rule-row"]').text()).toContain('gpt-saved-rule')
    expect(showError).not.toHaveBeenCalled()
  })

  it('shows percentage differences in the catalog including official zero edge cases', async () => {
    modelPricing.catalog.mockResolvedValue([
      customRow,
      perRequestRow('zero-same', 0, 0),
      perRequestRow('zero-raised', 0, 0.01),
      perRequestRow('missing-official', null, 0.01),
    ])
    const wrapper = mountView()
    await flushPromises()

    const rows = wrapper.findAll('[data-testid="catalog-row"]')
    expect(rows[0].text()).toContain('+20%')
    expect(rows[1].text()).toContain('0%')
    expect(rows[2].text()).toContain('暂无')
    expect(rows[3].text()).toContain('暂无')
  })

  it('previews effective sale prices and percentage differences as the draft changes', async () => {
    const zeroOfficialRow = perRequestRow('zero-preview', 0, 0.01)
    modelPricing.catalog.mockResolvedValue([customRow, zeroOfficialRow])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    let editor = wrapper.get('[data-testid="sale-editor"]')
    expect(editor.get('[data-testid="effective-price-input_price"]').text()).toContain('$6')
    expect(editor.get('[data-testid="draft-difference-input_price"]').text()).toContain('+20%')
    await editor.get('[data-price-field="input_price"] [data-mode="inherit"]').trigger('click')
    expect(editor.get('[data-testid="effective-price-input_price"]').text()).toContain('$5')
    expect(editor.get('[data-testid="draft-difference-input_price"]').text()).toContain('0%')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    await wrapper.get('[data-testid="edit-sale-7-zero-preview"]').trigger('click')
    editor = wrapper.get('[data-testid="sale-editor"]')
    expect(editor.get('[data-testid="draft-difference-per_request_price"]').text()).toContain('暂无')
    await editor.get('[data-price-field="per_request_price"] [data-mode="zero"]').trigger('click')
    expect(editor.get('[data-testid="effective-price-per_request_price"]').text()).toContain('$0')
    expect(editor.get('[data-testid="draft-difference-per_request_price"]').text()).toContain('0%')
  })

  it('uses dialog escape, initial focus, focus restoration, and body scroll locking', async () => {
    const wrapper = mountView()
    await flushPromises()
    const trigger = wrapper.get<HTMLElement>('[data-testid="edit-sale-7-gpt-custom"]')
    trigger.element.focus()

    await trigger.trigger('click')
    await flushPromises()

    const dialog = wrapper.get('[role="dialog"]')
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(dialog.element.contains(document.activeElement)).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="sale-editor"]').exists()).toBe(false)
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
  })

  it('prevents closing, reopening, and duplicate platform sale submits while saving', async () => {
    const saveRequest = deferred<typeof customRow.override>()
    modelPricing.upsertPlatformSale.mockImplementationOnce(() => saveRequest.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    await editor.get('[data-testid="sale-save"]').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.get('[data-testid="edit-sale-7-gpt-5.6-sol"]').trigger('click')
    await flushPromises()

    expect(modelPricing.upsertPlatformSale).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="sale-editor"]').text()).toContain('gpt-custom')

    saveRequest.resolve(customRow.override)
    await flushPromises()
    expect(wrapper.find('[data-testid="sale-editor"]').exists()).toBe(false)
  })

  it('prevents closing and duplicate advanced rule submits while saving', async () => {
    const saveRequest = deferred<typeof customRow.override>()
    modelPricing.create.mockImplementationOnce(() => saveRequest.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="advanced-toggle"]').trigger('click')
    await wrapper.get('[data-testid="create-advanced-rule"]').trigger('click')
    const editor = wrapper.get('[data-testid="advanced-editor"]')
    const inputs = editor.findAll('input')
    await inputs[0].setValue('codex')
    await inputs[1].setValue('gpt-new')
    const save = editor.findAll('button').find(button => button.text() === '保存')
    expect(save).toBeDefined()
    await save!.trigger('click')
    await save!.trigger('click')
    const cancel = editor.findAll('button').find(button => button.text() === '取消')
    expect(cancel).toBeDefined()
    await cancel!.trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(modelPricing.create).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="advanced-editor"]').exists()).toBe(true)

    saveRequest.resolve(customRow.override)
    await flushPromises()
    expect(wrapper.find('[data-testid="advanced-editor"]').exists()).toBe(false)
  })

  it('shows immutable official prices beside editable platform prices', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="edit-sale-7-gpt-custom"]').trigger('click')
    const editor = wrapper.get('[data-testid="sale-editor"]')
    const officialInput = editor.get('[data-testid="official-price-input_price"]')

    expect(officialInput.text()).toContain('$5')
    expect(officialInput.text()).toContain('USD / 1M Token')
    expect(officialInput.find('input').exists()).toBe(false)
    expect(editor.get<HTMLInputElement>('[data-price-field="input_price"] [data-testid="custom-price-input"]').element.value).toBe('6')
    expect(editor.get('[data-testid="official-price-cache_write_price"]').text()).toContain('admin.modelPricing.noPrice')
    expect(editor.get('[data-testid="official-price-cache_read_price"]').text()).toContain('$0')
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

  it('shows all token price fields for expanded intervals', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="catalog-row"]')[1].get('button').trigger('click')

    const interval = wrapper.get('[data-testid="catalog-interval-0"]')
    expect(interval.get('[data-testid="interval-price-input_price"]').text()).toContain('$1')
    expect(interval.get('[data-testid="interval-price-output_price"]').text()).toContain('$2')
    expect(interval.get('[data-testid="interval-price-cache_write_price"]').text()).toContain('$0')
    expect(interval.get('[data-testid="interval-price-cache_read_price"]').text()).toContain('admin.modelPricing.noPrice')
    expect(interval.text()).toContain('USD / 1M Token')
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

  it('uses the delete failure fallback when removing an advanced rule fails', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(true)
    modelPricing.remove.mockRejectedValueOnce({})
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="advanced-toggle"]').trigger('click')
    await wrapper.get('[data-testid="advanced-rule-row"] button[aria-label="common.delete"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('模型价格删除失败')
    expect(showError).not.toHaveBeenCalledWith('模型价格保存失败')
    confirm.mockRestore()
  })
})
