import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AmountInput from '../AmountInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
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
})
