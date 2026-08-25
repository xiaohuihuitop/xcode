import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { catalog } from '@/api/admin/modelPricing'

describe('admin model pricing API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('normalizes a missing official billing mode from legacy catalog responses', async () => {
    get.mockResolvedValue({
      data: [{
        platform_id: 7,
        platform_code: 'codex',
        platform_name: 'Codex Platform',
        account_platform: 'openai',
        model_pattern: 'legacy-model',
        upstream_model: 'legacy-model',
        billing_mode: 'token',
        official_pricing: null,
        official_source: {
          source_type: 'unavailable',
          source_name: '',
          matched_model: '',
        },
        sale_pricing: null,
        sale_source: 'unavailable',
        override: null,
        intervals: [],
      }],
    })

    const rows = await catalog()

    expect(rows[0].official_billing_mode).toBe('')
  })
})
