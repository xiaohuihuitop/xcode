<template>
  <div>
    <div class="flex min-h-10 flex-wrap gap-1.5 rounded-lg border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-800">
      <span
        v-for="(model, index) in models"
        :key="model"
        class="inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-sm"
        :class="getPlatformTagClass(platform || '')"
      >
        {{ model }}
        <button
          type="button"
          class="ml-0.5 rounded-full p-0.5 hover:bg-primary-200 dark:hover:bg-primary-800"
          @click="removeModel(index)"
        >
          <Icon name="x" size="xs" />
        </button>
      </span>
      <input
        ref="inputRef"
        v-model="inputValue"
        type="text"
        class="min-w-[120px] flex-1 border-none bg-transparent text-sm outline-none placeholder:text-gray-400 dark:text-white"
        :placeholder="models.length === 0 ? placeholder : ''"
        @keydown.enter.prevent="addModel"
        @keydown.tab.prevent="addModel"
        @keydown.delete="handleBackspace"
        @paste="handlePaste"
        @blur="addModel"
      />
    </div>
    <p class="mt-1 text-xs text-gray-400">
      {{ t('admin.channelMonitor.form.modelInputHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { getPlatformTagClass } from './modelTagStyles'

const { t } = useI18n()

const props = defineProps<{
  models: string[]
  placeholder?: string
  platform?: string
}>()

const emit = defineEmits<{
  'update:models': [models: string[]]
}>()

const inputValue = ref('')
const inputRef = ref<HTMLInputElement>()

function addModel() {
  const model = inputValue.value.trim()
  if (!model) return
  if (!props.models.includes(model)) emit('update:models', [...props.models, model])
  inputValue.value = ''
}

function removeModel(index: number) {
  const models = [...props.models]
  models.splice(index, 1)
  emit('update:models', models)
}

function handleBackspace() {
  if (inputValue.value === '' && props.models.length > 0) removeModel(props.models.length - 1)
}

function handlePaste(event: ClipboardEvent) {
  event.preventDefault()
  const text = event.clipboardData?.getData('text') || ''
  const models = text.split(/[,\n;]+/).map((value) => value.trim()).filter(Boolean)
  if (models.length === 0) return
  emit('update:models', [...new Set([...props.models, ...models])])
  inputValue.value = ''
}
</script>
