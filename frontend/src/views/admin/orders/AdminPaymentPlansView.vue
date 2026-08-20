<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- Balance rate and actions -->
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <label for="global-balance-rate-multiplier" class="text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t('payment.admin.globalBalanceRateMultiplier') }}
          </label>
          <div class="flex items-center">
            <input
              id="global-balance-rate-multiplier"
              v-model.number="globalBalanceRateMultiplier"
              data-testid="global-balance-rate-multiplier"
              type="number"
              min="0"
              step="0.01"
              class="input w-24 text-right"
            />
            <span class="ml-2 text-sm font-medium text-gray-500 dark:text-gray-400">x</span>
          </div>
          <button
            data-testid="save-global-balance-rate-multiplier"
            type="button"
            class="btn btn-secondary"
            :disabled="globalBalanceRateSaving"
            :title="t('common.save')"
            @click="saveGlobalBalanceRateMultiplier"
          >
            <Icon name="check" size="md" />
          </button>
        </div>

        <div class="flex items-center gap-2">
          <button @click="loadPlans" :disabled="plansLoading" class="btn btn-secondary" :title="t('common.refresh')">
            <Icon name="refresh" size="md" :class="plansLoading ? 'animate-spin' : ''" />
          </button>
          <button @click="openPlanEdit(null)" class="btn btn-primary">{{ t('payment.admin.createPlan') }}</button>
        </div>
      </div>

      <!-- Plans Table -->
      <DataTable :columns="planColumns" :data="plans" :loading="plansLoading">
        <template #cell-name="{ value }">
          <span class="text-sm font-medium text-gray-900 dark:text-white">{{ value }}</span>
        </template>
        <template #cell-price="{ value, row }">
          <div class="text-sm">
            <span class="font-medium text-gray-900 dark:text-white">{{ planCurrencySymbol(row.currency) }}{{ (value ?? 0).toFixed(2) }}</span>
            <span v-if="row.currency" class="ml-1 text-xs text-gray-400">{{ row.currency }}</span>
            <span v-if="row.original_price" class="ml-1 text-xs text-gray-400 line-through">{{ planCurrencySymbol(row.currency) }}{{ row.original_price.toFixed(2) }}</span>
          </div>
        </template>
        <template #cell-rate_multiplier="{ value }">
          <span class="text-sm font-medium text-gray-900 dark:text-white">{{ formatRateMultiplier(value) }}x</span>
        </template>
        <template #cell-limits="{ row }">
          <div class="space-y-0.5 text-xs text-gray-600 dark:text-gray-300">
            <div>{{ t('payment.admin.daily') }}: {{ formatPlanLimit(row.daily_limit_usd) }}</div>
            <div>{{ t('payment.admin.weekly') }}: {{ formatPlanLimit(row.weekly_limit_usd) }}</div>
            <div>{{ t('payment.admin.monthly') }}: {{ formatPlanLimit(row.monthly_limit_usd) }}</div>
          </div>
        </template>
        <template #cell-validity_days="{ value, row }">
          <span class="text-sm">{{ value }} {{ t('payment.admin.' + (row.validity_unit || 'days')) }}</span>
        </template>
        <template #cell-for_sale="{ value, row }">
          <button
            type="button"
            :class="[
              'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
              value ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'
            ]"
            @click="toggleForSale(row)"
          >
            <span :class="[
              'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
              value ? 'translate-x-4' : 'translate-x-0'
            ]" />
          </button>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-2">
            <button @click="openPlanEdit(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400">
              <Icon name="edit" size="sm" />
              <span class="text-xs">{{ t('common.edit') }}</span>
            </button>
            <button @click="confirmDeletePlan(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400">
              <Icon name="trash" size="sm" />
              <span class="text-xs">{{ t('common.delete') }}</span>
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- Plan Edit Dialog -->
    <PlanEditDialog :show="showPlanDialog" :plan="editingPlan" :payment-config="paymentConfig" @close="showPlanDialog = false" @saved="loadPlans" />

    <ConfirmDialog :show="showDeletePlanDialog" :title="t('payment.admin.deletePlan')" :message="t('payment.admin.deletePlanConfirm')" :confirm-text="t('common.delete')" danger @confirm="handleDeletePlan" @cancel="showDeletePlanDialog = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type { SubscriptionPlan } from '@/types/payment'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import PlanEditDialog from './PlanEditDialog.vue'
