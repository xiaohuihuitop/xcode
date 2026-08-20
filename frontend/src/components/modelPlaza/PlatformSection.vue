<template>
  <section class="overflow-hidden rounded-xl border bg-white shadow-card dark:bg-dark-800/50" :class="platformBorderStrongClass(platform.account_platform)">
    <header class="border-b border-gray-100 px-5 py-4 dark:border-dark-700/60">
      <div class="flex flex-wrap items-center gap-2">
        <PlatformIcon :platform="platform.account_platform as AccountPlatform" size="sm" />
        <h2 class="font-semibold text-gray-900 dark:text-white">{{ platform.name }}</h2>
        <span class="text-xs text-gray-400 dark:text-dark-500">{{ platform.code }}</span>
      </div>
      <div class="mt-2 flex flex-wrap gap-1.5 text-xs text-gray-500 dark:text-dark-400">
        <span v-for="capability in platform.endpoint_capabilities" :key="capability" class="badge badge-gray">
          {{ capability }}
        </span>
      </div>
    </header>
    <PlatformModelPricingTable :models="platform.models" />
  </section>
</template>

<script setup lang="ts">
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformBorderStrongClass } from '@/utils/platformColors'
import type { AccountPlatform } from '@/types'
import type { ModelPlazaPlatform } from '@/api/modelPlaza'
import PlatformModelPricingTable from './PlatformModelPricingTable.vue'

defineProps<{ platform: ModelPlazaPlatform }>()
</script>
