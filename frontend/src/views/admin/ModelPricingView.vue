<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.modelPricing.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.description') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <button class="icon-button" type="button" :disabled="loading" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="load">
            <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />
          </button>
          <button class="btn btn-primary gap-1.5" type="button" @click="openCreate">
            <Icon name="plus" size="sm" />
            <span>{{ t('admin.modelPricing.create') }}</span>
          </button>
        </div>
      </div>

      <div v-if="loadError" class="flex items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
        <span>{{ t('admin.modelPricing.loadFailed') }}</span>
        <button class="btn btn-secondary" type="button" @click="load">{{ t('common.refresh') }}</button>
      </div>

      <DataTable :columns="columns" :data="items" :loading="loading" row-key="id">
        <template #cell-adapter="{ value }">
          <span class="font-mono text-sm text-gray-700 dark:text-gray-200">{{ value }}</span>
        </template>
        <template #cell-model_pattern="{ value }">
          <span class="font-mono text-sm text-gray-700 dark:text-gray-200">{{ value }}</span>
        </template>
        <template #cell-billing_mode="{ value }">
          <span class="rounded border border-primary-200 bg-primary-50 px-1.5 py-0.5 text-xs text-primary-700 dark:border-primary-800 dark:bg-primary-950/40 dark:text-primary-300">{{ modeLabel(value) }}</span>
        </template>
        <template #cell-input_price="{ row }">
          {{ formatPrice(row.input_price) }}
        </template>
        <template #cell-output_price="{ row }">
          {{ formatPrice(row.output_price) }}
        </template>
        <template #cell-status="{ row }">
          <span :class="row.status === 'active' ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'">{{ row.status === 'active' ? t('admin.modelPricing.active') : t('admin.modelPricing.disabled') }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-1">
            <button class="icon-button" type="button" :title="t('common.edit')" :aria-label="t('common.edit')" @click="openEdit(row)">
              <Icon name="edit" size="sm" />
            </button>
            <button class="icon-button text-red-600 dark:text-red-400" type="button" :title="t('common.delete')" :aria-label="t('common.delete')" @click="remove(row)">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </template>
      </DataTable>

      <div v-if="!loading && items.length === 0" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.noData') }}</div>
    </div>

    <div v-if="editorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true">
      <div class="w-full max-w-3xl rounded-lg border border-gray-200 bg-white p-5 shadow-xl dark:border-dark-600 dark:bg-dark-800">
        <div class="flex items-center justify-between gap-3">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ editing ? t('admin.modelPricing.edit') : t('admin.modelPricing.create') }}</h2>
          <button class="icon-button" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="editorOpen = false"><Icon name="x" size="sm" /></button>
        </div>
        <div class="mt-4 grid gap-4 sm:grid-cols-2">
          <label class="space-y-1 text-sm"><span>{{ t('admin.modelPricing.adapter') }}</span><input v-model="draft.adapter" class="input" maxlength="50" /></label>
          <label class="space-y-1 text-sm"><span>{{ t('admin.modelPricing.modelPattern') }}</span><input v-model="draft.model_pattern" class="input font-mono" maxlength="100" /></label>
          <label class="space-y-1 text-sm"><span>{{ t('admin.modelPricing.billingMode') }}</span><select v-model="draft.billing_mode" class="input"><option value="token">{{ t('admin.modelPricing.token') }}</option><option value="per_request">{{ t('admin.modelPricing.perRequest') }}</option><option value="image">{{ t('admin.modelPricing.image') }}</option></select></label>
          <label class="space-y-1 text-sm"><span>{{ t('admin.modelPricing.status') }}</span><select v-model="draft.status" class="input"><option value="active">{{ t('admin.modelPricing.active') }}</option><option value="disabled">{{ t('admin.modelPricing.disabled') }}</option></select></label>
          <label v-for="field in priceFields" :key="field.key" class="space-y-1 text-sm"><span>{{ t(field.label) }}</span><input v-model="draft[field.key]" class="input font-mono" type="number" min="0" step="any" /></label>
        </div>
        <label class="mt-4 block space-y-1 text-sm"><span>Intervals JSON</span><textarea v-model="intervalsText" class="input min-h-24 font-mono text-xs" spellcheck="false" placeholder="[]" /></label>
        <p v-if="formError" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
        <div class="mt-5 flex justify-end gap-2"><button class="btn btn-secondary" type="button" @click="editorOpen = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="button" :disabled="saving" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button></div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { adminAPI } from '@/api/admin'
