import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import PricingIntervalsEditor from '../PricingIntervalsEditor.vue'
import PricingValueEditor from '../PricingValueEditor.vue'
import PricingIntervalsEditorParentHarness from './PricingIntervalsEditorParentHarness.vue'

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

  it('uses fieldset groups without labels wrapping interactive price editors', () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: { billingMode: 'token', modelValue: [{ min_tokens: 0, max_tokens: null }] },
    })

    const priceGroups = wrapper.findAll('[data-price-field]')
    expect(priceGroups).toHaveLength(4)
    for (const group of priceGroups) {
      expect(group.element.tagName).toBe('FIELDSET')
      expect(group.get('legend').text()).not.toBe('')
    }
    expect(wrapper.find('label [data-mode]').exists()).toBe(false)
  })

  it('uses per-request pricing for image intervals and emits edited values', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: null }],
        labels: { imageUnit: 'USD / image', requestUnit: 'USD / request' },
      },
    })

    await wrapper.setProps({ billingMode: 'image' })

    expect(wrapper.find('[data-price-field="input_price"]').exists()).toBe(false)
    expect(wrapper.find('[data-price-field="output_price"]').exists()).toBe(false)
    expect(wrapper.find('[data-price-field="cache_write_price"]').exists()).toBe(false)
    expect(wrapper.find('[data-price-field="cache_read_price"]').exists()).toBe(false)
    expect(wrapper.find('[data-price-field="per_request_price"]').exists()).toBe(true)
    await wrapper.get('[data-price-field="per_request_price"] [data-mode="custom"]').trigger('click')
    expect(wrapper.get('[data-price-field="per_request_price"]').text()).toContain('USD / image')
    expect(wrapper.get('[data-price-field="per_request_price"]').text()).not.toContain('USD / request')
    await wrapper.get('[data-price-field="per_request_price"] [data-testid="custom-price-input"]').setValue('2')

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted?.[emitted.length - 1][0]).toEqual([
      expect.objectContaining({ per_request_price: 2, sort_order: 0 }),
    ])
  })

  it('uses request units for per-request intervals without image units', () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'per_request',
        modelValue: [{ min_tokens: 0, max_tokens: null, per_request_price: 0.02 }],
        labels: { imageUnit: 'USD / image', requestUnit: 'USD / request' },
      },
    })

    const field = wrapper.get('[data-price-field="per_request_price"]')
    expect(field.text()).toContain('USD / request')
    expect(field.text()).not.toContain('USD / image')
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
    expect(wrapper.emitted('update:valid')).toEqual([[false]])
  })

  it('lets a parent v-model disable saving while a draft is invalid and recover when fixed', async () => {
    const initialValue = [{ min_tokens: 0, max_tokens: 100, tier_label: 'Initial' }]
    const wrapper = mount(PricingIntervalsEditorParentHarness, {
      props: { initialValue },
    })
    await nextTick()

    const editor = wrapper.getComponent(PricingIntervalsEditor)
    const saveButton = wrapper.get<HTMLButtonElement>('[data-testid="save-button"]')

    expect(wrapper.get('[data-testid="valid-status"]').text()).toBe('true')
    expect(saveButton.element.disabled).toBe(false)
    expect(editor.emitted('update:valid')).toEqual([[true]])

    await wrapper.get('[data-testid="min-tokens-0"]').setValue('-1')

    expect(wrapper.get('[data-testid="valid-status"]').text()).toBe('false')
    expect(saveButton.element.disabled).toBe(true)
    expect(JSON.parse(wrapper.get('[data-testid="parent-model"]').text())).toEqual(initialValue)
    await saveButton.trigger('click')
    expect(wrapper.get('[data-testid="save-count"]').text()).toBe('0')

    await wrapper.get('[data-testid="min-tokens-0"]').setValue('0')

    expect(wrapper.get('[data-testid="valid-status"]').text()).toBe('true')
    expect(saveButton.element.disabled).toBe(false)
    expect(editor.emitted('update:valid')).toEqual([[true], [false], [true]])
    await saveButton.trigger('click')
    expect(wrapper.get('[data-testid="save-count"]').text()).toBe('1')
  })

  it('disables parent saving for an initially invalid model', async () => {
    const wrapper = mount(PricingIntervalsEditorParentHarness, {
      props: {
        initialValue: [
          { min_tokens: 0, max_tokens: 100 },
          { min_tokens: 50, max_tokens: null },
        ],
      },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="valid-status"]').text()).toBe('false')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="save-button"]').element.disabled).toBe(true)
    expect(wrapper.getComponent(PricingIntervalsEditor).emitted('update:valid')).toEqual([[false]])
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

  it('reports validity once per state while a negative child price is corrected', async () => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'token',
        modelValue: [{ min_tokens: 0, max_tokens: null, input_price: -0.000005 }],
      },
    })
    const priceEditor = wrapper.getComponent(PricingValueEditor)

    expect(priceEditor.emitted('validity-change')).toEqual([[false]])
    expect(wrapper.emitted('update:valid')).toEqual([[false]])

    await priceEditor.get('[data-testid="custom-price-input"]').setValue('5')

    expect(priceEditor.emitted('update:modelValue')).toEqual([[0.000005]])
    expect(priceEditor.emitted('validity-change')?.slice(-1)).toEqual([[true]])
    expect(wrapper.emitted('update:valid')).toEqual([[false], [true]])
    expect(wrapper.emitted('update:modelValue')).toEqual([[
      [expect.objectContaining({ input_price: 0.000005, sort_order: 0 })],
    ]])
  })

  it.each([
    'input_price',
    'output_price',
    'cache_write_price',
    'cache_read_price',
    'per_request_price',
  ] as const)('rejects an initial negative %s even when the field is hidden', async field => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: field === 'per_request_price' ? 'token' : 'per_request',
        modelValue: [{ min_tokens: 0, max_tokens: null, [field]: -1 }],
        labels: { invalidPrice: 'Localized invalid interval price.' },
      },
    })

    await wrapper.get('[data-testid="tier-label-0"]').setValue('Edited')

    expect(wrapper.get('[role="alert"]').text()).toContain('Localized invalid interval price.')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it.each([Number.NaN, Number.POSITIVE_INFINITY])('rejects a non-finite interval price: %s', async value => {
    const wrapper = mount(PricingIntervalsEditor, {
      props: {
        billingMode: 'per_request',
        modelValue: [{ min_tokens: 0, max_tokens: null, cache_read_price: value }],
      },
    })

    await wrapper.get('[data-testid="tier-label-0"]').setValue('Edited')

    expect(wrapper.get('[role="alert"]').text()).toContain('invalid')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
