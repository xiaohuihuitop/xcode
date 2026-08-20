import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminPaymentPlansView from '../AdminPaymentPlansView.vue'

const { getPlans, getConfig, getGroups, getGlobalBalanceRateMultiplier, updateGlobalBalanceRateMultiplier } = vi.hoisted(() => ({
  getPlans: vi.fn(),
  getConfig: vi.fn(),
  getGroups: vi.fn(),
  getGlobalBalanceRateMultiplier: vi.fn(),
  updateGlobalBalanceRateMultiplier: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getPlans,
    getConfig,
    getGlobalBalanceRateMultiplier,
    updateGlobalBalanceRateMultiplier,
  },
}))

vi.mock('@/api/admin', () => ({
  default: {
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-price" :value="row.price" :row="row" />
        <slot name="cell-rate_multiplier" :value="row.rate_multiplier" :row="row" />
        <slot name="cell-limits" :value="row.daily_limit_usd" :row="row" />
      </div>
    </div>
  `,
}

describe('AdminPaymentPlansView', () => {
  beforeEach(() => {
	vi.clearAllMocks()
    getGroups.mockResolvedValue([])
    getConfig.mockResolvedValue({ data: {} })
    getGlobalBalanceRateMultiplier.mockResolvedValue({ data: { rate_multiplier: 1.25 } })
    updateGlobalBalanceRateMultiplier.mockResolvedValue({ data: { rate_multiplier: 0.6 } })
    getPlans.mockResolvedValue({
      data: [
        {
          id: 1,
          name: 'CNY plan',
          price: 499,
          original_price: 599,
          currency: 'CNY',
          validity_days: 30,
          validity_unit: 'day',
          rate_multiplier: 0.5,
          daily_limit_usd: 20,
          weekly_limit_usd: null,
          monthly_limit_usd: 200,
          sort_order: 0,
          for_sale: true,
          features: [],
        },
        {
          id: 2,
          name: 'Legacy plan',
          price: 10,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          rate_multiplier: 1,
          daily_limit_usd: null,
          weekly_limit_usd: null,
          monthly_limit_usd: null,
          sort_order: 0,
          for_sale: true,
          features: [],
        },
      ],
    })
  })

  it('uses the configured currency symbol and keeps legacy prices in USD', async () => {
    const wrapper = mount(AdminPaymentPlansView, {
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          DataTable: DataTableStub,
          ConfirmDialog: true,
          GroupBadge: true,
          Icon: true,
          PlanEditDialog: true,
        },
      },
    })

    await flushPromises()
    expect(getGroups).not.toHaveBeenCalled()

    expect(wrapper.text()).toContain('¥499.00CNY')
    expect(wrapper.text()).toContain('¥599.00')
    expect(wrapper.text()).toContain('$10.00')
  })

  it('renders subscription multiplier and limits from the plan, not the routing group', async () => {
    const wrapper = mount(AdminPaymentPlansView, {
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          DataTable: DataTableStub,
          ConfirmDialog: true,
          GroupBadge: true,
          Icon: true,
          PlanEditDialog: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('0.50x')
    expect(wrapper.text()).toContain('$20.00')
    expect(wrapper.text()).toContain('$200.00')
  })

  it('loads and saves the global balance multiplier separately from subscription plans', async () => {
    const wrapper = mount(AdminPaymentPlansView, {
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          DataTable: DataTableStub,
          ConfirmDialog: true,
          Icon: true,
          PlanEditDialog: true,
        },
      },
    })

    await flushPromises()

    const input = wrapper.get('[data-testid="global-balance-rate-multiplier"]')
    expect((input.element as HTMLInputElement).value).toBe('1.25')

    await input.setValue('0.6')
    await wrapper.get('[data-testid="save-global-balance-rate-multiplier"]').trigger('click')
    await flushPromises()

    expect(getGlobalBalanceRateMultiplier).toHaveBeenCalledTimes(1)
    expect(updateGlobalBalanceRateMultiplier).toHaveBeenCalledWith(0.6)
  })
})
