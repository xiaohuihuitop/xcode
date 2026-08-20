import { apiClient } from './client'

export interface PlatformPlazaPricing {
  billing_mode: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: Array<{
    min_tokens: number
    max_tokens: number | null
    tier_label?: string
    input_price: number | null
    output_price: number | null
    cache_write_price: number | null
    cache_read_price: number | null
    per_request_price: number | null
  }>
}

export interface PlatformPlazaModel {
  pattern: string
  upstream_model?: string
  endpoint_capabilities: string[]
  pricing: PlatformPlazaPricing | null
}

export interface ModelPlazaPlatform {
  id: number
  code: string
  name: string
  account_platform: string
  endpoint_capabilities: string[]
  models: PlatformPlazaModel[]
}

export interface ModelPlazaResponse {
  description: string
  platforms: ModelPlazaPlatform[]
}

export async function getModelPlaza(options?: { signal?: AbortSignal }): Promise<ModelPlazaResponse> {
  const { data } = await apiClient.get<ModelPlazaResponse>('/model-plaza', {
    signal: options?.signal
  })
  return data
}

export const modelPlazaAPI = { getModelPlaza }

export default modelPlazaAPI
