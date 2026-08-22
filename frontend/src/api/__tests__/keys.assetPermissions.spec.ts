import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post },
}))

import { create, getAvailablePlatforms } from '@/api/keys'

describe('API Key asset permissions API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    post.mockResolvedValue({ data: { id: 7 } })
  })

  it('creates a key with platform, all-subscriptions, and balance permissions', async () => {
    await create({
      name: 'multi-asset-key',
      platform_ids: [11, 12],
      allow_all_subscriptions: true,
      allow_balance: true,
    })

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'multi-asset-key',
      platform_ids: [11, 12],
      allow_all_subscriptions: true,
      allow_balance: true,
    })
  })

  it('loads only the safe metadata needed by the platform selector', async () => {
    get.mockResolvedValue({
      data: [{ id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' }],
    })

    await expect(getAvailablePlatforms()).resolves.toEqual([
      { id: 11, code: 'openai-primary', name: 'OpenAI Primary', account_platform: 'openai' },
    ])
    expect(get).toHaveBeenCalledWith('/platforms/available')
  })
})
