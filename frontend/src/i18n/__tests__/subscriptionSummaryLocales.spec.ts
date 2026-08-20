import { describe, expect, it } from 'vitest'

import en from '../locales/en/dashboard'
import zh from '../locales/zh/dashboard'

describe('subscription summary locales', () => {
  it('provides dashboard summary labels for Chinese and English', () => {
    expect(zh.dashboard.subscriptionSummary).toEqual({
      title: '订阅套餐',
      remaining: '剩余',
      unlimited: '不限额',
    })
    expect(en.dashboard.subscriptionSummary).toEqual({
      title: 'Subscription',
      remaining: 'Remaining',
      unlimited: 'Unlimited',
    })
  })
})
