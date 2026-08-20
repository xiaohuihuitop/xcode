import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../DashboardView.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('Dashboard subscription summary', () => {
  it('loads active subscriptions and passes them to the stats card grid', () => {
    expect(componentSource).toContain("import { getActiveSubscriptions } from '@/api/subscriptions'")
    expect(componentSource).toContain('const loadSubscriptions = async () =>')
    expect(componentSource).toContain('subscriptions.value = await getActiveSubscriptions()')
    expect(componentSource).toContain(':subscriptions="subscriptions"')
    expect(componentSource).toContain('loadSubscriptions()')
  })
})
