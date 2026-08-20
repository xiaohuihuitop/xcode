import { apiClient } from './client'
import type { AvailablePlatformPool } from '@/types'

/** Lists active platform pools and their enabled model patterns. */
export async function listAvailablePlatforms(): Promise<AvailablePlatformPool[]> {
  const { data } = await apiClient.get<AvailablePlatformPool[]>('/platforms/available')
  return data
}

export default { listAvailablePlatforms }
