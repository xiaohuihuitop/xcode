<template>
  <div class="min-w-0 space-y-3">
    <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
      <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ text.title }}</span>
      <div class="flex shrink-0 items-center gap-1">
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100"
          :title="text.sort"
          :aria-label="text.sort"
          :disabled="rows.length < 2"
          @click="normalizeRows"
        >
          <Icon name="arrowsUpDown" size="sm" />
        </button>
        <button type="button" class="btn btn-secondary h-9 rounded-md px-3 py-0" :aria-label="text.add" @click="addRow">
          <Icon name="plus" size="sm" />
          <span class="whitespace-nowrap">{{ text.add }}</span>
        </button>
      </div>
    </div>

    <p v-if="rows.length === 0" class="py-4 text-center text-sm text-gray-500 dark:text-gray-400">{{ text.empty }}</p>

    <div
      v-for="(row, index) in rows"
      :key="row.id"
      class="min-w-0 rounded-md border border-gray-200 p-3 dark:border-dark-600"
      data-testid="interval-row"
    >
      <div class="mb-3 flex min-w-0 items-center justify-between gap-2">
        <span class="truncate text-xs font-semibold uppercase text-gray-500 dark:text-gray-400" :title="`${text.interval} ${index + 1}`">
          {{ text.interval }} {{ index + 1 }}
        </span>
        <button
          type="button"
          class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-red-50 hover:text-red-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:text-gray-400 dark:hover:bg-red-950/30 dark:hover:text-red-300"
          :title="`${text.delete} ${index + 1}`"
          :aria-label="`${text.delete} ${index + 1}`"
          @click="deleteRow(index)"
        >
          <Icon name="trash" size="sm" />
        </button>
      </div>

      <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-3">
        <label class="min-w-0 text-sm">
          <span class="input-label break-words">{{ text.minTokens }}</span>
          <input
            :value="row.min_tokens"
            class="input min-w-0 rounded-md font-mono tabular-nums"
            :data-testid="`min-tokens-${index}`"
            type="number"
            min="0"
            step="1"
            inputmode="numeric"
            @input="updateBound(row, 'min_tokens', $event)"
          />
        </label>
        <label class="min-w-0 text-sm">
          <span class="input-label break-words">{{ text.maxTokens }}</span>
          <input
            :value="row.max_tokens"
            class="input min-w-0 rounded-md font-mono tabular-nums"
            :data-testid="`max-tokens-${index}`"
            type="number"
            min="0"
            step="1"
            inputmode="numeric"
            :placeholder="text.unbounded"
            @input="updateBound(row, 'max_tokens', $event)"
          />
        </label>
        <label class="min-w-0 text-sm">
          <span class="input-label break-words">{{ text.tierLabel }}</span>
          <input
            :value="row.tier_label"
            class="input min-w-0 rounded-md"
            :data-testid="`tier-label-${index}`"
            type="text"
            @input="updateTierLabel(row, $event)"
          />
        </label>
      </div>

      <div v-if="priceFields.length" class="mt-3 grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
        <fieldset v-for="field in priceFields" :key="field.key" class="m-0 min-w-0 border-0 p-0 text-sm" :data-price-field="field.key">
          <legend class="input-label break-words">{{ field.label }}</legend>
          <PricingValueEditor
            :model-value="row[field.key]"
            :scale="field.scale"
            :unit-label="field.unit"
            :labels="{ invalid: text.invalidPrice }"
            @update:model-value="updatePrice(row, field.key, $event)"
            @validity-change="setPriceValidity(row, field.key, $event)"
          />
        </fieldset>
      </div>
    </div>

    <p v-if="validationError" class="input-error-text break-words" role="alert">{{ validationError }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import type { ModelPricingBillingMode, ModelPricingInterval } from '@/api/admin/modelPricing'
import Icon from '@/components/icons/Icon.vue'
import { TOKEN_PRICE_SCALE } from '@/utils/pricing'
import PricingValueEditor from './PricingValueEditor.vue'

type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price' | 'per_request_price'
type BoundKey = 'min_tokens' | 'max_tokens'

interface DraftInterval {
  id: number
  min_tokens: string
  max_tokens: string
  tier_label: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  per_request_price: number | null
}

const props = withDefaults(defineProps<{
  modelValue: ModelPricingInterval[]
  billingMode: ModelPricingBillingMode
  valid?: boolean | null
  labels?: Partial<{
    title: string
    interval: string
    add: string
    delete: string
    sort: string
    empty: string
    minTokens: string
    maxTokens: string
    tierLabel: string
    unbounded: string
    inputPrice: string
    outputPrice: string
    cacheWritePrice: string
    cacheReadPrice: string
    perRequestPrice: string
    invalidPrice: string
  }>
}>(), {
  labels: () => ({}),
})

const emit = defineEmits<{
  'update:modelValue': [value: ModelPricingInterval[]]
  'update:valid': [valid: boolean]
}>()

const text = computed(() => ({
  title: props.labels.title ?? 'Pricing intervals',
  interval: props.labels.interval ?? 'Interval',
  add: props.labels.add ?? 'Add interval',
  delete: props.labels.delete ?? 'Delete interval',
  sort: props.labels.sort ?? 'Sort intervals',
  empty: props.labels.empty ?? 'No pricing intervals.',
  minTokens: props.labels.minTokens ?? 'Min tokens',
  maxTokens: props.labels.maxTokens ?? 'Max tokens',
  tierLabel: props.labels.tierLabel ?? 'Tier label',
  unbounded: props.labels.unbounded ?? 'Unbounded',
  inputPrice: props.labels.inputPrice ?? 'Input price',
  outputPrice: props.labels.outputPrice ?? 'Output price',
  cacheWritePrice: props.labels.cacheWritePrice ?? 'Cache write price',
  cacheReadPrice: props.labels.cacheReadPrice ?? 'Cache read price',
  perRequestPrice: props.labels.perRequestPrice ?? 'Per-request price',
  invalidPrice: props.labels.invalidPrice ?? 'Interval price is invalid; enter a finite, non-negative value.',
}))

const priceKeys: PriceKey[] = [
  'input_price',
  'output_price',
  'cache_write_price',
  'cache_read_price',
  'per_request_price',
]

const priceFields = computed<Array<{ key: PriceKey; label: string; scale: number; unit: string }>>(() => {
  if (props.billingMode === 'per_request' || props.billingMode === 'image') {
    return [{ key: 'per_request_price', label: text.value.perRequestPrice, scale: 1, unit: '$ / request' }]
  }
  return [
    { key: 'input_price', label: text.value.inputPrice, scale: TOKEN_PRICE_SCALE, unit: '$ / MTok' },
    { key: 'output_price', label: text.value.outputPrice, scale: TOKEN_PRICE_SCALE, unit: '$ / MTok' },
    { key: 'cache_write_price', label: text.value.cacheWritePrice, scale: TOKEN_PRICE_SCALE, unit: '$ / MTok' },
    { key: 'cache_read_price', label: text.value.cacheReadPrice, scale: TOKEN_PRICE_SCALE, unit: '$ / MTok' },
  ]
})

let nextRowId = 1
let lastEmittedSignature = ''
const rows = ref<DraftInterval[]>(props.modelValue.map(toDraftInterval))
const validationError = ref('')
const invalidPrices = new Set<string>()

onMounted(() => {
  validateAndReport()
})

watch(() => props.modelValue, value => {
  const signature = JSON.stringify(value)
  if (signature === lastEmittedSignature) {
    lastEmittedSignature = ''
    return
  }
  rows.value = value.map(toDraftInterval)
  invalidPrices.clear()
  validationError.value = ''
  validateAndReport()
}, { deep: true })

watch(() => props.billingMode, () => {
  invalidPrices.clear()
  validationError.value = ''
  validateAndReport()
})

function toDraftInterval(interval: ModelPricingInterval): DraftInterval {
  return {
    id: nextRowId++,
    min_tokens: String(interval.min_tokens),
    max_tokens: interval.max_tokens == null ? '' : String(interval.max_tokens),
    tier_label: interval.tier_label ?? '',
    input_price: interval.input_price ?? null,
    output_price: interval.output_price ?? null,
    cache_write_price: interval.cache_write_price ?? null,
    cache_read_price: interval.cache_read_price ?? null,
    per_request_price: interval.per_request_price ?? null,
  }
}

function updateBound(row: DraftInterval, key: BoundKey, event: Event) {
  row[key] = (event.target as HTMLInputElement).value
  emitIfValid()
}

function updateTierLabel(row: DraftInterval, event: Event) {
  row.tier_label = (event.target as HTMLInputElement).value
  emitIfValid()
}

function updatePrice(row: DraftInterval, key: PriceKey, value: number | null) {
  row[key] = value
  emitIfValid()
}

function priceValidityKey(row: DraftInterval, key: PriceKey): string {
  return `${row.id}:${key}`
}

function setPriceValidity(row: DraftInterval, key: PriceKey, valid: boolean) {
  const validityKey = priceValidityKey(row, key)
  const wasInvalid = invalidPrices.has(validityKey)
  if (valid) invalidPrices.delete(validityKey)
  else invalidPrices.add(validityKey)
  if (!valid) {
    validationError.value = 'Resolve invalid price values before continuing.'
    emit('update:valid', false)
  } else if (wasInvalid) emitIfValid()
}

function addRow() {
  const finiteMaxima = rows.value
    .map(row => parseInteger(row.max_tokens))
    .filter((value): value is number => value !== null)
  const minTokens = finiteMaxima.length ? Math.max(...finiteMaxima) : 0
  rows.value.push(toDraftInterval({ min_tokens: minTokens, max_tokens: null }))
  emitIfValid()
}

function deleteRow(index: number) {
  const [deleted] = rows.value.splice(index, 1)
  if (deleted) {
    for (const key of priceKeys) {
      invalidPrices.delete(priceValidityKey(deleted, key))
    }
  }
  emitIfValid()
}

function normalizeRows() {
  const normalized = validateAndReport()
  if (!normalized) return
  rows.value.sort((left, right) => Number(left.min_tokens) - Number(right.min_tokens))
  emitNormalized(normalized)
}

function emitIfValid() {
  const normalized = validateAndReport()
  if (normalized) emitNormalized(normalized)
}

function validateAndReport(): ModelPricingInterval[] | null {
  const normalized = validateAndNormalize()
  emit('update:valid', normalized !== null)
  return normalized
}

function emitNormalized(normalized: ModelPricingInterval[]) {
  lastEmittedSignature = JSON.stringify(normalized)
  emit('update:modelValue', normalized)
}

function validateAndNormalize(): ModelPricingInterval[] | null {
  if (invalidPrices.size > 0) {
    validationError.value = 'Resolve invalid price values before continuing.'
    return null
  }

  const parsed: Array<{ row: DraftInterval; min: number; max: number | null }> = []
  for (const row of rows.value) {
    for (const key of priceKeys) {
      const price = row[key]
      if (price !== null && (!Number.isFinite(price) || price < 0)) {
        validationError.value = text.value.invalidPrice
        return null
      }
    }

    const min = parseInteger(row.min_tokens)
    const max = parseInteger(row.max_tokens)
    if (min === null || min < 0 || (row.max_tokens.trim() && (max === null || max < 0))) {
      validationError.value = 'Token bounds must be non-negative integers.'
      return null
    }
    if (max !== null && max <= min) {
      validationError.value = 'max_tokens must be greater than min_tokens.'
      return null
    }
    parsed.push({ row, min, max })
  }

  parsed.sort((left, right) => left.min - right.min)
  for (let index = 1; index < parsed.length; index += 1) {
    const previous = parsed[index - 1]
    const current = parsed[index]
    if (previous.max === null || previous.max > current.min) {
      validationError.value = 'Pricing intervals overlap after sorting by min_tokens.'
      return null
    }
  }

  validationError.value = ''
  return parsed.map(({ row, min, max }, sortOrder) => ({
    min_tokens: min,
    max_tokens: max,
    tier_label: row.tier_label.trim(),
    input_price: row.input_price,
    output_price: row.output_price,
    cache_write_price: row.cache_write_price,
    cache_read_price: row.cache_read_price,
    per_request_price: row.per_request_price,
    sort_order: sortOrder,
  }))
}

function parseInteger(value: string): number | null {
  if (!value.trim()) return null
  const parsed = Number(value)
  return Number.isInteger(parsed) ? parsed : null
}

validateAndNormalize()
</script>
