import { apiClient } from '../client'

export type ModelPricingBillingMode = 'token' | 'per_request' | 'image'

export interface ModelPricingInterval {
  min_tokens: number
  max_tokens?: number | null
  tier_label?: string
  input_price?: number | null
  output_price?: number | null
  cache_write_price?: number | null
  cache_read_price?: number | null
  per_request_price?: number | null
  sort_order?: number
}

export interface ModelPricingOverride {
  id: number
  adapter: string
  model_pattern: string
  billing_mode: ModelPricingBillingMode
  input_price?: number | null
  output_price?: number | null
  cache_write_price?: number | null
  cache_read_price?: number | null
  image_input_price?: number | null
  image_output_price?: number | null
  per_request_price?: number | null
  intervals: ModelPricingInterval[]
  status: 'active' | 'disabled'
}

export type ModelPricingOverrideInput = Omit<ModelPricingOverride, 'id'>

export async function list(adapter?: string): Promise<ModelPricingOverride[]> {
  const { data } = await apiClient.get<ModelPricingOverride[]>('/admin/model-pricing', {
    params: adapter ? { adapter } : undefined,
  })
  return data
}

export async function getById(id: number): Promise<ModelPricingOverride> {
  const { data } = await apiClient.get<ModelPricingOverride>(`/admin/model-pricing/${id}`)
  return data
}

export async function create(input: ModelPricingOverrideInput): Promise<ModelPricingOverride> {
  const { data } = await apiClient.post<ModelPricingOverride>('/admin/model-pricing', input)
  return data
}

export async function update(id: number, input: ModelPricingOverrideInput): Promise<ModelPricingOverride> {
  const { data } = await apiClient.put<ModelPricingOverride>(`/admin/model-pricing/${id}`, input)
  return data
}

export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/model-pricing/${id}`)
}

const modelPricingAPI = { list, getById, create, update, remove }

export default modelPricingAPI
