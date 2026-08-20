<template>
  <BaseDialog
    :show="show"
    :title="platform ? t('admin.platforms.edit') : t('admin.platforms.create')"
    width="wide"
    @close="emit('close')"
  >
    <form class="space-y-5" @submit.prevent="submit">
      <div class="grid gap-4 md:grid-cols-2" data-tour="platform-form-identity">
        <label class="space-y-1.5">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.platforms.code') }}</span>
          <input
            v-model="form.code"
            data-test="platform-code"
            class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
            autocomplete="off"
            maxlength="50"
            required
          />
        </label>
        <label class="space-y-1.5">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.platforms.name') }}</span>
          <input
            v-model="form.name"
            data-test="platform-name"
            class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
            maxlength="100"
            required
          />
        </label>
        <label class="space-y-1.5">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.platforms.accountPlatform') }}</span>
          <select
            v-model="form.account_platform"
            data-test="platform-account-platform"
            class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
            required
          >
            <option v-for="option in accountPlatformOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
        </label>
        <label class="space-y-1.5">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.platforms.status') }}</span>
          <select
            v-model="form.status"
            data-test="platform-status"
            class="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
          >
            <option value="active">{{ t('common.active') }}</option>
            <option value="disabled">{{ t('common.disabled') }}</option>
          </select>
        </label>
      </div>

      <section class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-700">
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.platforms.endpointCapabilities') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.platforms.endpointCapabilitiesHint') }}</p>
        </div>
        <div class="flex flex-wrap gap-x-6 gap-y-2">
          <label v-for="endpoint in endpointOptions" :key="endpoint.value" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
            <input
              v-model="form.endpoint_capabilities"
              type="checkbox"
              :value="endpoint.value"
              :data-test="`endpoint-${endpoint.value}`"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
            />
            <span>{{ endpoint.label }}</span>
          </label>
        </div>
      </section>

      <section class="space-y-4 border-t border-gray-200 pt-5 dark:border-dark-700" data-tour="platform-form-models">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.platforms.modelRules') }}</h3>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.platforms.modelRulesHint') }}</p>
          </div>
          <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ modelPreset }}</span>
        </div>

        <div data-test="platform-model-whitelist">
          <ModelWhitelistSelector v-model="form.allowed_models" :platform="modelPreset" />
        </div>

        <div class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.platforms.modelMappings') }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.platforms.modelMappingsHint') }}</p>
            </div>
            <button type="button" class="btn btn-secondary gap-1.5" data-test="add-model-mapping" @click="addMapping()">
              <Icon name="plus" size="sm" />
              <span>{{ t('admin.platforms.addMapping') }}</span>
            </button>
          </div>

          <div v-if="form.mappings.length === 0" class="rounded-md border border-dashed border-gray-300 px-4 py-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
            {{ t('admin.platforms.noMappings') }}
          </div>
          <div v-for="(mapping, index) in form.mappings" :key="mapping.key" class="flex items-center gap-2">
            <input
              v-model="mapping.from"
              :data-test="`mapping-from-${index}`"
              class="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm text-gray-900 outline-none focus:border-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
              :placeholder="t('admin.platforms.mappingFromPlaceholder')"
            />
            <span class="text-gray-400">-&gt;</span>
            <input
              v-model="mapping.to"
              :data-test="`mapping-to-${index}`"
              class="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm text-gray-900 outline-none focus:border-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-100"
              :placeholder="t('admin.platforms.mappingToPlaceholder')"
            />
            <button
              type="button"
              class="icon-button text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-300"
              :title="t('common.delete')"
              :aria-label="t('common.delete')"
              @click="removeMapping(index)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>

        <div v-if="presetMappings.length > 0" class="flex flex-wrap gap-2">
          <button
            v-for="preset in presetMappings"
            :key="`${preset.from}-${preset.to}`"
            type="button"
            class="rounded-md border border-gray-200 px-2.5 py-1.5 text-xs text-gray-700 hover:border-primary-300 hover:text-primary-700 dark:border-dark-600 dark:text-gray-300 dark:hover:border-primary-700 dark:hover:text-primary-300"
            @click="addMapping(preset.from, preset.to)"
          >
            {{ preset.label }}
          </button>
        </div>
      </section>

      <p v-if="validationError" class="text-sm text-red-600 dark:text-red-400">{{ validationError }}</p>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="submitting" data-test="save-platform" data-tour="platform-form-submit" @click="submit">
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import { getPresetMappingsByPlatform } from '@/composables/useModelWhitelist'
import { buildPlatformModelRules, resolvePlatformModelPreset, splitPlatformModelRules, type PlatformModelMapping } from './platformModelRules'
import type { AccountPlatform, CreatePlatformPoolRequest, PlatformModelRule, PlatformPool } from '@/types'

