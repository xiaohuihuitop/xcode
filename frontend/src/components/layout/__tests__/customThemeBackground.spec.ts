import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const frontendRoot = resolve(__dirname, '../../../..')
const themeSource = readFileSync(resolve(frontendRoot, 'tailwind.config.js'), 'utf8')
const styleSource = readFileSync(resolve(frontendRoot, 'src/style.css'), 'utf8')

describe('custom warm theme background', () => {
  it('keeps the warm palette and dark-mode background gradient', () => {
    expect(themeSource).toContain("400: '#f59e0b'")
    expect(themeSource).toContain("500: '#d97706'")
    expect(themeSource).toContain("950: '#0c0a09'")
    expect(themeSource).toContain('rgba(245, 158, 11, 0.14)')
    expect(styleSource).toContain('@apply bg-stone-100 text-gray-900 dark:bg-dark-950 dark:text-gray-100;')
    expect(styleSource).toContain('.dark body {')
    expect(styleSource).toContain('rgba(28, 25, 23, 0.98)')
  })
})
