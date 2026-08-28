import { describe, expect, it, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'

import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'
import type { UserSubscription } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const stats: UserStatsType = {
  total_api_keys: 2,
  active_api_keys: 1,
  total_requests: 12,
  total_input_tokens: 100,
  total_output_tokens: 50,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 150,
  total_cost: 1.25,
  total_actual_cost: 1,
  today_requests: 3,
  today_input_tokens: 30,
  today_output_tokens: 20,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 50,
  today_cost: 0.5,
  today_actual_cost: 0.4,
  average_duration_ms: 250,
  rpm: 1,
  tpm: 10,
  by_platform: [
    {
      platform: 'openai',
      total_requests: 8,
      total_tokens: 100,
      total_actual_cost: 0.8,
      today_requests: 2,
      today_tokens: 30,
      today_actual_cost: 0.3,
    },
    {
      platform: 'anthropic',
      total_requests: 4,
      total_tokens: 50,
      total_actual_cost: 0.2,
      today_requests: 1,
      today_tokens: 20,
      today_actual_cost: 0.1,
    },
  ],
}

describe('UserDashboardStats', () => {
  it('does not render the per-platform breakdown', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats,
        balance: 5,
        isSimple: false,
      },
      global: { stubs: { Icon: true, RouterLink: RouterLinkStub } },
    })

    expect(wrapper.text()).not.toContain('dashboard.platformBreakdown')
    expect(wrapper.text()).not.toContain('OpenAI')
    expect(wrapper.text()).not.toContain('Claude')
    expect(wrapper.text()).toContain('dashboard.todayRequests')
  })

  it('links dashboard action cards to their management pages', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats,
        balance: 5,
        isSimple: false,
        subscriptions: [{
          id: 1,
          status: 'active',
          expires_at: null,
          plan_name_snapshot: 'Premium plan',
          daily_usage_usd: 0,
          daily_limit_usd_snapshot: 10,
          daily_window_start: null,
        } as UserSubscription],
      },
      global: {
        stubs: { Icon: true, RouterLink: RouterLinkStub },
      },
    })

    const links = wrapper.findAllComponents(RouterLinkStub)
    expect(links.map((link) => link.props('to'))).toEqual(['/purchase', '/keys', '/subscriptions'])
  })
})
