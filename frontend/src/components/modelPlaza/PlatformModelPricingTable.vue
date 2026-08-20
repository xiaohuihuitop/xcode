<template>
  <div class="overflow-x-auto">
    <table class="w-full min-w-[560px] table-fixed border-collapse text-sm tabular-nums">
      <thead>
        <tr class="border-b border-gray-200 text-left text-xs font-semibold uppercase tracking-wider text-gray-500 dark:border-dark-700 dark:text-dark-400">
          <th class="w-[34%] px-5 py-3">{{ t('modelPlaza.table.model') }}</th>
          <th class="w-[22%] px-3 py-3">{{ t('modelPlaza.table.input') }}</th>
          <th class="w-[22%] px-3 py-3">{{ t('modelPlaza.table.output') }}</th>
          <th class="w-[22%] px-3 py-3">{{ t('modelPlaza.table.cache') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="model in sortedModels"
          :key="model.pattern"
          class="border-b border-gray-100 last:border-b-0 hover:bg-gray-50/70 dark:border-dark-800 dark:hover:bg-dark-800/50"
        >
          <td class="px-5 py-3 align-top">
            <div class="font-medium text-gray-900 dark:text-white">{{ model.pattern }}</div>
            <div v-if="model.upstream_model" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ model.upstream_model }}
            </div>
          </td>
          <td class="px-3 py-3 align-top font-mono text-xs text-gray-700 dark:text-gray-200">
            {{ formatPrice(model.pricing?.input_price) }}
          </td>
          <td class="px-3 py-3 align-top font-mono text-xs text-gray-700 dark:text-gray-200">
            {{ formatPrice(model.pricing?.output_price) }}
          </td>
          <td class="px-3 py-3 align-top text-xs text-gray-600 dark:text-dark-300">
            <span v-if="hasCache(model.pricing)">
              {{ t('modelPlaza.table.cacheWrite') }} {{ formatPrice(model.pricing?.cache_write_price) }} /
              {{ t('modelPlaza.table.cacheRead') }} {{ formatPrice(model.pricing?.cache_read_price) }}
            </span>
            <span v-else>-</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled } from '@/utils/pricing'
import type { PlatformPlazaModel, PlatformPlazaPricing } from '@/api/modelPlaza'

const props = defineProps<{ models: PlatformPlazaModel[] }>()
const { t } = useI18n()

const sortedModels = computed(() => [...props.models].sort((a, b) => a.pattern.localeCompare(b.pattern)))

function formatPrice(value: number | null | undefined): string {
  if (value == null) return '-'
  return formatScaled(value, 1_000_000, 2)
}

function hasCache(pricing: PlatformPlazaPricing | null | undefined): boolean {
  return pricing?.cache_write_price != null || pricing?.cache_read_price != null
}
</script>
