<template>
  <div class="min-w-0">
    <div
      class="grid h-9 w-full min-w-0 grid-cols-3 overflow-hidden rounded-md border border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-900"
      role="group"
      :aria-label="text.modeGroup"
    >
      <button
        v-for="option in modeOptions"
        :key="option.mode"
        type="button"
        class="min-w-0 truncate px-2 text-xs font-medium transition-colors focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
        :class="mode === option.mode
          ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300'
          : 'text-gray-600 hover:bg-white/70 dark:text-gray-300 dark:hover:bg-dark-700/70'"
        :data-mode="option.mode"
        :aria-pressed="mode === option.mode"
        :title="option.label"
        @click="selectMode(option.mode)"
      >
        {{ option.label }}
      </button>
    </div>

    <label v-if="mode === 'custom'" class="mt-2 block min-w-0">
      <span class="sr-only">{{ text.inputLabel }}</span>
      <div class="flex min-w-0 items-stretch">
        <input
          :value="customValue"
          class="input min-w-0 rounded-r-none rounded-l-md font-mono tabular-nums"
          :class="error && 'input-error'"
          data-testid="custom-price-input"
          type="number"
          min="0"
          step="any"
          inputmode="decimal"
          :aria-invalid="Boolean(error)"
          :aria-label="text.inputLabel"
          @input="updateCustomValue"
        />
        <span class="flex max-w-[45%] shrink-0 items-center rounded-r-md border border-l-0 border-gray-200 bg-gray-50 px-2 text-xs text-gray-500 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-400">
          <span class="truncate" :title="unitLabel">{{ unitLabel }}</span>
        </span>
      </div>
    </label>
    <p v-if="error" class="input-error-text break-words" role="alert">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { TOKEN_PRICE_SCALE } from '@/utils/pricing'

type PricingValueMode = 'inherit' | 'custom' | 'zero'

const props = withDefaults(defineProps<{
  modelValue: number | null
  scale?: number
  unitLabel?: string
  labels?: Partial<{
    modeGroup: string
    inherit: string
    custom: string
    zero: string
    inputLabel: string
    required: string
    invalid: string
  }>
}>(), {
  scale: TOKEN_PRICE_SCALE,
  unitLabel: '$ / MTok',
  labels: () => ({}),
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  'validity-change': [valid: boolean]
}>()

const text = computed(() => ({
  modeGroup: props.labels.modeGroup ?? 'Price behavior',
  inherit: props.labels.inherit ?? 'Inherit',
  custom: props.labels.custom ?? 'Custom',
  zero: props.labels.zero ?? 'Zero',
  inputLabel: props.labels.inputLabel ?? 'Custom price',
  required: props.labels.required ?? 'Custom price is required.',
  invalid: props.labels.invalid ?? 'Custom price must be greater than zero.',
}))

const modeOptions = computed<Array<{ mode: PricingValueMode; label: string }>>(() => [
  { mode: 'inherit', label: text.value.inherit },
  { mode: 'custom', label: text.value.custom },
  { mode: 'zero', label: text.value.zero },
])

const mode = ref<PricingValueMode>(modeForValue(props.modelValue))
const customValue = ref(displayValue(props.modelValue))
const error = ref(modelValueError(props.modelValue))

onMounted(() => {
  if (error.value) emit('validity-change', false)
})

watch(() => props.modelValue, value => {
  mode.value = modeForValue(value)
  customValue.value = displayValue(value)
  error.value = modelValueError(value)
  emit('validity-change', !error.value)
})

function modeForValue(value: number | null): PricingValueMode {
  if (value === null) return 'inherit'
  return value === 0 ? 'zero' : 'custom'
}

function displayValue(value: number | null): string {
  return value != null && value !== 0 ? String(value * props.scale) : ''
}

function modelValueError(value: number | null): string {
  return value != null && (!Number.isFinite(value) || value < 0) ? text.value.invalid : ''
}

function selectMode(nextMode: PricingValueMode) {
  mode.value = nextMode
  error.value = ''

  if (nextMode === 'inherit') {
    customValue.value = ''
    emit('update:modelValue', null)
    emit('validity-change', true)
    return
  }
  if (nextMode === 'zero') {
    customValue.value = ''
    emit('update:modelValue', 0)
    emit('validity-change', true)
    return
  }

  customValue.value = displayValue(props.modelValue)
  error.value = modelValueError(props.modelValue)
  if (error.value) {
    emit('validity-change', false)
  } else if (!customValue.value) {
    error.value = text.value.required
    emit('validity-change', false)
  }
}

function updateCustomValue(event: Event) {
  const rawValue = (event.target as HTMLInputElement).value
  customValue.value = rawValue
  if (!rawValue.trim()) {
    error.value = text.value.required
    emit('validity-change', false)
    return
  }

  const value = Number(rawValue)
  if (!Number.isFinite(value) || value <= 0) {
    error.value = text.value.invalid
    emit('validity-change', false)
    return
  }

  error.value = ''
  emit('update:modelValue', value / props.scale)
  emit('validity-change', true)
}
</script>
