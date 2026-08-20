<template>
  <div class="table-wrapper">
    <table class="w-full table-fixed border-collapse text-sm">
      <thead>
        <tr class="border-b border-gray-100 bg-gray-50/50 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:bg-dark-800/50 dark:text-gray-400">
          <th class="w-48 px-4 py-3">{{ t('nav.platforms') }}</th>
          <th class="w-40 px-4 py-3">{{ t('availablePlatforms.accountPlatform') }}</th>
          <th class="px-4 py-3">{{ t('availablePlatforms.models') }}</th>
        </tr>
      </thead>
      <tbody v-if="loading">
        <tr><td colspan="3" class="py-10 text-center"><Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" /></td></tr>
      </tbody>
      <tbody v-else-if="rows.length === 0">
        <tr><td colspan="3" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('availablePlatforms.empty') }}</td></tr>
      </tbody>
      <tbody v-else>
        <tr v-for="platform in rows" :key="platform.id" class="border-b border-gray-100 last:border-0 dark:border-dark-700">
          <td class="px-4 py-3 align-top">
            <div class="font-medium text-gray-900 dark:text-white">{{ platform.name }}</div>
            <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ platform.code }}</div>
          </td>
          <td class="px-4 py-3 align-top text-gray-700 dark:text-gray-300">{{ platform.account_platform }}</td>
          <td class="px-4 py-3 align-top">
            <div class="flex flex-wrap gap-1.5">
              <span v-for="model in platform.models ?? []" :key="`${platform.id}-${model.pattern}`" class="badge badge-gray">
                {{ model.pattern }}
              </span>
              <span v-if="(platform.models ?? []).length === 0" class="text-xs text-gray-400">-</span>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AvailablePlatformPool } from '@/types'

const { t } = useI18n()

defineProps<{
  rows: AvailablePlatformPool[]
  loading: boolean
}>()
</script>
