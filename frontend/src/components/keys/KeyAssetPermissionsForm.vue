<template>
  <div class="space-y-5">
    <section class="space-y-2" data-tour="key-form-platforms">
      <div>
        <label class="input-label">{{ t('keys.platformsLabel') }}</label>
        <p class="input-hint">{{ t('keys.platformsHint') }}</p>
      </div>
      <div v-if="platforms.length" class="max-h-56 space-y-1 overflow-y-auto rounded-md border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-800">
        <label
          v-for="platform in platforms"
          :key="platform.id"
          class="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-gray-50 dark:hover:bg-dark-700"
        >
          <input
            :data-test="`key-platform-${platform.id}`"
            type="checkbox"
            :checked="platformIds.includes(platform.id)"
            class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-900"
            @change="togglePlatform(platform.id, checked($event))"
          />
          <PlatformIcon :platform="platform.account_platform" size="sm" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium text-gray-900 dark:text-gray-100">{{ platform.name }}</span>
            <span class="block truncate font-mono text-xs text-gray-500 dark:text-gray-400">{{ platform.code }}</span>
          </span>
        </label>
      </div>
      <p v-else class="text-sm text-amber-700 dark:text-amber-300">{{ t('keys.noPlatforms') }}</p>
    </section>

    <section class="space-y-2 border-t border-gray-200 pt-5 dark:border-dark-700" data-tour="key-form-billing">
      <div>
        <label class="input-label">{{ t('keys.billingLabel') }}</label>
        <p class="input-hint">{{ t('keys.billingHint') }}</p>
      </div>
      <label class="flex cursor-pointer items-start gap-3">
        <input
          data-test="key-all-subscriptions"
          type="checkbox"
          :checked="allowAllSubscriptions"
          class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-900"
          @change="emit('update:allowAllSubscriptions', checked($event))"
        />
        <span>
          <span class="block text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('keys.allowAllSubscriptionsLabel') }}</span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('keys.allowAllSubscriptionsHint') }}</span>
        </span>
      </label>
      <label class="flex cursor-pointer items-start gap-3">
        <input
          data-test="key-balance"
          type="checkbox"
          :checked="allowBalance"
          class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-900"
          @change="emit('update:allowBalance', checked($event))"
        />
        <span>
          <span class="block text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('keys.balanceLabel') }}</span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('keys.balanceHint') }}</span>
        </span>
      </label>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { AvailablePlatformPool } from '@/types'

const props = defineProps<{
  platforms: AvailablePlatformPool[]
  platformIds: number[]
  allowAllSubscriptions: boolean
  allowBalance: boolean
}>()

const emit = defineEmits<{
  'update:platformIds': [ids: number[]]
  'update:allowAllSubscriptions': [allowed: boolean]
  'update:allowBalance': [allowed: boolean]
}>()

const { t } = useI18n()

function checked(event: Event): boolean {
  return (event.target as HTMLInputElement).checked
}

function replaceSelection(ids: number[], id: number, selected: boolean): number[] {
  const next = new Set(ids)
  if (selected) next.add(id)
  else next.delete(id)
  return [...next].sort((left, right) => left - right)
}

function togglePlatform(id: number, selected: boolean) {
  emit('update:platformIds', replaceSelection(props.platformIds, id, selected))
}

</script>
