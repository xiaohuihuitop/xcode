<template>
  <div class="w-full min-w-0">
    <table
      data-testid="desktop-pricing-table"
      class="hidden w-full table-fixed border-collapse text-sm tabular-nums sm:table"
    >
      <thead>
        <tr class="border-b border-gray-200 text-left text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-dark-400">
          <th class="w-[28%] px-4 py-2.5">{{ t('modelPlaza.table.model') }}</th>
          <th class="w-[36%] px-4 py-2.5">{{ t('modelPlaza.table.officialPrice') }}</th>
          <th class="w-[36%] px-4 py-2.5">{{ t('modelPlaza.table.salePrice') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="item in displayModels"
          :key="item.model.pattern"
          class="border-b border-gray-100 last:border-b-0 hover:bg-gray-50/70 dark:border-dark-800 dark:hover:bg-dark-800/50"
        >
          <td class="px-4 py-3 align-top">
            <div class="min-w-0 break-words font-medium leading-5 text-gray-900 dark:text-white" :title="item.model.pattern">
              {{ item.model.pattern }}
            </div>
            <div
              v-if="item.model.upstream_model"
              class="mt-1 min-w-0 break-words text-xs leading-4 text-gray-400 dark:text-dark-500"
              :title="item.model.upstream_model"
            >
              {{ item.model.upstream_model }}
            </div>
          </td>
          <td class="px-4 py-3 align-top" :data-testid="`desktop-official-${item.model.pattern}`">
            <PricingFields v-if="item.official" :display="item.official" />
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.noPrice') }}</span>
          </td>
          <td class="px-4 py-3 align-top" :data-testid="`desktop-sale-${item.model.pattern}`">
            <div
              v-if="item.badgeKey"
              class="mb-2 inline-flex rounded border px-1.5 py-0.5 text-[11px] font-medium leading-4"
              :class="sourceBadgeClass(item.badgeKey)"
            >
              {{ t(item.badgeKey) }}
            </div>
            <PricingFields v-if="item.sale" :display="item.sale" />
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.noPrice') }}</span>
          </td>
        </tr>
      </tbody>
    </table>

    <div data-testid="mobile-pricing-list" class="divide-y divide-gray-100 sm:hidden dark:divide-dark-800">
      <article
        v-for="item in displayModels"
        :key="item.model.pattern"
        class="min-w-0 px-4 py-3"
        :data-testid="`mobile-model-${item.model.pattern}`"
      >
        <div class="min-w-0 break-words text-sm font-medium leading-5 text-gray-900 dark:text-white" :title="item.model.pattern">
          {{ item.model.pattern }}
        </div>
        <div
          v-if="item.model.upstream_model"
          class="mt-0.5 min-w-0 break-words text-xs leading-4 text-gray-400 dark:text-dark-500"
          :title="item.model.upstream_model"
        >
          {{ item.model.upstream_model }}
        </div>

        <dl class="mt-3 grid min-w-0 grid-cols-[minmax(6.75rem,38%)_minmax(0,1fr)] gap-x-3 gap-y-3">
          <dt class="text-xs font-medium leading-5 text-gray-500 dark:text-dark-400">
            {{ t('modelPlaza.table.officialPrice') }}
          </dt>
          <dd class="min-w-0">
            <PricingFields v-if="item.official" :display="item.official" />
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.noPrice') }}</span>
          </dd>

          <dt class="text-xs font-medium leading-5 text-gray-500 dark:text-dark-400">
            {{ t('modelPlaza.table.salePrice') }}
          </dt>
          <dd class="min-w-0">
            <div
              v-if="item.badgeKey"
              class="mb-2 inline-flex rounded border px-1.5 py-0.5 text-[11px] font-medium leading-4"
              :class="sourceBadgeClass(item.badgeKey)"
            >
              {{ t(item.badgeKey) }}
            </div>
            <PricingFields v-if="item.sale" :display="item.sale" />
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.noPrice') }}</span>
          </dd>
        </dl>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, type PropType, type VNode } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled, TOKEN_PRICE_SCALE } from '@/utils/pricing'
import type { PlatformPlazaModel, PlatformPlazaPricing } from '@/api/modelPlaza'

type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price' | 'image_input_price' | 'image_output_price' | 'per_request_price'

interface DisplayField {
  key: PriceKey
  label: string
  price: string
  unit: string
}

interface DisplayTier {
  label: string
  range: string
  fields: DisplayField[]
}

interface PricingDisplay {
  fields: DisplayField[]
  tiers: DisplayTier[]
}

const props = defineProps<{ models: PlatformPlazaModel[] }>()
const { t } = useI18n()

const tokenFields: Array<{ key: PriceKey; labelKey: string }> = [
  { key: 'input_price', labelKey: 'modelPlaza.table.input' },
  { key: 'output_price', labelKey: 'modelPlaza.table.output' },
  { key: 'cache_write_price', labelKey: 'modelPlaza.table.cacheWrite' },
  { key: 'cache_read_price', labelKey: 'modelPlaza.table.cacheRead' },
]

