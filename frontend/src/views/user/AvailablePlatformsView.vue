<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <input
            v-model="search"
            type="search"
            class="input w-full sm:max-w-xs"
            :placeholder="t('common.searchPlaceholder')"
          />
          <button class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="load">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #table>
        <AvailablePlatformsTable :rows="filteredPlatforms" :loading="loading" />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import AvailablePlatformsTable from '@/components/platforms/AvailablePlatformsTable.vue'
import platformCatalogAPI from '@/api/platformCatalog'
import type { AvailablePlatformPool } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const platforms = ref<AvailablePlatformPool[]>([])
const loading = ref(false)
const search = ref('')

const filteredPlatforms = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return platforms.value
  return platforms.value.filter((platform) => {
    const models = platform.models ?? []
    return [platform.name, platform.code, platform.account_platform, ...models.map((model) => model.pattern)]
      .some((value) => value.toLowerCase().includes(query))
  })
})

async function load() {
  loading.value = true
  try {
    platforms.value = await platformCatalogAPI.listAvailablePlatforms()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
