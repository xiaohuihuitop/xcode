import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { PlatformPlazaModel, PlatformPlazaPricing } from '@/api/modelPlaza'

const translations: Record<string, string> = {
  'modelPlaza.table.model': '模型',
  'modelPlaza.table.officialPrice': '官方公开价（参考成本）',
  'modelPlaza.table.salePrice': '平台售价',
  'modelPlaza.table.input': '输入',
  'modelPlaza.table.output': '输出',
  'modelPlaza.table.cacheWrite': '缓存写入',
  'modelPlaza.table.cacheRead': '缓存读取',
  'modelPlaza.table.imageInput': '图片输入',
  'modelPlaza.table.imageOutput': '图片输出',
  'modelPlaza.table.perRequest': '按次',
  'modelPlaza.table.perImage': '按图片',
  'modelPlaza.table.inheritedPrice': '继承官方价',
  'modelPlaza.table.customPrice': '自定义售价',
  'modelPlaza.table.noPrice': '暂无价格',
  'modelPlaza.table.unitPerMillion': 'USD/1M Token',
  'modelPlaza.table.unitPerRequest': 'USD/次',
  'modelPlaza.table.unitPerImage': 'USD/图片',
  'modelPlaza.table.tiers': '阶梯',
  'modelPlaza.table.range': '范围',
  'modelPlaza.table.rangeBounded': '{min}–{max} Tokens',
  'modelPlaza.table.rangeUnbounded': '{min}+ Tokens',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, values?: Record<string, number>) => (translations[key] ?? key).replace(
      /\{(\w+)\}/g,
      (_, name: string) => String(values?.[name] ?? `{${name}}`),
    ),
  }),
}))

import PlatformModelPricingTable from '../PlatformModelPricingTable.vue'

type PricingFixture = PlatformPlazaPricing
type ModelFixture = PlatformPlazaModel & {
  official_pricing?: PricingFixture | null
  sale_pricing?: PricingFixture | null
  sale_pricing_source?: 'official' | 'custom' | 'unavailable'
  official_source?: string
  internal_path?: string
  override?: unknown
}

function pricing(
  billingMode: string,
  values: Partial<PricingFixture> = {},
): PricingFixture {
  return {
    billing_mode: billingMode,
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    ...values,
  }
}

const tokenOfficial = pricing('token', {
  input_price: 0.000001,
  output_price: 0.000002,
  cache_write_price: 0.0000005,
  cache_read_price: 0.00000025,
})
const tokenSale = pricing('token', {
  input_price: 0.0000015,
  output_price: 0.0000025,
  cache_write_price: 0.00000075,
  cache_read_price: 0.0000003,
})

