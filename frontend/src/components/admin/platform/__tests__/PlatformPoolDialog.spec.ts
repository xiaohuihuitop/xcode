import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import PlatformPoolDialog from '../PlatformPoolDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: { modelValue: { type: Array, required: true } },
  emits: ['update:modelValue'],
  template: `
    <div>
      <button type="button" data-test="whitelist-gpt-5.6" @click="$emit('update:modelValue', ['gpt-5.6'])">gpt-5.6</button>
      <button type="button" data-test="whitelist-gpt-5.5" @click="$emit('update:modelValue', [...modelValue, 'gpt-5.5'])">gpt-5.5</button>
    </div>
  `,
})

const mountDialog = (platform = null) => mount(PlatformPoolDialog, {
  props: {
    show: true,
    platform,
    submitting: false,
  },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>',
      },
      Icon: true,
      ModelWhitelistSelector: ModelWhitelistSelectorStub,
    },
  },
})

describe('PlatformPoolDialog', () => {
  it('emits platform endpoints, whitelist models, and mappings together', async () => {
    const wrapper = mountDialog()

    await wrapper.get('[data-test="platform-code"]').setValue('openai-primary')
    await wrapper.get('[data-test="platform-name"]').setValue('OpenAI Primary')
    await wrapper.get('[data-test="endpoint-chat_completions"]').setValue(true)
    await wrapper.get('[data-test="endpoint-responses"]').setValue(true)
    await wrapper.get('[data-test="whitelist-gpt-5.6"]').trigger('click')
    await wrapper.get('[data-test="whitelist-gpt-5.5"]').trigger('click')
    await wrapper.get('[data-test="add-model-mapping"]').trigger('click')
    await wrapper.get('[data-test="mapping-from-0"]').setValue('gpt-latest')
    await wrapper.get('[data-test="mapping-to-0"]').setValue('gpt-5.6')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('save')).toEqual([[
      {
        code: 'openai-primary',
        name: 'OpenAI Primary',
        account_platform: 'openai',
        status: 'active',
        endpoint_capabilities: ['chat_completions', 'responses'],
        model_rules: [
          { model_pattern: 'gpt-5.5', upstream_model: '', enabled: true },
          { model_pattern: 'gpt-5.6', upstream_model: '', enabled: true },
          { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
        ],
      },
    ]])
  })

  it('splits existing rules and keeps endpoint capabilities at platform level', () => {
    const wrapper = mountDialog({
      id: 12,
      code: 'grok-accounts',
      name: 'Grok Accounts',
      account_platform: 'grok',
      status: 'disabled',
      endpoint_capabilities: ['responses'],
      model_rules: [
        { id: 5, model_pattern: 'grok-4', upstream_model: 'grok-4', enabled: true },
        { id: 6, model_pattern: 'grok-latest', upstream_model: 'grok-4', enabled: true },
      ],
    })

    expect((wrapper.get('[data-test="platform-code"]').element as HTMLInputElement).value).toBe('grok-accounts')
    expect((wrapper.get('[data-test="endpoint-responses"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('[data-test="endpoint-responses-0"]').exists()).toBe(false)
    expect((wrapper.get('[data-test="mapping-from-0"]').element as HTMLInputElement).value).toBe('grok-latest')
    expect((wrapper.get('[data-test="mapping-to-0"]').element as HTMLInputElement).value).toBe('grok-4')
  })
})