const imageTokenFields: Array<{ key: PriceKey; labelKey: string }> = [
  { key: 'image_input_price', labelKey: 'modelPlaza.table.imageInput' },
  { key: 'image_output_price', labelKey: 'modelPlaza.table.imageOutput' },
]

function fieldDefinitions(mode: string, interval = false): Array<{ key: PriceKey; labelKey: string; scale: number; unitKey: string }> {
  if (mode === 'per_request') {
    return [{ key: 'per_request_price', labelKey: 'modelPlaza.table.perRequest', scale: 1, unitKey: 'modelPlaza.table.unitPerRequest' }]
  }
  if (mode === 'image') {
    const perImage = { key: 'per_request_price' as const, labelKey: 'modelPlaza.table.perImage', scale: 1, unitKey: 'modelPlaza.table.unitPerImage' }
    if (interval) return [perImage]
    return [
      perImage,
      ...imageTokenFields.map((field) => ({ ...field, scale: TOKEN_PRICE_SCALE, unitKey: 'modelPlaza.table.unitPerMillion' })),
    ]
  }
  return tokenFields.map((field) => ({ ...field, scale: TOKEN_PRICE_SCALE, unitKey: 'modelPlaza.table.unitPerMillion' }))
}

function displayFields(
  values: Partial<Record<PriceKey, number | null>>,
  mode: string,
  interval = false,
): DisplayField[] {
  return fieldDefinitions(mode, interval).map((field) => {
    const value = values[field.key]
    return {
      key: field.key,
      label: t(field.labelKey),
      price: value == null ? t('modelPlaza.table.noPrice') : formatScaled(value, field.scale, 2),
      unit: value == null ? '' : t(field.unitKey),
    }
  })
}

function pricingDisplay(pricing: PlatformPlazaPricing | null | undefined): PricingDisplay | null {
  if (!pricing) return null
  const mode = pricing.billing_mode || 'token'
  return {
    fields: displayFields(pricing, mode),
    tiers: (pricing.intervals ?? []).map((interval) => {
      const range = formatIntervalRange(interval.min_tokens, interval.max_tokens)
      return {
        label: interval.tier_label || range,
        range,
        fields: displayFields(interval, mode, true),
      }
    }),
  }
}

function formatIntervalRange(minTokens: number, maxTokens: number | null): string {
  const firstMatchingToken = minTokens + 1
  return maxTokens == null
    ? t('modelPlaza.table.rangeUnbounded', { min: firstMatchingToken })
    : t('modelPlaza.table.rangeBounded', { min: firstMatchingToken, max: maxTokens })
}

function priceFieldNode(field: DisplayField): VNode {
  return h('div', { key: field.key, class: 'grid min-w-0 grid-cols-[minmax(4.5rem,auto)_minmax(0,1fr)] gap-x-2 text-xs leading-5' }, [
    h('span', { class: 'min-w-0 break-words text-gray-500 dark:text-dark-400', title: field.label }, field.label),
    h('span', { class: 'min-w-0 break-words text-right font-mono text-gray-700 dark:text-gray-200' }, field.unit ? `${field.price} ${field.unit}` : field.price),
  ])
}

const PricingFields = defineComponent({
  name: 'PricingFields',
  props: {
    display: { type: Object as PropType<PricingDisplay>, required: true },
  },
  setup(componentProps) {
    return () => h('div', { class: 'min-w-0 space-y-2' }, [
      h('div', { class: 'min-w-0 space-y-0.5' }, componentProps.display.fields.map(priceFieldNode)),
      ...(componentProps.display.tiers.length
        ? [h('div', { class: 'min-w-0 border-t border-gray-100 pt-2 dark:border-dark-700/70' }, [
            h('div', { class: 'mb-1 text-[11px] font-semibold leading-4 text-gray-500 dark:text-dark-400' }, t('modelPlaza.table.tiers')),
            h('div', { class: 'min-w-0 space-y-2' }, componentProps.display.tiers.map((tier, index) => h('div', { key: `${tier.label}-${index}`, class: 'min-w-0 border-l-2 border-gray-200 pl-2 dark:border-dark-600' }, [
              h('div', { class: 'min-w-0 break-words text-xs font-medium leading-4 text-gray-700 dark:text-gray-200', title: tier.label }, tier.label),
              h('div', { class: 'mb-0.5 text-[11px] leading-4 text-gray-400 dark:text-dark-500' }, `${t('modelPlaza.table.range')} ${tier.range}`),
              ...tier.fields.map(priceFieldNode),
            ]))),
          ])]
        : []),
    ])
  },
})

function sourceBadgeClass(key: string): string {
  return key === 'modelPlaza.table.inheritedPrice'
    ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
    : 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300'
}

const displayModels = computed(() => [...props.models]
  .sort((a, b) => a.pattern.localeCompare(b.pattern))
  .map((model) => ({
    model,
    official: pricingDisplay(model.official_pricing),
    sale: pricingDisplay(model.sale_pricing ?? model.pricing),
    badgeKey: model.sale_pricing_source === 'official'
      ? 'modelPlaza.table.inheritedPrice'
      : model.sale_pricing_source === 'custom'
        ? 'modelPlaza.table.customPrice'
        : null,
  })))
</script>
