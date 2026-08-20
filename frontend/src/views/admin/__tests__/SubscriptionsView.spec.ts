import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../SubscriptionsView.vue')
const source = readFileSync(componentPath, 'utf8')

describe('SubscriptionsView billing redesign wiring', () => {
  it('assigns a concrete subscription plan instead of a legacy group and day count', () => {
    expect(source).toContain('v-model="assignForm.plan_id"')
    expect(source).toContain('plan_id: assignForm.plan_id')
    expect(source).not.toContain('v-model="assignForm.group_id"')
    expect(source).not.toContain('validity_days: assignForm.validity_days')
  })

  it('renders usage from the immutable subscription terms helper', () => {
    expect(source).toContain("getSubscriptionLimit(row, 'daily')")
    expect(source).toContain("getSubscriptionLimit(row, 'weekly')")
    expect(source).toContain("getSubscriptionLimit(row, 'monthly')")
  })

  it('does not load or expose legacy groups for plan-backed subscriptions', () => {
    expect(source).not.toContain('v-model="filters.group_id"')
    expect(source).not.toContain('GroupBadge')
    expect(source).not.toContain('loadGroups()')
    expect(source).not.toContain('group_id: filters.group_id')
    expect(source).toContain("key: 'plan'")
  })
})