type MappingForm = PlatformModelMapping & { key: string }

const props = defineProps<{
  show: boolean
  platform: PlatformPool | null
  submitting: boolean
}>()

const emit = defineEmits<{
  close: []
  save: [input: CreatePlatformPoolRequest]
}>()

const { t } = useI18n()
const accountPlatformOptions: Array<{ value: AccountPlatform; label: string }> = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'grok', label: 'Grok' },
  { value: 'antigravity', label: 'Antigravity' },
]
const endpointOptions = [
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
]

const form = reactive({
  code: '',
  name: '',
  account_platform: 'openai' as AccountPlatform,
  status: 'active' as 'active' | 'disabled',
  endpoint_capabilities: [] as string[],
  allowed_models: [] as string[],
  mappings: [] as MappingForm[],
  disabled_rules: [] as PlatformModelRule[],
})

const modelPreset = computed(() => resolvePlatformModelPreset(form.code, form.account_platform))
const presetMappings = computed(() => getPresetMappingsByPlatform(modelPreset.value))
const validationError = computed(() => {
  if (!form.code.trim() || !form.name.trim()) return t('admin.platforms.requiredFields')
  if (!/^[a-z0-9_-]+$/.test(form.code.trim().toLowerCase())) return t('admin.platforms.invalidCode')
  if (form.status === 'active' && form.endpoint_capabilities.length === 0) return t('admin.platforms.invalidEndpoints')
  if (form.mappings.some(mapping => !mapping.from.trim() || !mapping.to.trim())) return t('admin.platforms.invalidMapping')
  if (buildPlatformModelRules(form.allowed_models, form.mappings).length === 0) return t('admin.platforms.noModelsConfigured')
  return ''
})

function newMapping(from = '', to = ''): MappingForm {
  return { key: `${Date.now()}-${Math.random()}`, from, to }
}

function resetForm() {
  form.code = props.platform?.code ?? ''
  form.name = props.platform?.name ?? ''
  form.account_platform = props.platform?.account_platform ?? 'openai'
  form.status = props.platform?.status ?? 'active'
  form.endpoint_capabilities = [...(props.platform?.endpoint_capabilities ?? [])]
  const split = splitPlatformModelRules(props.platform?.model_rules ?? [])
  form.allowed_models = split.allowedModels
  form.mappings = split.mappings.map(mapping => newMapping(mapping.from, mapping.to))
  form.disabled_rules = split.disabledRules
}

function addMapping(from = '', to = '') {
  form.mappings.push(newMapping(from, to))
}

function removeMapping(index: number) {
  form.mappings.splice(index, 1)
}

function submit() {
  if (validationError.value) return
  emit('save', {
    code: form.code.trim().toLowerCase(),
    name: form.name.trim(),
    account_platform: form.account_platform,
    status: form.status,
    endpoint_capabilities: [...form.endpoint_capabilities],
    model_rules: buildPlatformModelRules(form.allowed_models, form.mappings, form.disabled_rules),
  })
}

watch(() => [props.show, props.platform] as const, ([show]) => {
  if (show) resetForm()
}, { immediate: true })
</script>
