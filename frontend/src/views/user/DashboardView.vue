<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" :subscriptions="subscriptions" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="onDateRangeChange" @granularityChange="onGranularityChange" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" /></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'; import { useAuthStore } from '@/stores/auth'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem, UserSubscription } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { getActiveSubscriptions } from '@/api/subscriptions'
import { formatDateLocalInput } from '@/utils/format'
import { isValidDateRange, readPersistedViewState, writePersistedViewState } from '@/composables/usePersistedViewState'

const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const subscriptions = ref<UserSubscription[]>([])

type DashboardViewState = { startDate: string; endDate: string; granularity: 'day' | 'hour' }
const DASHBOARD_VIEW_STORAGE_KEY = 'sub2api:user-dashboard:view-state:v1'
const isDashboardViewState = (value: unknown): value is DashboardViewState => {
  if (!value || typeof value !== 'object') return false
  const state = value as Partial<DashboardViewState>
  return isValidDateRange(state.startDate, state.endDate) && state.startDate <= (state.endDate ?? '') && (state.granularity === 'day' || state.granularity === 'hour')
}
const initialViewState = readPersistedViewState(DASHBOARD_VIEW_STORAGE_KEY, {
  startDate: formatDateLocalInput(new Date(Date.now() - 6 * 86400000)), endDate: formatDateLocalInput(new Date()), granularity: 'day' as const,
}, isDashboardViewState)
const startDate = ref(initialViewState.startDate); const endDate = ref(initialViewState.endDate); const granularity = ref<'day' | 'hour'>(initialViewState.granularity)
const persistDashboardViewState = () => writePersistedViewState(DASHBOARD_VIEW_STORAGE_KEY, { startDate: startDate.value, endDate: endDate.value, granularity: granularity.value })

const loadStats = async () => { loading.value = true; try { await authStore.refreshUser(); stats.value = await usageAPI.getDashboardStats() } catch (error) { console.error('Failed to load dashboard stats:', error) } finally { loading.value = false } }
const loadCharts = async () => { loadingCharts.value = true; try { const res = await Promise.all([usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as any }), usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value })]); trendData.value = res[0].trend || []; modelStats.value = res[1].models || [] } catch (error) { console.error('Failed to load charts:', error) } finally { loadingCharts.value = false } }
const loadRecent = async () => { loadingUsage.value = true; try { const res = await usageAPI.getByDateRange(startDate.value, endDate.value); recentUsage.value = res.items.slice(0, 5) } catch (error) { console.error('Failed to load recent usage:', error) } finally { loadingUsage.value = false } }
const loadPlatformQuotas = async () => { try { const data = await getMyPlatformQuotas(); platformQuotas.value = data.platform_quotas ?? [] } catch (error) { console.warn('Failed to load platform quotas:', error); platformQuotas.value = [] } }
const loadSubscriptions = async () => { try { subscriptions.value = await getActiveSubscriptions() } catch (error) { console.warn('Failed to load subscriptions:', error); subscriptions.value = [] } }
const refreshAll = () => { loadStats(); loadCharts(); loadRecent(); loadPlatformQuotas(); loadSubscriptions() }
const onDateRangeChange = () => { persistDashboardViewState(); loadCharts(); loadRecent() }
const onGranularityChange = () => { persistDashboardViewState(); loadCharts() }

onMounted(() => { refreshAll() })
</script>
