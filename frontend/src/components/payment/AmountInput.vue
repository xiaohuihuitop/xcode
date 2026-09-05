<template>
  <div class="space-y-4">
    <!-- Quick Amount Buttons -->
    <div v-if="filteredAmounts.length > 0">
      <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('payment.quickAmounts') }}
      </label>
      <div class="grid grid-cols-3 gap-2">
        <button
          v-for="amt in filteredAmounts"
          :key="amt"
          type="button"
          :class="[
            'rounded-lg border-2 px-4 py-3 text-center font-medium transition-colors',
            modelValue === amt
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/40 dark:text-primary-300'
              : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:border-dark-500',
          ]"
          @click="selectAmount(amt)"
        >
          {{ amt }}
        </button>
      </div>
    </div>

    <!-- Custom Amount Input -->
    <div>
      <label for="recharge-amount" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('payment.customAmountPrefix') }}<span
          data-testid="custom-amount-highlight"
          class="text-red-600 dark:text-red-400"
        >{{ t('payment.customAmountHighlight') }}</span>{{ t('payment.customAmountSuffix') }}
      </label>
      <div class="relative">
        <span
          data-testid="amount-currency-symbol"
          class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500"
        >
          {{ currencySymbol(currency) }}
        </span>
        <input
          id="recharge-amount"
          type="text"
          inputmode="decimal"
          :value="customText"
          :placeholder="placeholderText"
          aria-describedby="recharge-amount-limits"
          class="input w-full py-3 pl-8 pr-4"
          @input="handleInput"
        />
      </div>
      <div
        id="recharge-amount-limits"
        class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400"
      >
        <span data-testid="minimum-recharge-amount">
          {{ t('payment.minimumRechargeAmount') }}:
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ formattedConfiguredMin }}</span>
        </span>
        <span data-testid="maximum-recharge-amount">
          {{ t('payment.maximumRechargeAmount') }}:
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ formattedConfiguredMax }}</span>
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DEFAULT_PAYMENT_CURRENCY, currencySymbol, formatPaymentAmount } from './currency'

const props = withDefaults(defineProps<{
  amounts?: number[]
  modelValue: number | null
  min?: number
  max?: number
  currency?: string
  configuredMin?: number
  configuredMax?: number
}>(), {
  amounts: () => [10, 20, 50, 100, 200, 500, 1000, 2000, 5000],
  min: 0,
  max: 0,
  currency: DEFAULT_PAYMENT_CURRENCY,
  configuredMin: 0,
  configuredMax: 0,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t, locale } = useI18n()

const customText = ref('')

// 0 = no limit
const filteredAmounts = computed(() =>
  props.amounts.filter((a) => (props.min <= 0 || a >= props.min) && (props.max <= 0 || a <= props.max))
)

const placeholderText = computed(() => {
  if (props.min > 0 && props.max > 0) return `${props.min} - ${props.max}`
  if (props.min > 0) return `≥ ${props.min}`
  if (props.max > 0) return `≤ ${props.max}`
  return t('payment.enterAmount')
})

const localeCode = computed(() => typeof locale.value === 'string' ? locale.value : undefined)
const formattedConfiguredMin = computed(() => props.configuredMin > 0
  ? formatPaymentAmount(props.configuredMin, props.currency, localeCode.value)
  : t('payment.noLimit'))
const formattedConfiguredMax = computed(() => props.configuredMax > 0
  ? formatPaymentAmount(props.configuredMax, props.currency, localeCode.value)
  : t('payment.noLimit'))

const AMOUNT_PATTERN = /^\d*(\.\d{0,2})?$/

function selectAmount(amt: number) {
  customText.value = String(amt)
  emit('update:modelValue', amt)
}

function handleInput(e: Event) {
  const val = (e.target as HTMLInputElement).value
  if (!AMOUNT_PATTERN.test(val)) return
  customText.value = val
  if (val === '') {
    emit('update:modelValue', null)
    return
  }
  const num = parseFloat(val)
  if (!isNaN(num) && num > 0) {
    emit('update:modelValue', num)
  } else {
    emit('update:modelValue', null)
  }
}

watch(() => props.modelValue, (v) => {
  if (v !== null && String(v) !== customText.value) {
    customText.value = String(v)
  }
}, { immediate: true })
</script>
