import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { post }
}))

import { generate } from '@/api/admin/redeem'

describe('admin redeem API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: [] })
  })

  it('uses a subscription plan instead of legacy group terms', async () => {
    await generate(3, 'subscription', 0, 17, 7)

    expect(post).toHaveBeenCalledWith('/admin/redeem-codes/generate', {
      count: 3,
      type: 'subscription',
      value: 0,
      subscription_plan_id: 17,
      expires_in_days: 7
    })
  })
})
