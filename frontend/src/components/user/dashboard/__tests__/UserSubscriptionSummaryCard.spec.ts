import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserSubscriptionSummaryCard from '../UserSubscriptionSummaryCard.vue'
import type { UserSubscription } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const subscription = {
  id: 1,
  subscription_plan_id: 2,
  status: 'active',
  starts_at: '2026-07-01T00:00:00Z',
  expires_at: '2026-08-01T00:00:00Z',
  daily_usage_usd: 3,
  weekly_usage_usd: 0,
  monthly_usage_usd: 0,
  daily_window_start: '2026-07-11T00:00:00Z',
  weekly_window_start: null,
  monthly_window_start: null,
  plan_name_snapshot: 'Premium plan',
  daily_limit_usd_snapshot: 10,
  weekly_limit_usd_snapshot: null,
  monthly_limit_usd_snapshot: null,
  rate_multiplier_snapshot: 0.8,
  group: {
    name: 'Routing group',
    daily_limit_usd: 99,
  },
} as UserSubscription

describe('UserSubscriptionSummaryCard', () => {
  it('shows the active quota, remaining amount, and progress', () => {
    const wrapper = mount(UserSubscriptionSummaryCard, {
      props: { subscription },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('Premium plan')
    expect(wrapper.text()).not.toContain('Routing group')
    expect(wrapper.text()).toContain('$3.00 / $10.00')
    expect(wrapper.text()).toContain('$7.00')
    expect(wrapper.get('[data-testid="subscription-progress"]').attributes('style')).toContain('30%')
  })

  it('shows unlimited when the subscription has no quota', () => {
    const wrapper = mount(UserSubscriptionSummaryCard, {
      props: {
        subscription: {
          ...subscription,
          plan_name_snapshot: 'Unlimited plan',
          daily_limit_usd_snapshot: null,
          weekly_limit_usd_snapshot: null,
          monthly_limit_usd_snapshot: null,
          group: { name: 'Unlimited' },
        },
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('Unlimited plan')
    expect(wrapper.text()).toContain('dashboard.subscriptionSummary.unlimited')
    expect(wrapper.find('[data-testid="subscription-progress"]').exists()).toBe(false)
  })
})
