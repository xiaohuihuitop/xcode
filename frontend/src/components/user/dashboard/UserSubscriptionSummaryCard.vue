<template>
  <article class="card p-4">
    <div class="flex items-start gap-3">
      <div class="rounded-lg bg-primary-100 p-2 dark:bg-primary-900/30">
        <Icon name="creditCard" size="md" class="text-primary-700 dark:text-primary-300" :stroke-width="2" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('dashboard.subscriptionSummary.title') }}
            </p>
            <h3 class="mt-0.5 truncate text-base font-bold text-gray-900 dark:text-white">
              {{ subscriptionName }}
            </h3>
          </div>
          <span class="shrink-0 text-xs text-gray-500 dark:text-gray-400">
            {{ expirationText }}
          </span>
        </div>

        <div v-if="metric" class="mt-3 space-y-1.5">
          <div class="flex items-center justify-between gap-3 text-xs">
            <span class="font-medium text-gray-600 dark:text-gray-300">{{ metric.label }}</span>
            <span class="font-mono text-gray-700 dark:text-gray-200">
              &#36;{{ metric.used.toFixed(2) }} / &#36;{{ metric.limit.toFixed(2) }}
            </span>
          </div>
          <div class="h-2 overflow-hidden rounded-full bg-emerald-500/20 dark:bg-emerald-400/20">
            <div
              data-testid="subscription-progress"
              class="h-full rounded-full transition-all"
              :class="progressClass"
              :style="{ width: progress + '%' }"
            ></div>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span>
              {{ t('dashboard.subscriptionSummary.remaining') }}
              <strong class="font-semibold text-gray-800 dark:text-gray-200">&#36;{{ remaining.toFixed(2) }}</strong>
            </span>
            <span v-if="resetText">{{ resetText }}</span>
          </div>
        </div>

        <p v-else class="mt-3 text-sm font-medium text-emerald-700 dark:text-emerald-300">
          {{ t('dashboard.subscriptionSummary.unlimited') }}
        </p>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getRemainingDurationParts, isOneTimeDailyQuota } from '@/utils/subscriptionQuota'
import { getSubscriptionLimit, getSubscriptionPlanName } from '@/utils/subscriptionTerms'
import type { UserSubscription } from '@/types'

type UsageMetric = {
  label: string
  used: number
  limit: number
  windowStart: string | null
  windowHours: number
}

const props = defineProps<{ subscription: UserSubscription }>()
const { t } = useI18n()

const subscriptionName = computed(() => getSubscriptionPlanName(props.subscription))

const metric = computed<UsageMetric | null>(() => {
  const dailyLimit = getSubscriptionLimit(props.subscription, 'daily')
  const weeklyLimit = getSubscriptionLimit(props.subscription, 'weekly')
  const monthlyLimit = getSubscriptionLimit(props.subscription, 'monthly')
  if (dailyLimit) {
    return makeMetric('daily', props.subscription.daily_usage_usd, dailyLimit, props.subscription.daily_window_start, 24)
  }
  if (weeklyLimit) {
    return makeMetric('weekly', props.subscription.weekly_usage_usd, weeklyLimit, props.subscription.weekly_window_start, 168)
  }
  if (monthlyLimit) {
    return makeMetric('monthly', props.subscription.monthly_usage_usd, monthlyLimit, props.subscription.monthly_window_start, 720)
  }
  return null
})

const progress = computed(() => {
  if (!metric.value?.limit) return 0
  return Math.min(100, Math.max(0, (metric.value.used / metric.value.limit) * 100))
})
const remaining = computed(() => Math.max((metric.value?.limit ?? 0) - (metric.value?.used ?? 0), 0))
const progressClass = computed(() => {
  if (progress.value >= 95) return 'bg-red-500'
  if (progress.value >= 75) return 'bg-amber-500'
  return 'bg-primary-500'
})
const expirationText = computed(() => {
  if (!props.subscription.expires_at) return t('userSubscriptions.noExpiration')
  return new Date(props.subscription.expires_at).toLocaleDateString()
})
const resetText = computed(() => {
  if (!metric.value) return ''
  const target = dailyQuotaEnd() ?? windowEnd(metric.value.windowStart, metric.value.windowHours)
  const parts = target ? getRemainingDurationParts(target) : null
  return parts ? t('userSubscriptions.resetIn', { time: formatDuration(parts) }) : ''
})

function makeMetric(
  window: 'daily' | 'weekly' | 'monthly',
  used: number,
  limit: number,
  windowStart: string | null,
  windowHours: number,
): UsageMetric {
  return { label: t('userSubscriptions.' + window), used: used || 0, limit, windowStart, windowHours }
}

function dailyQuotaEnd(): string | null {
  if (metric.value?.windowHours !== 24 || !isOneTimeDailyQuota(props.subscription)) return null
  return props.subscription.expires_at
}

function windowEnd(start: string | null, hours: number): Date | null {
  if (!start) return null
  const startTime = new Date(start).getTime()
  return Number.isFinite(startTime) ? new Date(startTime + hours * 60 * 60 * 1000) : null
}

function formatDuration(parts: { days: number; hours: number; minutes: number }): string {
  if (parts.days > 0) return parts.days + 'd ' + parts.hours + 'h'
  if (parts.hours > 0) return parts.hours + 'h ' + parts.minutes + 'm'
  return parts.minutes + 'm'
}
</script>
