import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'
import { formatPaymentAmount } from '../currency'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'zh-CN' },
    t: (key: string) => ({
      'payment.customAmountPrefix': '自定义',
      'payment.customAmountHighlight': '支付',
      'payment.customAmountSuffix': '金额',
      'payment.minimumRechargeAmount': '最小充值额度',
      'payment.maximumRechargeAmount': '最大充值额度',
      'payment.noLimit': '不限制',
    }[key] ?? key),
  }),
}))

describe('AmountInput', () => {
  it('hides quick amount choices when no quick amounts are provided', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: 1,
        amounts: [],
      },
      global: {
        mocks: {
          $t: (key: string) => key,
        },
      },
    })

    expect(wrapper.text()).not.toContain('payment.quickAmounts')
    expect(wrapper.find('input').element).toHaveProperty('value', '1')
  })

  it.each([
    ['CNY', '¥'],
    ['USD', '$'],
  ])('uses the %s payment currency symbol in the amount field', (currency, symbol) => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [],
        currency,
      },
    })

    expect(wrapper.get('[data-testid="amount-currency-symbol"]').text()).toBe(symbol)
  })

  it('labels the field as custom payment amount and emphasizes payment in red', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [],
      },
    })

    const label = wrapper.get('label[for="recharge-amount"]')
    const emphasis = label.get('[data-testid="custom-amount-highlight"]')
    expect(label.text()).toBe('自定义支付金额')
    expect(emphasis.text()).toBe('支付')
    expect(emphasis.classes()).toEqual(expect.arrayContaining(['text-red-600', 'dark:text-red-400']))
  })

  it('shows the configured minimum and maximum recharge amounts in the payment currency', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [],
        currency: 'CNY',
        configuredMin: 10,
        configuredMax: 5000,
      },
    })

    expect(wrapper.get('[data-testid="minimum-recharge-amount"]').text()).toContain(
      formatPaymentAmount(10, 'CNY', 'zh-CN'),
    )
    expect(wrapper.get('[data-testid="maximum-recharge-amount"]').text()).toContain(
      formatPaymentAmount(5000, 'CNY', 'zh-CN'),
    )
  })

  it('shows unlimited when a configured recharge boundary is zero', () => {
    const wrapper = mount(AmountInput, {
      props: {
        modelValue: null,
        amounts: [],
        configuredMin: 0,
        configuredMax: 0,
      },
    })

    expect(wrapper.get('[data-testid="minimum-recharge-amount"]').text()).toContain('不限制')
    expect(wrapper.get('[data-testid="maximum-recharge-amount"]').text()).toContain('不限制')
  })
})
