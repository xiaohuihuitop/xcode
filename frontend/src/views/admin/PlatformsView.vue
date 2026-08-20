<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-end gap-2">
        <button class="icon-button" type="button" :disabled="loading" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadPlatforms">
          <Icon name="refresh" size="sm" :class="loading && 'animate-spin'" />
        </button>
        <button class="btn btn-primary gap-1.5" type="button" data-test="create-platform" data-tour="platforms-create-btn" @click="openCreate">
          <Icon name="plus" size="sm" />
          <span>{{ t('admin.platforms.create') }}</span>
        </button>
      </div>

      <div
        v-if="loadError"
        data-test="platform-load-error"
        class="flex items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
      >
        <span>{{ t('admin.platforms.loadFailed') }}</span>
        <button class="btn btn-secondary" type="button" @click="loadPlatforms">{{ t('admin.platforms.retry') }}</button>
      </div>

      <DataTable :columns="columns" :data="platforms" :loading="loading" row-key="id">
        <template #cell-name="{ row }">
          <div class="min-w-0">
            <div class="truncate font-medium text-gray-900 dark:text-gray-100">{{ row.name }}</div>
            <div class="truncate font-mono text-xs text-gray-500 dark:text-gray-400">{{ row.code }}</div>
          </div>
        </template>
        <template #cell-account_platform="{ value }">
          <div class="flex items-center gap-2">
            <PlatformIcon :platform="value" size="xs" />
            <span class="text-sm text-gray-700 dark:text-gray-200">{{ value }}</span>
          </div>
        </template>
        <template #cell-endpoint_capabilities="{ value }">
          <div v-if="value?.length" class="flex flex-wrap gap-1">
            <span
              v-for="endpoint in value"
              :key="endpoint"
              class="rounded border border-primary-200 bg-primary-50 px-1.5 py-0.5 text-xs text-primary-700 dark:border-primary-800 dark:bg-primary-950/40 dark:text-primary-300"
            >
              {{ endpointLabel(endpoint) }}
            </span>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
        </template>
        <template #cell-model_rules="{ value }">
          <div v-if="value?.length" class="flex max-w-md flex-wrap gap-1">
            <span
              v-for="rule in value"
              :key="rule.id ?? rule.model_pattern"
              class="rounded border px-1.5 py-0.5 font-mono text-xs"
              :class="rule.enabled ? 'border-primary-200 bg-primary-50 text-primary-700 dark:border-primary-800 dark:bg-primary-950/40 dark:text-primary-300' : 'border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-400'"
              :title="formatModelRule(rule)"
            >
              {{ formatModelRule(rule) }}
            </span>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-dark-500">{{ t('admin.platforms.noRules') }}</span>
        </template>
        <template #cell-status="{ row }">
          <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
              :checked="row.status === 'active'"
              :aria-label="t('admin.platforms.status')"
              @change="toggleStatus(row)"
            />
            <span>{{ row.status === 'active' ? t('common.active') : t('common.disabled') }}</span>
          </label>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-1">
            <button class="icon-button" type="button" :title="t('common.edit')" :aria-label="t('common.edit')" @click="openEdit(row)">
              <Icon name="edit" size="sm" />
            </button>
            <button class="icon-button text-red-600 hover:text-red-700 dark:text-red-400" type="button" :data-test="`delete-platform-${row.id}`" :disabled="previewingPlatformID !== null" :title="t('common.delete')" :aria-label="t('common.delete')" @click="confirmDelete(row)">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </template>
      </DataTable>
    </div>

    <PlatformPoolDialog :show="showDialog" :platform="editingPlatform" :submitting="saving" @close="closeDialog" @save="savePlatform" />
    <ConfirmDialog
      :show="deletingPlatform !== null"
      :title="t('admin.platforms.deleteTitle')"
	  :message="deleteMessage"
      :confirm-text="t('common.delete')"
	  :confirm-disabled="!deleteImpact?.can_delete"
      danger
      @confirm="deletePlatform"
      @cancel="cancelDelete"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CreatePlatformPoolRequest, PlatformDeleteImpact, PlatformPool } from '@/types'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlatformPoolDialog from '@/components/admin/platform/PlatformPoolDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const platforms = ref<PlatformPool[]>([])
