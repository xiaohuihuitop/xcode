import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import ModelPlazaContent from '../ModelPlazaContent.vue'

describe('ModelPlazaContent', () => {
  it('filters by platform code so codex and glm remain separate when they share an adapter', async () => {
    const wrapper = mount(ModelPlazaContent, {
      props: {
        loading: false,
        response: {
          description: '',
          platforms: [
            { id: 1, code: 'codex', name: 'Codex', account_platform: 'openai', endpoint_capabilities: [], models: [{ pattern: 'gpt-5.6-sol', endpoint_capabilities: [], pricing: null }] },
            { id: 2, code: 'glm', name: 'GLM', account_platform: 'openai', endpoint_capabilities: [], models: [{ pattern: 'glm-5.2', endpoint_capabilities: [], pricing: null }] },
          ],
        },
      },
      global: {
        stubs: {
          PlatformSection: { template: '<div class="platform-section">{{ platform.code }}</div>', props: ['platform'] },
        },
        mocks: { $t: (key: string) => key },
      },
    })

    const buttons = wrapper.findAll('button')
    const glmButton = buttons.find((button) => button.text() === 'glm')
    expect(glmButton).toBeDefined()
    await glmButton!.trigger('click')

    expect(wrapper.findAll('.platform-section').map((item) => item.text())).toEqual(['glm'])
  })
})
