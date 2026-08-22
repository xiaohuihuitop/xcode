<template>
  <div class="space-y-5">
    <div v-if="!embedded">
      <h1 class="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-1.5 text-sm text-gray-500 dark:text-dark-400">{{ t('modelPlaza.description') }}</p>
    </div>

    <div v-if="descriptionHtml" class="plaza-description rounded-xl border border-gray-100 bg-white px-5 py-4 text-sm shadow-card dark:border-dark-700/50 dark:bg-dark-800/50" v-html="descriptionHtml"></div>

    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600"></div>
    </div>
    <div v-else-if="error" class="rounded-xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-500">{{ t('modelPlaza.filters.platformLabel') }}</span>
        <button
          v-for="item in ['all', ...platforms]"
          :key="item"
          type="button"
          class="rounded-lg px-3 py-1.5 text-sm font-medium transition"
          :class="selectedPlatform === item ? 'bg-primary-600 text-white' : 'bg-white text-gray-600 ring-1 ring-inset ring-gray-200 hover:bg-gray-50 dark:bg-dark-800/60 dark:text-dark-300 dark:ring-dark-700'"
          @click="selectedPlatform = item"
        >
          {{ item === 'all' ? t('modelPlaza.filters.all') : item }}
        </button>
        <div class="relative ml-auto w-full sm:w-72">
          <input v-model="searchQuery" type="search" class="input w-full" :placeholder="t('modelPlaza.filters.searchPlaceholder')" />
        </div>
      </div>

      <div v-if="filteredPlatforms.length" class="space-y-5">
        <PlatformSection v-for="platform in filteredPlatforms" :key="platform.id" :platform="platform" />
      </div>
      <div v-else class="rounded-xl border border-dashed border-gray-300 px-5 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400">
        {{ searchQuery.trim() ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { ModelPlazaPlatform, ModelPlazaResponse } from '@/api/modelPlaza'
import PlatformSection from './PlatformSection.vue'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  embedded?: boolean
}>()

const { t } = useI18n()
const selectedPlatform = ref('all')
const searchQuery = ref('')

const descriptionHtml = computed(() => {
  const value = props.response?.description?.trim()
  return value ? DOMPurify.sanitize(marked.parse(value) as string) : ''
})

const platforms = computed(() => (props.response?.platforms ?? []).map((item) => item.code).filter((value, index, all) => all.indexOf(value) === index).sort())

const filteredPlatforms = computed<ModelPlazaPlatform[]>(() => {
  const selected = selectedPlatform.value
  const query = searchQuery.value.trim().toLowerCase()
  return (props.response?.platforms ?? [])
    .filter((item) => selected === 'all' || item.code === selected)
    .map((item) => query
      ? { ...item, models: item.models.filter((model) => `${model.pattern} ${model.upstream_model ?? ''}`.toLowerCase().includes(query)) }
      : item)
    .filter((item) => item.models.length > 0)
})
</script>

<style scoped>
.plaza-description { line-height: 1.7; overflow-wrap: anywhere; }
.plaza-description :deep(a) { @apply text-primary-600 underline underline-offset-4 dark:text-primary-300; }
</style>
