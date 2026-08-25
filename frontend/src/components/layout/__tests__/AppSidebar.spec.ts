import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar subscription navigation', () => {
  it('keeps subscriptions out of the user navigation while retaining the admin entry', () => {
    const selfNavSource = componentSource.match(/function buildSelfNavItems[\s\S]*?\n}\n\n\/\/ finalizeNav/)?.[0]

    expect(selfNavSource).toBeDefined()
    expect(selfNavSource).not.toContain("path: '/subscriptions'")
    expect(componentSource).toContain("path: '/admin/subscriptions'")
  })
})

describe('AppSidebar available-platform navigation', () => {
  it('keeps the available-platform page out of regular and admin personal navigation', () => {
    const selfNavSource = componentSource.match(/function buildSelfNavItems[\s\S]*?\n}\n\n\/\/ finalizeNav/)?.[0]

    expect(selfNavSource).toBeDefined()
    expect(selfNavSource).not.toContain("path: '/available-platforms'")
    expect(selfNavSource).not.toContain("t('nav.availablePlatforms')")
  })
})

describe('AppSidebar platform asset navigation', () => {
  it('keeps the legacy groups editor out of the default admin navigation', () => {
    const start = componentSource.indexOf('const adminNavItems')
    const end = componentSource.indexOf('function toggleSidebar', start)
    const adminNavSource = start >= 0 && end >= 0 ? componentSource.slice(start, end) : undefined

    expect(adminNavSource).toBeDefined()
    expect(adminNavSource).not.toContain("path: '/admin/groups'")
    expect(adminNavSource).toContain("path: '/admin/platforms'")
    expect(adminNavSource).toContain("path: '/admin/subscriptions'")
  })

  it('keeps subscription plan management available when checkout is disabled', () => {
    const start = componentSource.indexOf('const adminNavItems')
    const end = componentSource.indexOf('function toggleSidebar', start)
    const adminNavSource = start >= 0 && end >= 0 ? componentSource.slice(start, end) : undefined

    expect(adminNavSource).toContain(
      "{ path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: CreditCardIcon, hideInSimpleMode: true },"
    )
    expect(adminNavSource?.indexOf("path: '/admin/orders/plans'")).toBeLessThan(
      adminNavSource?.indexOf("path: '/admin/orders',") ?? -1
    )
  })
})
