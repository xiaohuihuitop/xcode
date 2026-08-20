import { describe, expect, it } from 'vitest'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

describe('subscription purchase semantics', () => {
  it('uses listing and independent-purchase wording in Chinese', () => {
    expect(zh.nav.paymentPlans).toBe('上架订阅')
    expect(zh.payment.addAnother).toBe('再来一个')
    expect(zh.payment.independentSubscriptionNotice).toContain('不会累计时长')
    expect(zh.payment.independentSubscriptionNotice).toContain('叠加使用')
  })

  it('keeps the English copy semantically equivalent', () => {
    expect(en.nav.paymentPlans).toBe('Listed Subscriptions')
    expect(en.payment.addAnother).toBe('Add another')
    expect(en.payment.independentSubscriptionNotice).toContain('does not extend')
  })
})
