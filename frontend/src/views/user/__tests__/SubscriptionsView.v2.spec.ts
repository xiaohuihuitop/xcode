import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../SubscriptionsView.vue')
const source = readFileSync(componentPath, 'utf8')

describe('SubscriptionsView V2 asset boundary', () => {
  it('starts another independent plan purchase without exposing legacy routing-group presentation', () => {
    expect(source).toContain('subscription.subscription_plan_id')
    expect(source).not.toContain('subscription.group?.platform')
    expect(source).not.toContain('subscription.group?.description')
  })
})