import { currencySymbol } from '@/components/payment/currency'

const { t } = useI18n()
const appStore = useAppStore()

function planCurrencySymbol(currency?: string): string {
  return currencySymbol(currency || 'USD')
}

function formatRateMultiplier(value?: number): string {
  return Number.isFinite(value) ? Number(value).toFixed(2) : '1.00'
}

function formatPlanLimit(value?: number | null): string {
  return typeof value === 'number' && value > 0
    ? `$${value.toFixed(2)}`
    : t('payment.admin.unlimited')
}

const paymentConfig = ref<AdminPaymentConfig | null>(null)
const globalBalanceRateMultiplier = ref(1)
const globalBalanceRateSaving = ref(false)

async function loadPaymentConfig() {
  try {
    const res = await adminPaymentAPI.getConfig()
    paymentConfig.value = res.data
  } catch { /* preview only */ }
}

async function loadGlobalBalanceRateMultiplier() {
  try {
    const res = await adminPaymentAPI.getGlobalBalanceRateMultiplier()
    const rate = Number(res.data?.rate_multiplier)
    if (Number.isFinite(rate) && rate >= 0) globalBalanceRateMultiplier.value = rate
  } catch { /* retain the compatibility default */ }
}

async function saveGlobalBalanceRateMultiplier() {
  const rate = Number(globalBalanceRateMultiplier.value)
  if (!Number.isFinite(rate) || rate < 0) {
    appStore.showError(t('payment.admin.invalidGlobalBalanceRateMultiplier'))
    return
  }

  globalBalanceRateSaving.value = true
  try {
    const res = await adminPaymentAPI.updateGlobalBalanceRateMultiplier(rate)
    globalBalanceRateMultiplier.value = res.data.rate_multiplier
    appStore.showSuccess(t('common.saved'))
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    globalBalanceRateSaving.value = false
  }
}

// ==================== Plans ====================

const plansLoading = ref(false)
const plans = ref<SubscriptionPlan[]>([])
const showPlanDialog = ref(false)
const showDeletePlanDialog = ref(false)
const editingPlan = ref<SubscriptionPlan | null>(null)
const deletingPlanId = ref<number | null>(null)

const planColumns = computed((): Column[] => [
  { key: 'id', label: 'ID' },
  { key: 'name', label: t('payment.admin.planName') },
  { key: 'rate_multiplier', label: t('payment.admin.rateMultiplier') },
  { key: 'limits', label: t('payment.planCard.quota') },
  { key: 'price', label: t('payment.admin.price') },
  { key: 'validity_days', label: t('payment.admin.validity') },
  { key: 'for_sale', label: t('payment.admin.forSale') },
  { key: 'sort_order', label: t('payment.admin.sortOrder') },
  { key: 'actions', label: t('common.actions') },
])

async function loadPlans() {
  plansLoading.value = true
  try {
    const res = await adminPaymentAPI.getPlans()
    // Backend returns features as newline-separated string; parse to array
    plans.value = (res.data || []).map((p: Omit<SubscriptionPlan, 'features'> & { features: string | string[] }) => ({
      ...p,
      features: typeof p.features === 'string'
        ? p.features.split('\n').map((f: string) => f.trim()).filter(Boolean)
        : (p.features || []),
    }))
  }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { plansLoading.value = false }
}

function openPlanEdit(plan: SubscriptionPlan | null) {
  editingPlan.value = plan
  showPlanDialog.value = true
}


/** Quick toggle for_sale from the list */
async function toggleForSale(plan: SubscriptionPlan) {
  try {
    await adminPaymentAPI.updatePlan(plan.id, { for_sale: !plan.for_sale })
    plan.for_sale = !plan.for_sale
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  }
}

function confirmDeletePlan(plan: SubscriptionPlan) { deletingPlanId.value = plan.id; showDeletePlanDialog.value = true }
async function handleDeletePlan() {
  if (!deletingPlanId.value) return
  try { await adminPaymentAPI.deletePlan(deletingPlanId.value); appStore.showSuccess(t('common.deleted')); showDeletePlanDialog.value = false; loadPlans() }
  catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
}

// ==================== Lifecycle ====================

onMounted(() => {
  loadPaymentConfig()
  loadGlobalBalanceRateMultiplier()
  loadPlans()
})
</script>
