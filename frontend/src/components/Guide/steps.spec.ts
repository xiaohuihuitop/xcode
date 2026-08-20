import { describe, expect, it } from 'vitest'

import { getAdminSteps, getUserSteps } from './steps'

const t = (key: string) => key

function selectors(steps: ReturnType<typeof getAdminSteps>): string[] {
  return steps
    .map(step => step.element)
    .filter((element): element is string => typeof element === 'string')
}

describe('onboarding steps', () => {
  it('guides administrators through platform-owned routing without legacy groups', () => {
    const elements = selectors(getAdminSteps(t))

    expect(elements).toContain('[data-tour="sidebar-platforms"]')
    expect(elements).toContain('[data-tour="platforms-create-btn"]')
    expect(elements).toContain('[data-tour="platform-form-models"]')
    expect(elements).toContain('[data-tour="account-form-platform-pool"]')
    expect(elements).toContain('[data-tour="key-form-platforms"]')
    expect(elements).toContain('[data-tour="key-form-subscription-plans"]')
    expect(elements.every(element => !element.includes('group'))).toBe(true)
  })

  it('guides users through explicit platform and billing-asset authorization', () => {
    const elements = selectors(getUserSteps(t))

    expect(elements).toContain('[data-tour="key-form-platforms"]')
    expect(elements).toContain('[data-tour="key-form-subscription-plans"]')
    expect(elements.every(element => !element.includes('group'))).toBe(true)
  })
})
