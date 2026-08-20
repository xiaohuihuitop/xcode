import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import ConfirmDialog from '../ConfirmDialog.vue'

describe('ConfirmDialog', () => {
  it('does not emit confirmation when confirmation is disabled', async () => {
    const wrapper = mount(ConfirmDialog, {
      props: {
        show: true,
        title: 'Delete platform',
        message: 'Blocked',
        confirmDisabled: true,
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        },
      },
    })

    const buttons = wrapper.findAll('button')
    const confirmButton = buttons[1]
    expect(confirmButton.element.disabled).toBe(true)
    await confirmButton.trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })
})
