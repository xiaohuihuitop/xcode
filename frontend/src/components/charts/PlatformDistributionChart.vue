<template>
  <div class="card p-4">
    <div class="mb-4 flex items-center justify-between gap-3">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('admin.dashboard.platformDistribution') }}
      </h3>
      <div v-if="showMetricToggle" class="inline-flex rounded-lg border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-700 dark:bg-dark-800">
        <button type="button" class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors" :class="metric === 'tokens' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400'" @click="emit('update:metric', 'tokens')">
          {{ t('admin.dashboard.metricTokens') }}
        </button>
        <button type="button" class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors" :class="metric === 'actual_cost' ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400'" @click="emit('update:metric', 'actual_cost')">
          {{ t('admin.dashboard.metricActualCost') }}
        </button>
      </div>
    </div>
    <div v-if="loading" class="flex h-48 items-center justify-center"><LoadingSpinner /></div>
    <div v-else-if="sortedStats.length" class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
      <div class="h-48 w-48 shrink-0"><Doughnut :data="chartData" :options="chartOptions" /></div>
      <div class="max-h-48 w-full min-w-0 flex-1 overflow-auto">
        <table class="w-full text-xs">
          <thead><tr class="text-gray-500 dark:text-gray-400"><th class="pb-2 text-left">{{ t('usage.platform') }}</th><th class="pb-2 text-right">{{ t('admin.dashboard.requests') }}</th><th class="pb-2 text-right">{{ t('admin.dashboard.tokens') }}</th><th class="pb-2 text-right">{{ t('admin.dashboard.actual') }}</th><th v-if="showAccountCost" class="pb-2 text-right">{{ t('admin.dashboard.accountCost') }}</th><th class="pb-2 text-right">{{ t('admin.dashboard.standard') }}</th></tr></thead>
          <tbody>
            <tr v-for="platform in sortedStats" :key="platform.platform_id" class="border-t border-gray-100 dark:border-dark-700">
              <td class="max-w-[140px] truncate py-1.5 font-medium text-gray-900 dark:text-white" :title="platform.platform_name || platform.platform_code || '-'">{{ platform.platform_name || platform.platform_code || '-' }}</td>
              <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ formatNumber(platform.requests) }}</td>
              <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ formatTokens(platform.total_tokens) }}</td>
              <td class="py-1.5 text-right text-green-600 dark:text-green-400">${{ formatCost(platform.actual_cost) }}</td>
              <td v-if="showAccountCost" class="py-1.5 text-right text-orange-500 dark:text-orange-400">${{ formatCost(platform.account_cost) }}</td>
              <td class="py-1.5 text-right text-gray-400 dark:text-gray-500">${{ formatCost(platform.cost) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else class="flex h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.noDataAvailable') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { PlatformStat } from '@/types'

ChartJS.register(ArcElement, Tooltip, Legend)
const { t } = useI18n()
type DistributionMetric = 'tokens' | 'actual_cost'
const props = withDefaults(defineProps<{ platformStats: PlatformStat[]; loading?: boolean; metric?: DistributionMetric; showMetricToggle?: boolean; showAccountCost?: boolean }>(), { loading: false, metric: 'tokens', showMetricToggle: false, showAccountCost: true })
const emit = defineEmits<{ 'update:metric': [value: DistributionMetric] }>()
const colors = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316', '#6366f1', '#84cc16']
const toNumber = (value: unknown) => Number.isFinite(Number(value)) ? Number(value) : 0
const sortedStats = computed(() => [...(props.platformStats || [])].sort((a, b) => toNumber(b[props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens']) - toNumber(a[props.metric === 'actual_cost' ? 'actual_cost' : 'total_tokens'])))
const chartData = computed(() => ({ labels: sortedStats.value.map((p) => p.platform_name || p.platform_code || '-'), datasets: [{ data: sortedStats.value.map((p) => toNumber(props.metric === 'actual_cost' ? p.actual_cost : p.total_tokens)), backgroundColor: colors.slice(0, sortedStats.value.length), borderWidth: 0 }] }))
const chartOptions = computed(() => ({ responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false }, tooltip: { callbacks: { label: (context: any) => { const value = toNumber(context.raw); const total = context.dataset.data.reduce((a: number, b: number) => a + b, 0); const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'; return `${context.label}: ${props.metric === 'actual_cost' ? `$${formatCost(value)}` : formatTokens(value)} (${percentage}%)` } } } } }))
const formatTokens = (value: number) => value >= 1_000_000 ? `${(value / 1_000_000).toFixed(2)}M` : value >= 1_000 ? `${(value / 1_000).toFixed(2)}K` : value.toLocaleString()
const formatNumber = (value: number) => toNumber(value).toLocaleString()
const formatCost = (value: number | null | undefined) => { const n = toNumber(value); return n >= 1 ? n.toFixed(2) : n >= 0.01 ? n.toFixed(3) : n.toFixed(4) }
</script>
