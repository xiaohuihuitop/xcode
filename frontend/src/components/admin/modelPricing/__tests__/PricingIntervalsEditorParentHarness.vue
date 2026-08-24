<template>
  <div>
    <PricingIntervalsEditor
      v-model="intervals"
      v-model:valid="valid"
      billing-mode="token"
    />
    <button data-testid="save-button" type="button" :disabled="valid !== true" @click="save">
      Save
    </button>
    <output data-testid="valid-status">{{ String(valid) }}</output>
    <output data-testid="parent-model">{{ JSON.stringify(intervals) }}</output>
    <output data-testid="save-count">{{ saveCount }}</output>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

import type { ModelPricingInterval } from '@/api/admin/modelPricing'
import PricingIntervalsEditor from '../PricingIntervalsEditor.vue'

const props = defineProps<{
  initialValue: ModelPricingInterval[]
}>()

const intervals = ref(props.initialValue.map(interval => ({ ...interval })))
const valid = ref<boolean | null>(null)
const saveCount = ref(0)

function save() {
  if (valid.value !== true) return
  saveCount.value += 1
}
</script>