const fixtures: ModelFixture[] = [
  {
    pattern: 'token-custom-model',
    endpoint_capabilities: [],
    pricing: tokenSale,
    official_pricing: tokenOfficial,
    sale_pricing: tokenSale,
    sale_pricing_source: 'custom',
  },
  {
    pattern: 'token-inherited-model',
    endpoint_capabilities: [],
    pricing: tokenOfficial,
    official_pricing: tokenOfficial,
    sale_pricing: tokenOfficial,
    sale_pricing_source: 'official',
  },
  {
    pattern: 'explicit-zero-model',
    endpoint_capabilities: [],
    pricing: pricing('token', { input_price: 0 }),
    official_pricing: pricing('token', { input_price: 0 }),
    sale_pricing: pricing('token', { input_price: 0 }),
    sale_pricing_source: 'official',
  },
  {
    pattern: 'missing-official-model',
    endpoint_capabilities: [],
    pricing: pricing('token', { input_price: 0.000004 }),
    official_pricing: null,
    sale_pricing: pricing('token', { input_price: 0.000004 }),
    sale_pricing_source: 'custom',
  },
  {
    pattern: 'request-model',
    endpoint_capabilities: [],
    pricing: pricing('per_request', {
      per_request_price: 0.02,
      intervals: [
        { min_tokens: 0, max_tokens: 10, tier_label: 'Regular calls', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.018 },
        { min_tokens: 11, max_tokens: null, tier_label: 'Volume calls', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.015 },
      ],
    }),
    official_pricing: pricing('per_request', { per_request_price: 0.01 }),
    sale_pricing: pricing('per_request', {
      per_request_price: 0.02,
      intervals: [
        { min_tokens: 0, max_tokens: 10, tier_label: 'Regular calls', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.018 },
        { min_tokens: 11, max_tokens: null, tier_label: 'Volume calls', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.015 },
      ],
    }),
    sale_pricing_source: 'custom',
  },
  {
    pattern: 'image-model',
    endpoint_capabilities: [],
    pricing: pricing('image', {
      image_input_price: 0.000003,
      image_output_price: 0.000004,
      per_request_price: 0.08,
      intervals: [
        { min_tokens: 0, max_tokens: 2, tier_label: 'Small batch', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.07 },
        { min_tokens: 3, max_tokens: null, tier_label: 'Large batch', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.05 },
      ],
    }),
    official_pricing: pricing('image', {
      image_input_price: 0.000002,
      image_output_price: 0.000003,
      per_request_price: 0.06,
    }),
    sale_pricing: pricing('image', {
      image_input_price: 0.000003,
      image_output_price: 0.000004,
      per_request_price: 0.08,
      intervals: [
        { min_tokens: 0, max_tokens: 2, tier_label: 'Small batch', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.07 },
        { min_tokens: 3, max_tokens: null, tier_label: 'Large batch', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.05 },
      ],
    }),
    sale_pricing_source: 'custom',
  },
  {
    pattern: 'token-tier-model',
    endpoint_capabilities: [],
    pricing: pricing('token', {
      intervals: [
        { min_tokens: 0, max_tokens: 1000, tier_label: 'Standard context', input_price: 0.000001, output_price: 0.000002, cache_write_price: 0.0000005, cache_read_price: 0.00000025, per_request_price: null },
        { min_tokens: 1001, max_tokens: null, tier_label: 'Long context', input_price: 0.000002, output_price: 0.000004, cache_write_price: 0.000001, cache_read_price: 0.0000005, per_request_price: null },
      ],
    }),
    sale_pricing: pricing('token', {
      intervals: [
        { min_tokens: 0, max_tokens: 1000, tier_label: 'Standard context', input_price: 0.000001, output_price: 0.000002, cache_write_price: 0.0000005, cache_read_price: 0.00000025, per_request_price: null },
        { min_tokens: 1001, max_tokens: null, tier_label: 'Long context', input_price: 0.000002, output_price: 0.000004, cache_write_price: 0.000001, cache_read_price: 0.0000005, per_request_price: null },
      ],
    }),
    sale_pricing_source: 'custom',
  },
  {
    pattern: 'legacy-only-model',
    endpoint_capabilities: [],
    pricing: pricing('per_request', { per_request_price: 0.03 }),
  },
]

function mountTable(models: ModelFixture[] = fixtures) {
  return mount(PlatformModelPricingTable, {
    props: { models: models as PlatformPlazaModel[] },
  })
}

