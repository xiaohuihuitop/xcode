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

export interface ModelPricingValues {
  input_price?: number | null
  output_price?: number | null
  cache_write_price?: number | null
  cache_read_price?: number | null
  image_input_price?: number | null
  image_output_price?: number | null
  per_request_price?: number | null
  intervals: ModelPricingInterval[]
}

export type ModelPricingOfficialSourceType =
  | ''
  | 'remote_catalog'
  | 'cached_remote_catalog'
  | 'bundled_catalog'
  | 'code_fallback'
  | 'unavailable'

export interface ModelPricingOfficialSource {
  source_type: ModelPricingOfficialSourceType
  source_name: string
  source_url?: string
  matched_model: string
  updated_at?: string | null
}

export type ModelPricingSaleSource = 'official' | 'custom' | 'unavailable'

export interface ModelPricingCatalogRow {
  platform_id: number
  platform_code: string
  platform_name: string
  account_platform: string
  model_pattern: string
  upstream_model: string
  billing_mode: ModelPricingBillingMode
  official_billing_mode: ModelPricingBillingMode | ''
  official_pricing: ModelPricingValues | null
  official_source: ModelPricingOfficialSource
  sale_pricing: ModelPricingValues | null
  sale_source: ModelPricingSaleSource
  override: ModelPricingOverride | null
  intervals: ModelPricingInterval[]
}

export interface ModelPricingCatalogWireRow extends Omit<ModelPricingCatalogRow, 'official_billing_mode'> {
  official_billing_mode?: ModelPricingBillingMode | ''
}

export interface ModelPricingCatalogQuery {
  platform_id?: number
  query?: string
}

export interface PlatformSalePricingInput extends ModelPricingValues {
  platform_id: number
  model_pattern: string
  billing_mode: ModelPricingBillingMode
  status: ModelPricingOverride['status']
}

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

export async function catalog(query: ModelPricingCatalogQuery = {}): Promise<ModelPricingCatalogRow[]> {
  const params: Record<string, string | number> = {}
  if (query.platform_id != null) params.platform_id = query.platform_id
  const model = query.query?.trim()
  if (model) params.model = model
  const { data } = await apiClient.get<ModelPricingCatalogWireRow[]>('/admin/model-pricing/catalog', {
    params: Object.keys(params).length ? params : undefined,
  })
  return data.map(row => ({
    ...row,
    official_billing_mode: row.official_billing_mode ?? '',
  }))
}

export async function upsertPlatformSale(input: PlatformSalePricingInput): Promise<ModelPricingOverride> {
  const { data } = await apiClient.put<ModelPricingOverride>('/admin/model-pricing/platform-sale', input)
  return data
}

const modelPricingAPI = { list, getById, create, update, remove, catalog, upsertPlatformSale }

export default modelPricingAPI
