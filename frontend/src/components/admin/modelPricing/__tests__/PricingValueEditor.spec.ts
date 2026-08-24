import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PricingValueEditor from '../PricingValueEditor.vue'

describe('PricingValueEditor', () => {
  it('emits null when inherit is selected', async () => {
    const wrapper = mount(PricingValueEditor, { props: { modelValue: 0.000005 } })

    await wrapper.get('[data-mode="inherit"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[null]])
  })

  it('emits explicit zero when zero is selected', async () => {
    const wrapper = mount(PricingValueEditor, { props: { modelValue: null } })

    await wrapper.get('[data-mode="zero"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[0]])
    expect(wrapper.get('[data-mode="zero"]').attributes('aria-pressed')).toBe('true')
  })

  it('emits a per-token value for a custom per-million-token price', async () => {
    const wrapper = mount(PricingValueEditor, { props: { modelValue: null } })

    await wrapper.get('[data-mode="custom"]').trigger('click')
    await wrapper.get('[data-testid="custom-price-input"]').setValue('5')

    expect(wrapper.emitted('update:modelValue')).toEqual([[0.000005]])
  })

  it('shows an error and does not emit zero when a custom value is cleared', async () => {
    const wrapper = mount(PricingValueEditor, { props: { modelValue: 0.000005 } })

    await wrapper.get('[data-testid="custom-price-input"]').setValue('')

    expect(wrapper.get('[role="alert"]').text()).toContain('required')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows an injected error and reports invalidity for a negative initial value', () => {
    const wrapper = mount(PricingValueEditor, {
      props: {
        modelValue: -0.000005,
        labels: { invalid: 'Localized invalid price.' },
      },
    })

    expect(wrapper.get('[data-mode="custom"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get<HTMLInputElement>('[data-testid="custom-price-input"]').element.value).toBe('-5')
    expect(wrapper.get('[role="alert"]').text()).toBe('Localized invalid price.')
    expect(wrapper.emitted('validity-change')).toEqual([[false]])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