describe('PlatformModelPricingTable', () => {
  it('renders exactly model, official public price, and platform sale price columns on desktop', () => {
    const wrapper = mountTable([fixtures[0]])
    const headings = wrapper.findAll('[data-testid="desktop-pricing-table"] th').map((item) => item.text())

    expect(headings).toEqual(['模型', '官方公开价（参考成本）', '平台售价'])
    expect(wrapper.get('[data-testid="desktop-official-token-custom-model"]').text()).toContain('输入$1.00 USD/1M Token')
    expect(wrapper.get('[data-testid="desktop-official-token-custom-model"]').text()).toContain('输出$2.00 USD/1M Token')
    expect(wrapper.get('[data-testid="desktop-sale-token-custom-model"]').text()).toContain('输入$1.50 USD/1M Token')
    expect(wrapper.get('[data-testid="desktop-sale-token-custom-model"]').text()).toContain('缓存读取$0.30 USD/1M Token')
  })

  it('hides pricing-source badges while preserving zero and missing official pricing', () => {
    const wrapper = mountTable([fixtures[1], fixtures[2], fixtures[3]])

    expect(wrapper.get('[data-testid="desktop-sale-token-inherited-model"]').text()).not.toContain('继承官方价')
    expect(wrapper.get('[data-testid="mobile-model-token-inherited-model"]').text()).not.toContain('继承官方价')
    expect(wrapper.get('[data-testid="desktop-official-explicit-zero-model"]').text()).toContain('$0.00 USD/1M Token')
    expect(wrapper.get('[data-testid="desktop-sale-explicit-zero-model"]').text()).toContain('$0.00 USD/1M Token')
    expect(wrapper.get('[data-testid="desktop-official-missing-official-model"]').text()).toBe('暂无价格')
    expect(wrapper.get('[data-testid="desktop-sale-missing-official-model"]').text()).not.toContain('自定义售价')
    expect(wrapper.get('[data-testid="mobile-model-missing-official-model"]').text()).not.toContain('自定义售价')
  })

  it('marks only comparable changed platform sale values in red', () => {
    const crossModeModel: ModelFixture = {
      pattern: 'cross-mode-model',
      endpoint_capabilities: [],
      pricing: pricing('per_request', { per_request_price: 0.03 }),
      official_pricing: pricing('token', { input_price: 0.000001 }),
      sale_pricing: pricing('per_request', { per_request_price: 0.03 }),
      sale_pricing_source: 'custom',
    }
    const wrapper = mountTable([fixtures[0], fixtures[1], fixtures[3], crossModeModel])

    const changedSale = wrapper.get('[data-testid="desktop-sale-token-custom-model"] [data-price-key="input_price"]')
    const inheritedSale = wrapper.get('[data-testid="desktop-sale-token-inherited-model"] [data-price-key="input_price"]')
    const missingOfficialSale = wrapper.get('[data-testid="desktop-sale-missing-official-model"] [data-price-key="input_price"]')
    const crossModeSale = wrapper.get('[data-testid="desktop-sale-cross-mode-model"] [data-price-key="per_request_price"]')

    expect(changedSale.classes()).toContain('text-red-600')
    expect(changedSale.classes()).toContain('dark:text-red-400')
    expect(inheritedSale.classes()).not.toContain('text-red-600')
    expect(missingOfficialSale.classes()).not.toContain('text-red-600')
    expect(crossModeSale.classes()).not.toContain('text-red-600')
    expect(wrapper.text()).not.toContain('自定义售价')
  })

  it('uses per-request and mutually exclusive per-image units without showing image token prices', () => {
    const wrapper = mountTable([fixtures[4], fixtures[5]])
    const requestSale = wrapper.get('[data-testid="desktop-sale-request-model"]').text()
    const imageSale = wrapper.get('[data-testid="desktop-sale-image-model"]').text()

    expect(requestSale).toContain('按次$0.02 USD/次')
    expect(requestSale).not.toContain('USD/1M Token')
    expect(imageSale).toContain('按图片$0.08 USD/图片')
    expect(imageSale).not.toContain('图片输入')
    expect(imageSale).not.toContain('图片输出')
    expect(imageSale).not.toContain('USD/1M Token')
  })

  it('renders tier labels, ranges, and mode-correct interval prices for multiple tiers', () => {
    const wrapper = mountTable([fixtures[4], fixtures[5], fixtures[6]])
    const requestSale = wrapper.get('[data-testid="desktop-sale-request-model"]').text()
    const requestMobile = wrapper.get('[data-testid="mobile-model-request-model"]').text()
    const imageSale = wrapper.get('[data-testid="desktop-sale-image-model"]').text()
    const tokenSale = wrapper.get('[data-testid="desktop-sale-token-tier-model"]').text()

    expect(requestSale).toContain('Regular calls')
    expect(requestSale).toContain('范围 1–10 Tokens')
    expect(requestSale).toContain('按次$0.018 USD/次')
    expect(requestSale).toContain('Volume calls')
    expect(requestSale).toContain('范围 12+ Tokens')
    expect(requestSale).not.toContain('范围 0–10')
    expect(requestSale).not.toContain('范围 11–不限')
    expect(requestSale).not.toContain('USD/图片')
    expect(requestMobile).toContain('范围 1–10 Tokens')
    expect(requestMobile).toContain('范围 12+ Tokens')
    expect(imageSale).toContain('阶梯')
    expect(imageSale).toContain('Small batch')
    expect(imageSale).toContain('范围 1–2 Tokens')
    expect(imageSale).toContain('按图片$0.07 USD/图片')
    expect(imageSale).toContain('Large batch')
    expect(imageSale).toContain('范围 4+ Tokens')
    expect(tokenSale).toContain('Standard context')
    expect(tokenSale).toContain('范围 1–1000 Tokens')
    expect(tokenSale).toContain('输入$1.00 USD/1M Token')
    expect(tokenSale).toContain('Long context')
    expect(tokenSale).toContain('范围 1002+ Tokens')
    expect(tokenSale).toContain('输出$4.00 USD/1M Token')
  })

  it('does not imply that an exclusive minimum is included when a tier has no label', () => {
    const unlabeledModel: ModelFixture = {
      pattern: 'unlabeled-tier-model',
      endpoint_capabilities: [],
      pricing: pricing('per_request', {
        intervals: [
          { min_tokens: 0, max_tokens: 10, input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.01 },
        ],
      }),
    }
    const sale = mountTable([unlabeledModel]).get('[data-testid="desktop-sale-unlabeled-tier-model"]').text()

    expect(sale).toContain('范围 1–10 Tokens')
    expect(sale).not.toContain('0+')
  })

  it('falls back to legacy pricing as the platform sale price', () => {
    const wrapper = mountTable([fixtures[7]])

    expect(wrapper.get('[data-testid="desktop-official-legacy-only-model"]').text()).toBe('暂无价格')
    expect(wrapper.get('[data-testid="desktop-sale-legacy-only-model"]').text()).toContain('按次$0.03 USD/次')
    expect(wrapper.get('[data-testid="desktop-sale-legacy-only-model"]').text()).not.toContain('继承官方价')
    expect(wrapper.get('[data-testid="desktop-sale-legacy-only-model"]').text()).not.toContain('自定义售价')
  })

  it('uses a stacked labeled mobile layout without a forced-width table wrapper', () => {
    const wrapper = mountTable([fixtures[0]])

    expect(wrapper.find('[data-testid="desktop-pricing-table"]').classes()).toContain('hidden')
    expect(wrapper.find('[data-testid="mobile-pricing-list"]').classes()).toContain('sm:hidden')
    expect(wrapper.get('[data-testid="mobile-model-token-custom-model"]').text()).toContain('官方公开价（参考成本）')
    expect(wrapper.get('[data-testid="mobile-model-token-custom-model"]').text()).toContain('平台售价')
    expect(wrapper.html()).not.toContain('min-w-[560px]')
    expect(wrapper.html()).not.toContain('overflow-x-auto')
  })

  it('never exposes backend source paths or administrator-only pricing details', () => {
    const sensitiveModel: ModelFixture = {
      ...fixtures[0],
      official_source: 'internal/catalog/openai.yaml',
      internal_path: '/srv/config/pricing.json',
      override: { adapter: 'openai', model_pattern: '*' },
    }
    const text = mountTable([sensitiveModel]).text()

    expect(text).not.toContain('official_source')
    expect(text).not.toContain('internal/catalog/openai.yaml')
    expect(text).not.toContain('/srv/config/pricing.json')
    expect(text).not.toContain('override')
    expect(text).not.toContain('administrator')
  })
})
