<template>
  <div>
    <div class="mb-2 flex items-center justify-between text-xs text-gray-500 dark:text-dark-400">
      <span>{{ t('admin.announcements.form.selectPackages') }}</span>
      <span>{{ t('common.selectedCount', { count: modelValue.length }) }}</span>
    </div>
    <div class="grid max-h-40 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 bg-gray-50 p-2 sm:grid-cols-2 dark:border-dark-600 dark:bg-dark-800">
      <label
        v-for="plan in plans"
        :key="plan.id"
        class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm text-gray-700 transition-colors hover:bg-white dark:text-gray-200 dark:hover:bg-dark-700"
      >
        <input
          type="checkbox"
          :checked="modelValue.includes(plan.id)"
          class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
          @change="toggle(plan.id, ($event.target as HTMLInputElement).checked)"
        />
        <span class="min-w-0 flex-1 truncate">{{ plan.name }}</span>
        <span class="shrink-0 text-xs text-gray-400">#{{ plan.id }}</span>
      </label>
      <div v-if="plans.length === 0" class="col-span-2 py-2 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.noData') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'

const { t } = useI18n()

const props = defineProps<{
  modelValue: number[]
  plans: SubscriptionPlan[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: number[]]
}>()

function toggle(planID: number, checked: boolean) {
  const ids = checked
    ? [...new Set([...props.modelValue, planID])]
    : props.modelValue.filter((id) => id !== planID)
  emit('update:modelValue', ids)
}
</script>
