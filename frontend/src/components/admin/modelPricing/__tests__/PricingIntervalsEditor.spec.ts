import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PricingIntervalsEditor from '../PricingIntervalsEditor.vue'

describe('PricingIntervalsEditor', () => {
  it('sorts rows by min_tokens and emits normalized sort_order values', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [
          { min_tokens: 100, max_tokens: null, tier_label: 'Large', input_price: 0.000005 },
          { min_tokens: 0, max_tokens: 100, tier_label: 'Small', input_price: 0.000001 },
        ],
      },
    })

    await wrapper.get('[data-testid="tier-label-0"]').setValue('Large context')

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toHaveLength(1)
    expect(emitted![0][0]).toEqual([
      expect.objectContaining({ min_tokens: 0, max_tokens: 100, sort_order: 0 }),
      expect.objectContaining({ min_tokens: 100, max_tokens: null, tier_label: 'Large context', sort_order: 1 }),
    ])
  })

  it('shows only the price fields applicable to the billing mode', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: { billingMode: 'token', modelValue: [{ min_tokens: 0, max_tokens: null }] },
    })

    expect(wrapper.find('[data-price-field="input_price"]').exists()).toBe(true)
    expect(wrapper.find('[data-price-field="output_price"]').exists()).toBe(true)
    expect(wrapper.find('[data-price-field="cache_write_price"]').exists()).toBe(true)
    expect(wrapper.find('[data-price-field="cache_read_price"]').exists()).toBe(true)
    expect(wrapper.find('[data-price-field="per_request_price"]').exists()).toBe(false)

    await wrapper.setProps({ billingMode: 'per_request' })

    expect(wrapper.find('[data-price-field="input_price"]').exists()).toBe(false)
    expect(wrapper.find('[data-price-field="per_request_price"]').exists()).toBe(true)
  })

  it('supports adding and deleting rows', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: 100, tier_label: 'First' }],
      },
    })

    await wrapper.get('[aria-label="Add interval"]').trigger('click')
    expect(wrapper.findAll('[data-testid="interval-row"]')).toHaveLength(2)
    expect(wrapper.emitted('update:modelValue')?.[0][0]).toEqual([
      expect.objectContaining({ min_tokens: 0, sort_order: 0 }),
      expect.objectContaining({ min_tokens: 100, sort_order: 1 }),
    ])

    await wrapper.get('[aria-label="Delete interval 2"]').trigger('click')
    expect(wrapper.findAll('[data-testid="interval-row"]')).toHaveLength(1)
  })

  it.each([
    { field: 'min-tokens-0', value: '-1', message: 'non-negative' },
    { field: 'max-tokens-0', value: '0', message: 'greater than min_tokens' },
  ])('rejects invalid interval bounds for $field', async ({ field, value, message }) => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: 100 }],
      },
    })

    await wrapper.get(`[data-testid="${field}"]`).setValue(value)

    expect(wrapper.get('[role="alert"]').text()).toContain(message)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('rejects overlapping intervals after sorting', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [
          { min_tokens: 0, max_tokens: 100 },
          { min_tokens: 100, max_tokens: null },
        ],
      },
    })

    await wrapper.get('[data-testid="min-tokens-1"]').setValue('50')

    expect(wrapper.get('[role="alert"]').text()).toContain('overlap')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('shows an error immediately when existing intervals overlap', () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [
          { min_tokens: 0, max_tokens: 100 },
          { min_tokens: 50, max_tokens: null },
        ],
      },
    })

    expect(wrapper.get('[role="alert"]').text()).toContain('overlap')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps bound errors visible when the billing mode changes', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: 100 }],
      },
    })

    await wrapper.get('[data-testid="min-tokens-0"]').setValue('-1')
    await wrapper.setProps({ billingMode: 'per_request' })

    expect(wrapper.get('[role="alert"]').text()).toContain('non-negative')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('does not emit an interval while a custom price is empty', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: null, input_price: 0.000005 }],
      },
    })

    await wrapper.get('[data-price-field="input_price"] [data-testid="custom-price-input"]').setValue('')

    expect(wrapper.findAll('[role="alert"]').some(alert => alert.text().includes('required'))).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
