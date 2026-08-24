import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import ModelPlazaContent from '../ModelPlazaContent.vue'

const tokenPricing = (inputPrice: number) => ({
  billing_mode: 'token',
  input_price: inputPrice,
  output_price: null,
  cache_write_price: null,
  cache_read_price: null,
  image_input_price: null,
  image_output_price: null,
  per_request_price: null,
  intervals: [],
})

describe('ModelPlazaContent', () => {
  it('filters by platform code so codex and glm remain separate when they share an adapter', async () => {
    const wrapper = mount(ModelPlazaContent, {
      props: {
        loading: false,
        response: {
          description: '',
          platforms: [
            {
              id: 1,
              code: 'codex',
              name: 'Codex',
              account_platform: 'openai',
              endpoint_capabilities: [],
              models: [{
                pattern: 'gpt-5.6-sol',
                endpoint_capabilities: [],
                pricing: tokenPricing(0.000002),
                official_pricing: tokenPricing(0.000001),
                sale_pricing: tokenPricing(0.000002),
                sale_pricing_source: 'custom' as const,
              }],
            },
            {
              id: 2,
              code: 'glm',
              name: 'GLM',
              account_platform: 'openai',
              endpoint_capabilities: [],
              models: [{ pattern: 'glm-5.2', endpoint_capabilities: [], pricing: tokenPricing(0.000003) }],
            },
          ],
        },
      },
      global: {
        stubs: {
          PlatformSection: {
            template: '<div class="platform-section">{{ platform.code }}:{{ platform.models[0].sale_pricing?.input_price ?? platform.models[0].pricing?.input_price }}</div>',
            props: ['platform'],
          },
        },
        mocks: { $t: (key: string) => key },
      },
    })

    const buttons = wrapper.findAll('button')
    const glmButton = buttons.find((button) => button.text() === 'glm')
    expect(wrapper.findAll('.platform-section').map((item) => item.text())).toEqual(['codex:0.000002', 'glm:0.000003'])
    expect(glmButton).toBeDefined()
    await glmButton!.trigger('click')

    expect(wrapper.findAll('.platform-section').map((item) => item.text())).toEqual(['glm:0.000003'])
  })
})
