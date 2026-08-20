/**
 * Administrator platform account pool endpoints.
 * A platform pool owns account routing and model mapping, not billing.
 */

import { apiClient } from '../client'
import type {
  CreatePlatformPoolRequest,
  PlatformDeleteImpact,
  PlatformDeleteResult,
  PlatformPool,
  UpdatePlatformPoolRequest
} from '@/types'

export async function list(): Promise<PlatformPool[]> {
  const { data } = await apiClient.get<PlatformPool[]>('/admin/platforms')
  return data
}

export async function getById(id: number): Promise<PlatformPool> {
  const { data } = await apiClient.get<PlatformPool>(`/admin/platforms/${id}`)
  return data
}

export async function create(input: CreatePlatformPoolRequest): Promise<PlatformPool> {
  const { data } = await apiClient.post<PlatformPool>('/admin/platforms', input)
  return data
}

export async function update(id: number, input: UpdatePlatformPoolRequest): Promise<PlatformPool> {
  const { data } = await apiClient.put<PlatformPool>(`/admin/platforms/${id}`, input)
  return data
}

export async function previewDelete(id: number): Promise<PlatformDeleteImpact> {
  const { data } = await apiClient.get<PlatformDeleteImpact>(`/admin/platforms/${id}/delete-impact`)
  return data
}

export async function remove(id: number): Promise<PlatformDeleteResult> {
  const { data } = await apiClient.delete<PlatformDeleteResult>(`/admin/platforms/${id}`)
  return data
}

const platformsAPI = { list, getById, create, update, previewDelete, remove }

export default platformsAPI
