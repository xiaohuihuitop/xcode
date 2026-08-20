import { mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import KeyAssetPermissionsForm from '../KeyAssetPermissionsForm.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('KeyAssetPermissionsForm', () => {
  it('allows independent multi-selection and keeps balance enabled by default', async () => {
    const wrapper = mount(KeyAssetPermissionsForm, {
      props: {
        platforms: [
          { id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' },
          { id: 12, code: 'grok-primary', name: 'Grok Primary', account_platform: 'grok' },
        ],
        subscriptionPlans: [
          { id: 21, name: 'Pro' },
          { id: 22, name: 'Team' },
        ],
        platformIds: [],
        subscriptionPlanIds: [],
        allowBalance: true,
      },
    })

    expect((wrapper.get('[data-test="key-balance"]').element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('[data-test="key-platform-11"]').setValue(true)
    await wrapper.get('[data-test="key-plan-21"]').setValue(true)
    await wrapper.get('[data-test="key-balance"]').setValue(false)

    expect(wrapper.emitted('update:platformIds')).toEqual([[[11]]])
    expect(wrapper.emitted('update:subscriptionPlanIds')).toEqual([[[21]]])
    expect(wrapper.emitted('update:allowBalance')).toEqual([[false]])
  })

  it('keeps platform and subscription selections when wired through the parent form', async () => {
    const parent = defineComponent({
      components: { KeyAssetPermissionsForm },
      setup() {
        const state = reactive({ platformIds: [] as number[], subscriptionPlanIds: [] as number[] })
        return { state }
      },
      template: `
        <KeyAssetPermissionsForm
          :platforms="[
            { id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' },
          ]"
          :subscription-plans="[
            { id: 21, name: 'Pro' },
          ]"
          :platform-ids="state.platformIds"
          :subscription-plan-ids="state.subscriptionPlanIds"
          :allow-balance="true"
          @update:platform-ids="state.platformIds = $event"
          @update:subscription-plan-ids="state.subscriptionPlanIds = $event"
        />
      `,
    })
    const wrapper = mount(parent)

    await wrapper.get('[data-test="key-platform-11"]').setValue(true)
    await wrapper.get('[data-test="key-plan-21"]').setValue(true)

    expect(wrapper.vm.state.platformIds).toEqual([11])
    expect(wrapper.vm.state.subscriptionPlanIds).toEqual([21])
  })
})