const loading = ref(false)
const loadError = ref(false)
const saving = ref(false)
const showDialog = ref(false)
const editingPlatform = ref<PlatformPool | null>(null)
const deletingPlatform = ref<PlatformPool | null>(null)
const deleteImpact = ref<PlatformDeleteImpact | null>(null)
const previewingPlatformID = ref<number | null>(null)

const deleteMessage = computed(() => {
	const platform = deletingPlatform.value
	const impact = deleteImpact.value
	if (!platform || !impact) return ''
	const params = {
		name: platform.name,
		accounts: impact.accounts,
		api_keys: impact.api_keys,
		usage_logs: impact.usage_logs,
		audits: impact.audits,
		ops: impact.ops,
		configs: impact.configs,
	}
	return t(impact.can_delete ? 'admin.platforms.deleteMessage' : 'admin.platforms.deleteBlockedMessage', params)
})

const columns = computed((): Column[] => [
  { key: 'name', label: t('admin.platforms.name') },
  { key: 'account_platform', label: t('admin.platforms.accountPlatform') },
  { key: 'endpoint_capabilities', label: t('admin.platforms.endpointCapabilities') },
  { key: 'model_rules', label: t('admin.platforms.modelRules') },
  { key: 'status', label: t('admin.platforms.status') },
  { key: 'actions', label: t('common.actions') },
])

async function loadPlatforms() {
  loading.value = true
  loadError.value = false
  try {
    platforms.value = await adminAPI.platforms.list()
  } catch (error) {
    platforms.value = []
    loadError.value = true
    appStore.showError(extractApiErrorMessage(error, t('admin.platforms.loadFailed')))
  } finally {
    loading.value = false
  }
}

function endpointLabel(endpoint: string) {
  return endpoint === 'chat_completions' ? 'Chat Completions' : endpoint === 'responses' ? 'Responses' : endpoint
}

function formatModelRule(rule: PlatformPool['model_rules'][number]) {
  const pattern = rule.model_pattern.trim()
  const upstream = rule.upstream_model.trim()
  return upstream && upstream !== pattern ? `${pattern} -> ${upstream}` : pattern
}

function openCreate() {
  editingPlatform.value = null
  showDialog.value = true
}

function openEdit(platform: PlatformPool) {
  editingPlatform.value = platform
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editingPlatform.value = null
}

async function savePlatform(input: CreatePlatformPoolRequest) {
  saving.value = true
  try {
    if (editingPlatform.value) {
      await adminAPI.platforms.update(editingPlatform.value.id, input)
      appStore.showSuccess(t('admin.platforms.updated'))
    } else {
      await adminAPI.platforms.create(input)
      appStore.showSuccess(t('admin.platforms.created'))
    }
    closeDialog()
    await loadPlatforms()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.platforms.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function toggleStatus(platform: PlatformPool) {
  try {
    await adminAPI.platforms.update(platform.id, { status: platform.status === 'active' ? 'disabled' : 'active' })
    await loadPlatforms()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.platforms.saveFailed')))
  }
}

async function confirmDelete(platform: PlatformPool) {
  if (previewingPlatformID.value !== null) return
  previewingPlatformID.value = platform.id
  try {
    deleteImpact.value = await adminAPI.platforms.previewDelete(platform.id)
    deletingPlatform.value = platform
  } catch (error) {
    cancelDelete()
    appStore.showError(extractApiErrorMessage(error, t('admin.platforms.deleteFailed')))
  } finally {
    previewingPlatformID.value = null
  }
}

function cancelDelete() {
  deletingPlatform.value = null
  deleteImpact.value = null
}

async function deletePlatform() {
  const platform = deletingPlatform.value
	if (!platform || !deleteImpact.value?.can_delete) return
  try {
    await adminAPI.platforms.remove(platform.id)
    cancelDelete()
    appStore.showSuccess(t('admin.platforms.deleted'))
    await loadPlatforms()
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.platforms.errors', t('admin.platforms.deleteFailed')))
  }
}

onMounted(loadPlatforms)
</script>
