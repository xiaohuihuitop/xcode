<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="emit('close')">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div>
          <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 2.958 1.076 5.665 3 7.938l3-2.647z" />
        </svg>
      </div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center">
        <p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p>
      </div>
      <div v-else class="max-h-96 space-y-3 overflow-y-auto">
        <div
          v-for="key in apiKeys"
          :key="key.id"
          class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="flex items-start justify-between">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span>
                <span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">{{ key.status }}</span>
              </div>
              <p class="truncate font-mono text-sm text-gray-500">{{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}</p>
            </div>
          </div>
          <div class="mt-3 grid gap-2 text-xs text-gray-500 sm:grid-cols-3">
            <div>
              <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('dashboard.platformsLabel') }}:</span>
              {{ key.platform_ids?.join(', ') || t('admin.users.none') }}
            </div>
            <div>
              <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('dashboard.subscriptionPlansLabel') }}:</span>
              {{ key.subscription_plan_ids?.join(', ') || t('admin.users.none') }}
            </div>
            <div>
              <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('dashboard.balanceLabel') }}:</span>
              {{ key.allow_balance !== false ? t('dashboard.balanceEnabled') : t('admin.users.none') }}
            </div>
            <div class="sm:col-span-3">{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminUser, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const apiKeys = ref<ApiKey[]>([])
const loading = ref(false)

const load = async (user: AdminUser | null) => {
  if (!user) return
  loading.value = true
  try {
    const response = await adminAPI.users.getUserApiKeys(user.id)
    apiKeys.value = response.items || []
  } catch (error) {
    console.error('Failed to load API keys:', error)
    apiKeys.value = []
  } finally {
    loading.value = false
  }
}

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
watch(() => [props.show, props.user] as const, ([show, user]) => {
  if (show) void load(user)
}, { immediate: true })
</script>
