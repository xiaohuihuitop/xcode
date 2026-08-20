import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppLayout custom background', () => {
  it('keeps the custom stone surface, mesh opacity, and top accent layer', () => {
    expect(componentSource).toContain('min-h-screen bg-stone-100 dark:bg-dark-950')
    expect(componentSource).toContain('bg-mesh-gradient opacity-90 dark:opacity-70')
    expect(componentSource).toContain('from-primary-100/60 via-primary-50/25 to-transparent')
  })
})