import type { ModelPricingOverride, ModelPricingOverrideInput } from '@/api/admin/modelPricing'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const items = ref<ModelPricingOverride[]>([])
const loading = ref(false)
const saving = ref(false)
const loadError = ref(false)
const editorOpen = ref(false)
const editing = ref<ModelPricingOverride | null>(null)
const formError = ref('')
const intervalsText = ref('[]')

type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price' | 'image_input_price' | 'image_output_price' | 'per_request_price'
const priceFields: Array<{ key: PriceKey; label: string }> = [
  { key: 'input_price', label: 'admin.modelPricing.inputPrice' },
  { key: 'output_price', label: 'admin.modelPricing.outputPrice' },
  { key: 'cache_write_price', label: 'admin.modelPricing.cacheWritePrice' },
  { key: 'cache_read_price', label: 'admin.modelPricing.cacheReadPrice' },
  { key: 'image_input_price', label: 'admin.modelPricing.imageInputPrice' },
  { key: 'image_output_price', label: 'admin.modelPricing.imageOutputPrice' },
  { key: 'per_request_price', label: 'admin.modelPricing.perRequestPrice' },
]

const columns = computed<Column[]>(() => [
  { key: 'adapter', label: t('admin.modelPricing.adapter') },
  { key: 'model_pattern', label: t('admin.modelPricing.modelPattern') },
  { key: 'billing_mode', label: t('admin.modelPricing.billingMode') },
  { key: 'input_price', label: t('admin.modelPricing.inputPrice') },
  { key: 'output_price', label: t('admin.modelPricing.outputPrice') },
  { key: 'status', label: t('admin.modelPricing.status') },
  { key: 'actions', label: t('common.actions') },
])

const draft = reactive<ModelPricingOverrideInput>({
  adapter: '', model_pattern: '', billing_mode: 'token', status: 'active', intervals: [],
})

async function load() {
  loading.value = true
  loadError.value = false
  try {
    items.value = await adminAPI.modelPricing.list()
  } catch (error) {
    items.value = []
    loadError.value = true
    appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.loadFailed')))
  } finally {
    loading.value = false
  }
}

function resetDraft(item?: ModelPricingOverride) {
  Object.assign(draft, { adapter: item?.adapter ?? '', model_pattern: item?.model_pattern ?? '', billing_mode: item?.billing_mode ?? 'token', status: item?.status ?? 'active' })
  for (const field of priceFields) draft[field.key] = item?.[field.key] ?? null
  intervalsText.value = JSON.stringify(item?.intervals ?? [], null, 2)
  formError.value = ''
}

function openCreate() { editing.value = null; resetDraft(); editorOpen.value = true }
function openEdit(item: ModelPricingOverride) { editing.value = item; resetDraft(item); editorOpen.value = true }

function numericValue(value: number | null | undefined): number | null | undefined {
  if (value === null || value === undefined || Number.isNaN(value)) return null
  return Number(value)
}

async function save() {
  if (!draft.adapter.trim() || !draft.model_pattern.trim()) { formError.value = t('admin.modelPricing.required'); return }
  let intervals
  try { intervals = JSON.parse(intervalsText.value || '[]') } catch { formError.value = 'Intervals JSON is invalid'; return }
  if (!Array.isArray(intervals)) { formError.value = 'Intervals must be an array'; return }
  const payload: ModelPricingOverrideInput = { ...draft, adapter: draft.adapter.trim(), model_pattern: draft.model_pattern.trim(), intervals }
  for (const field of priceFields) payload[field.key] = numericValue(draft[field.key])
  saving.value = true
  try {
    if (editing.value) await adminAPI.modelPricing.update(editing.value.id, payload)
    else await adminAPI.modelPricing.create(payload)
    appStore.showSuccess(t('admin.modelPricing.saved'))
    editorOpen.value = false
    await load()
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.saveFailed'))) }
  finally { saving.value = false }
}

async function remove(item: ModelPricingOverride) {
  if (!window.confirm(t('admin.modelPricing.confirmDelete'))) return
  try { await adminAPI.modelPricing.remove(item.id); appStore.showSuccess(t('admin.modelPricing.deleted')); await load() }
  catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.saveFailed'))) }
}

function formatPrice(value: number | null | undefined) { return value == null ? '-' : value.toExponential(4) }
function modeLabel(value: string) { return value === 'per_request' ? t('admin.modelPricing.perRequest') : value === 'image' ? t('admin.modelPricing.image') : t('admin.modelPricing.token') }

onMounted(load)
</script>
