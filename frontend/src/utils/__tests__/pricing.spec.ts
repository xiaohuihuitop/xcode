import { describe, expect, it } from 'vitest'

import { fromPerMillionTokens, toPerMillionTokens } from '../pricing'

describe('pricing unit conversions', () => {
  it('converts per-token prices to per-million-token prices', () => {
    expect(toPerMillionTokens(0.000005)).toBe(5)
  })

  it('converts per-million-token prices to per-token prices', () => {
    expect(fromPerMillionTokens(5)).toBeCloseTo(0.000005, 12)
  })

  it('preserves explicit zero when converting from per-million-token prices', () => {
    expect(fromPerMillionTokens(0)).toBe(0)
  })

  it('preserves inherited null when converting from per-million-token prices', () => {
    expect(fromPerMillionTokens(null)).toBeNull()
  })
})
