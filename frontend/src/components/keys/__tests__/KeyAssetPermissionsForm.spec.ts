import { mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import KeyAssetPermissionsForm from '../KeyAssetPermissionsForm.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('KeyAssetPermissionsForm', () => {
  it('shows concrete platforms and two billing switches with create defaults', async () => {
    const wrapper = mount(KeyAssetPermissionsForm, {
      props: {
        platforms: [
          { id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' },
          { id: 12, code: 'grok-primary', name: 'Grok Primary', account_platform: 'grok' },
        ],
        platformIds: [11, 12],
        allowAllSubscriptions: true,
        allowBalance: true,
      },
    })

    expect(wrapper.find('[data-test="key-plan-21"]').exists()).toBe(false)
    expect((wrapper.get('[data-test="key-platform-11"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="key-platform-12"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="key-all-subscriptions"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="key-balance"]').element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('[data-test="key-all-subscriptions"]').setValue(false)
    await wrapper.get('[data-test="key-balance"]').setValue(false)

    expect(wrapper.emitted('update:allowAllSubscriptions')).toEqual([[false]])
    expect(wrapper.emitted('update:allowBalance')).toEqual([[false]])
  })

  it('keeps platform selection when wired through the parent form', async () => {
    const parent = defineComponent({
      components: { KeyAssetPermissionsForm },
      setup() {
        const state = reactive({ platformIds: [] as number[], allowAllSubscriptions: true, allowBalance: true })
        return { state }
      },
      template: `
        <KeyAssetPermissionsForm
          :platforms="[
            { id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' },
          ]"
          :platform-ids="state.platformIds"
          :allow-all-subscriptions="state.allowAllSubscriptions"
          :allow-balance="state.allowBalance"
          @update:platform-ids="state.platformIds = $event"
          @update:allow-all-subscriptions="state.allowAllSubscriptions = $event"
          @update:allow-balance="state.allowBalance = $event"
        />
      `,
    })
    const wrapper = mount(parent)

    await wrapper.get('[data-test="key-platform-11"]').setValue(true)

    expect(wrapper.vm.state.platformIds).toEqual([11])
  })
})
