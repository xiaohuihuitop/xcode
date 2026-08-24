<template>
  <AppLayout>
    <div class="min-w-0 space-y-5 overflow-x-hidden">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.modelPricing.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.description') }}</p>
        </div>
        <button class="icon-button shrink-0" type="button" :disabled="catalogLoading || rulesLoading" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadAll">
          <Icon name="refresh" size="sm" :class="(catalogLoading || rulesLoading) && 'animate-spin'" />
        </button>
      </header>

      <section class="min-w-0" aria-labelledby="catalog-heading">
        <div class="flex flex-wrap items-end justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
          <h2 id="catalog-heading" class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.modelPricing.catalog') }}</h2>
          <form class="flex min-w-0 flex-1 flex-wrap items-end justify-end gap-2" @submit.prevent="loadCatalog">
            <label class="min-w-36 text-sm">
              <span class="sr-only">{{ t('admin.modelPricing.platform') }}</span>
              <select v-model="filters.platform_id" class="input h-9 min-w-0 py-1.5">
                <option value="">{{ t('admin.modelPricing.allPlatforms') }}</option>
                <option v-for="platform in platformOptions" :key="platform.id" :value="String(platform.id)">{{ platform.name }}</option>
              </select>
            </label>
            <label class="min-w-48 flex-1 text-sm sm:max-w-72">
              <span class="sr-only">{{ t('admin.modelPricing.searchPlaceholder') }}</span>
              <input v-model="filters.query" class="input h-9 min-w-0 py-1.5" :placeholder="t('admin.modelPricing.searchPlaceholder')" />
            </label>
            <button class="btn btn-secondary h-9 px-3 py-0" type="submit"><Icon name="search" size="sm" /><span>{{ t('admin.modelPricing.applyFilters') }}</span></button>
            <button v-if="filters.platform_id || filters.query" class="icon-button" type="button" :title="t('admin.modelPricing.clearFilters')" :aria-label="t('admin.modelPricing.clearFilters')" @click="clearFilters"><Icon name="x" size="sm" /></button>
          </form>
        </div>

        <div v-if="catalogError" data-testid="catalog-load-error" class="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          <span>{{ t('admin.modelPricing.loadFailed') }}</span><button class="btn btn-secondary" type="button" @click="loadCatalog">{{ t('common.refresh') }}</button>
        </div>
        <div v-if="catalogLoading" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</div>
        <div v-else-if="!catalogError && catalogRows.length === 0" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.noData') }}</div>

        <div v-else-if="!catalogError" class="mt-3 hidden min-w-0 overflow-x-auto rounded-md border border-gray-200 dark:border-dark-700 md:block">
          <table class="w-full min-w-[1180px] table-fixed border-collapse text-left text-sm">
            <colgroup><col class="w-[13%]" /><col class="w-[18%]" /><col class="w-[9%]" /><col class="w-[16%]" /><col class="w-[16%]" /><col class="w-[12%]" /><col class="w-[10%]" /><col class="w-[6%]" /></colgroup>
            <thead class="bg-gray-50 text-xs font-medium uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr><th class="px-3 py-2.5">{{ t('admin.modelPricing.platform') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.publicModel') }} / {{ t('admin.modelPricing.upstreamModel') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.billingMode') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.officialPrice') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.salePrice') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.priceDifference') }}</th><th class="px-3 py-2.5">{{ t('admin.modelPricing.saleStatus') }}</th><th class="px-3 py-2.5 text-right">{{ t('common.actions') }}</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
              <template v-for="row in catalogRows" :key="catalogKey(row)">
                <tr data-testid="catalog-row" class="align-top hover:bg-gray-50 dark:hover:bg-dark-800/70">
                  <td class="px-3 py-3"><div class="truncate font-medium" :title="row.platform_name">{{ row.platform_name }}</div><div class="truncate font-mono text-xs text-gray-500">{{ row.platform_code }}</div></td>
                  <td class="px-3 py-3"><div class="truncate font-mono" :title="row.model_pattern">{{ row.model_pattern }}</div><div class="mt-0.5 truncate font-mono text-xs text-gray-500" :title="row.upstream_model || row.model_pattern">{{ row.upstream_model || row.model_pattern }}</div></td>
                  <td class="px-3 py-3"><span class="status-chip">{{ modeLabel(row.billing_mode) }}</span></td>
                  <td class="px-3 py-3"><PriceLines :fields="primaryPriceFields(row.billing_mode)" :pricing="row.official_pricing" /><div class="mt-1 truncate text-xs text-gray-500" :title="row.official_source.source_name">{{ row.official_source.source_name || sourceTypeLabel(row.official_source.source_type) }}</div></td>
                  <td class="px-3 py-3"><PriceLines :fields="primaryPriceFields(row.billing_mode)" :pricing="row.sale_pricing" /></td>
                  <td class="px-3 py-3"><DifferenceLines :fields="primaryPriceFields(row.billing_mode)" :official="row.official_pricing" :sale="row.sale_pricing" /></td>
                  <td class="px-3 py-3"><span class="status-chip" :class="saleSourceClass(row.sale_source)">{{ saleSourceLabel(row.sale_source) }}</span></td>
                  <td class="px-3 py-3"><div class="flex justify-end gap-1"><button class="icon-button" type="button" :title="detailsTitle(row)" :aria-label="detailsTitle(row)" @click="toggleDetails(row)"><Icon :name="isExpanded(row) ? 'chevronUp' : 'chevronDown'" size="sm" /></button><button class="icon-button" type="button" :data-testid="`edit-sale-${row.platform_id}-${row.model_pattern}`" :title="t('admin.modelPricing.editSale')" :aria-label="t('admin.modelPricing.editSale')" @click="openSaleEditor(row)"><Icon name="edit" size="sm" /></button></div></td>
                </tr>
                <tr v-if="isExpanded(row)" class="bg-gray-50/70 dark:bg-dark-800/40"><td colspan="8" class="px-4 py-4"><CatalogDetails :row="row" /></td></tr>
              </template>
            </tbody>
          </table>
        </div>

        <div v-if="!catalogLoading && !catalogError && catalogRows.length" class="mt-3 divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700 md:hidden">
          <article v-for="row in catalogRows" :key="`mobile-${catalogKey(row)}`" class="min-w-0 py-4">
            <div class="mb-3 flex min-w-0 items-start justify-between gap-2"><div class="min-w-0"><div class="truncate font-medium">{{ row.platform_name }}</div><div class="truncate font-mono text-sm">{{ row.model_pattern }}</div></div><div class="flex shrink-0 gap-1"><button class="icon-button" type="button" :title="detailsTitle(row)" :aria-label="detailsTitle(row)" @click="toggleDetails(row)"><Icon :name="isExpanded(row) ? 'chevronUp' : 'chevronDown'" size="sm" /></button><button class="icon-button" type="button" :title="t('admin.modelPricing.editSale')" :aria-label="t('admin.modelPricing.editSale')" @click="openSaleEditor(row)"><Icon name="edit" size="sm" /></button></div></div>
            <dl class="grid min-w-0 grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
              <dt class="text-gray-500">{{ t('admin.modelPricing.upstreamModel') }}</dt><dd class="min-w-0 break-words font-mono">{{ row.upstream_model || row.model_pattern }}</dd>
              <dt class="text-gray-500">{{ t('admin.modelPricing.billingMode') }}</dt><dd>{{ modeLabel(row.billing_mode) }}</dd>
              <dt class="text-gray-500">{{ t('admin.modelPricing.officialPrice') }}</dt><dd><PriceLines :fields="primaryPriceFields(row.billing_mode)" :pricing="row.official_pricing" /></dd>
              <dt class="text-gray-500">{{ t('admin.modelPricing.salePrice') }}</dt><dd><PriceLines :fields="primaryPriceFields(row.billing_mode)" :pricing="row.sale_pricing" /></dd>
              <dt class="text-gray-500">{{ t('admin.modelPricing.priceDifference') }}</dt><dd><DifferenceLines :fields="primaryPriceFields(row.billing_mode)" :official="row.official_pricing" :sale="row.sale_pricing" /></dd>
              <dt class="text-gray-500">{{ t('admin.modelPricing.saleStatus') }}</dt><dd>{{ saleSourceLabel(row.sale_source) }}</dd>
            </dl>
            <div v-if="isExpanded(row)" class="mt-4 border-t border-gray-200 pt-4 dark:border-dark-700"><CatalogDetails :row="row" /></div>
          </article>
        </div>
      </section>

      <section class="min-w-0 border-t border-gray-200 pt-4 dark:border-dark-700" aria-labelledby="advanced-heading">
        <div class="flex items-start justify-between gap-3"><div class="min-w-0"><h2 id="advanced-heading" class="text-base font-semibold">{{ t('admin.modelPricing.advancedRules') }}</h2><p class="mt-1 text-sm text-gray-500">{{ t('admin.modelPricing.advancedRulesDescription') }}</p></div><button class="icon-button shrink-0" data-testid="advanced-toggle" type="button" :title="advancedOpen ? t('admin.modelPricing.hideAdvancedRules') : t('admin.modelPricing.showAdvancedRules')" :aria-label="advancedOpen ? t('admin.modelPricing.hideAdvancedRules') : t('admin.modelPricing.showAdvancedRules')" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen"><Icon :name="advancedOpen ? 'chevronUp' : 'chevronDown'" size="sm" /></button></div>
        <div v-if="advancedOpen" class="mt-4 min-w-0">
          <div class="mb-3 flex justify-end"><button class="btn btn-primary gap-1.5" data-testid="create-advanced-rule" type="button" @click="openAdvancedCreate"><Icon name="plus" size="sm" /><span>{{ t('admin.modelPricing.createRule') }}</span></button></div>
          <div v-if="rulesError" data-testid="rules-load-error" class="flex flex-wrap items-center justify-between gap-3 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700"><span>{{ t('admin.modelPricing.rulesLoadFailed') }}</span><button class="btn btn-secondary" type="button" @click="loadRules">{{ t('common.refresh') }}</button></div>
          <div v-else-if="rulesLoading" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
          <div v-else-if="rules.length === 0" class="py-8 text-center text-sm text-gray-500">{{ t('admin.modelPricing.noAdvancedRules') }}</div>
          <div v-else class="hidden overflow-x-auto rounded-md border border-gray-200 dark:border-dark-700 md:block"><table class="w-full min-w-[760px] table-fixed text-left text-sm"><thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800"><tr><th class="w-[20%] px-3 py-2.5">{{ t('admin.modelPricing.adapter') }}</th><th class="w-[28%] px-3 py-2.5">{{ t('admin.modelPricing.modelPattern') }}</th><th class="w-[16%] px-3 py-2.5">{{ t('admin.modelPricing.billingMode') }}</th><th class="w-[18%] px-3 py-2.5">{{ t('admin.modelPricing.status') }}</th><th class="w-[18%] px-3 py-2.5 text-right">{{ t('common.actions') }}</th></tr></thead><tbody class="divide-y divide-gray-200 dark:divide-dark-700"><tr v-for="rule in rules" :key="rule.id" data-testid="advanced-rule-row"><td class="truncate px-3 py-2.5 font-mono">{{ rule.adapter }}</td><td class="truncate px-3 py-2.5 font-mono">{{ rule.model_pattern }}</td><td class="px-3 py-2.5">{{ modeLabel(rule.billing_mode) }}</td><td class="px-3 py-2.5">{{ rule.status === 'active' ? t('admin.modelPricing.active') : t('admin.modelPricing.disabled') }}</td><td class="px-3 py-2.5"><div class="flex justify-end gap-1"><button class="icon-button" type="button" :title="t('common.edit')" :aria-label="t('common.edit')" @click="openAdvancedEdit(rule)"><Icon name="edit" size="sm" /></button><button class="icon-button text-red-600" type="button" :title="t('common.delete')" :aria-label="t('common.delete')" @click="removeRule(rule)"><Icon name="trash" size="sm" /></button></div></td></tr></tbody></table></div>
          <div v-if="rules.length" class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700 md:hidden"><article v-for="rule in rules" :key="`mobile-rule-${rule.id}`" class="py-3"><div class="flex items-start justify-between gap-2"><div class="min-w-0"><div class="truncate font-mono text-sm">{{ rule.model_pattern }}</div><div class="truncate font-mono text-xs text-gray-500">{{ rule.adapter }}</div></div><div class="flex shrink-0 gap-1"><button class="icon-button" type="button" :title="t('common.edit')" :aria-label="t('common.edit')" @click="openAdvancedEdit(rule)"><Icon name="edit" size="sm" /></button><button class="icon-button text-red-600" type="button" :title="t('common.delete')" :aria-label="t('common.delete')" @click="removeRule(rule)"><Icon name="trash" size="sm" /></button></div></div><dl class="mt-2 grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm"><dt class="text-gray-500">{{ t('admin.modelPricing.billingMode') }}</dt><dd>{{ modeLabel(rule.billing_mode) }}</dd><dt class="text-gray-500">{{ t('admin.modelPricing.status') }}</dt><dd>{{ rule.status === 'active' ? t('admin.modelPricing.active') : t('admin.modelPricing.disabled') }}</dd></dl></article></div>
        </div>
      </section>
    </div>

    <div v-if="saleEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-3" role="dialog" aria-modal="true" :aria-label="t('admin.modelPricing.editSale')">
      <div data-testid="sale-editor" class="max-h-[calc(100vh-1.5rem)] w-full max-w-4xl overflow-y-auto rounded-lg border border-gray-200 bg-white p-4 shadow-xl dark:border-dark-600 dark:bg-dark-800 sm:p-5">
        <div class="flex items-center justify-between gap-3"><h2 class="text-lg font-semibold">{{ t('admin.modelPricing.editSale') }}</h2><button class="icon-button" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="saleEditorOpen = false"><Icon name="x" size="sm" /></button></div>
        <dl class="mt-4 grid min-w-0 grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-2 border-y border-gray-200 py-3 text-sm dark:border-dark-700 sm:grid-cols-[7rem_minmax(0,1fr)_7rem_minmax(0,1fr)]"><dt class="text-gray-500">{{ t('admin.modelPricing.platform') }}</dt><dd class="min-w-0 break-words">{{ editingCatalogRow?.platform_name }}</dd><dt class="text-gray-500">{{ t('admin.modelPricing.modelPattern') }}</dt><dd class="min-w-0 break-words font-mono">{{ saleDraft.model_pattern }}</dd></dl>
        <div class="mt-4 min-w-0 space-y-4">
          <fieldset v-for="field in editablePriceFields(saleDraft.billing_mode)" :key="field.key" class="m-0 min-w-0 border-0 border-b border-gray-200 p-0 pb-4 text-sm last:border-b-0 last:pb-0 dark:border-dark-700" :data-price-field="field.key">
            <legend class="input-label break-words">{{ t(field.labelKey) }}</legend>
            <div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
              <div :data-testid="`official-price-${field.key}`" class="min-w-0 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-900">
                <div class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.officialPrice') }}</div>
                <div class="mt-1 break-words font-mono text-sm tabular-nums">{{ displayPrice(editingCatalogRow?.official_pricing?.[field.key], field.scale) }}</div>
                <div class="mt-0.5 break-words text-xs text-gray-500 dark:text-gray-400">{{ t(field.unitKey) }}</div>
              </div>
              <div class="min-w-0">
                <div class="mb-1 text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.salePrice') }}</div>
                <PricingValueEditor :model-value="saleDraft[field.key]" :scale="field.scale" :unit-label="t(field.unitKey)" :labels="priceEditorLabels(field.labelKey)" @update:model-value="saleDraft[field.key] = $event" @validity-change="saleFieldValidity[field.key] = $event" />
              </div>
            </div>
          </fieldset>
        </div>
        <div class="mt-5"><PricingIntervalsEditor v-model="saleDraft.intervals" v-model:valid="saleIntervalsValid" :billing-mode="saleDraft.billing_mode" :labels="intervalEditorLabels" /></div>
        <p v-if="!saleCanSave" class="mt-3 text-sm text-red-600">{{ t('admin.modelPricing.invalidIntervals') }}</p>
        <div class="mt-5 flex justify-end gap-2"><button class="btn btn-secondary" type="button" @click="saleEditorOpen = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" data-testid="sale-save" type="button" :disabled="savingSale || !saleCanSave" @click="saveSale">{{ savingSale ? t('common.saving') : t('common.save') }}</button></div>
      </div>
    </div>

    <div v-if="advancedEditorOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-3" role="dialog" aria-modal="true" :aria-label="editingRule ? t('admin.modelPricing.edit') : t('admin.modelPricing.createRule')">
      <div data-testid="advanced-editor" class="max-h-[calc(100vh-1.5rem)] w-full max-w-4xl overflow-y-auto rounded-lg border border-gray-200 bg-white p-4 shadow-xl dark:border-dark-600 dark:bg-dark-800 sm:p-5">
        <div class="flex items-center justify-between gap-3"><h2 class="text-lg font-semibold">{{ editingRule ? t('admin.modelPricing.edit') : t('admin.modelPricing.createRule') }}</h2><button class="icon-button" type="button" :title="t('common.close')" :aria-label="t('common.close')" @click="advancedEditorOpen = false"><Icon name="x" size="sm" /></button></div>
        <div class="mt-4 grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2"><label class="text-sm"><span class="input-label">{{ t('admin.modelPricing.adapter') }}</span><input v-model="advancedDraft.adapter" class="input min-w-0 font-mono" maxlength="50" /></label><label class="text-sm"><span class="input-label">{{ t('admin.modelPricing.modelPattern') }}</span><input v-model="advancedDraft.model_pattern" class="input min-w-0 font-mono" maxlength="100" /></label><label class="text-sm"><span class="input-label">{{ t('admin.modelPricing.billingMode') }}</span><select v-model="advancedDraft.billing_mode" class="input min-w-0"><option value="token">{{ t('admin.modelPricing.token') }}</option><option value="per_request">{{ t('admin.modelPricing.perRequest') }}</option><option value="image">{{ t('admin.modelPricing.image') }}</option></select></label><label class="text-sm"><span class="input-label">{{ t('admin.modelPricing.status') }}</span><select v-model="advancedDraft.status" class="input min-w-0"><option value="active">{{ t('admin.modelPricing.active') }}</option><option value="disabled">{{ t('admin.modelPricing.disabled') }}</option></select></label></div>
        <div class="mt-4 grid min-w-0 grid-cols-1 gap-4 lg:grid-cols-2"><fieldset v-for="field in editablePriceFields(advancedDraft.billing_mode)" :key="field.key" class="m-0 min-w-0 border-0 p-0 text-sm" :data-price-field="field.key"><legend class="input-label break-words">{{ t(field.labelKey) }}</legend><PricingValueEditor :model-value="advancedDraft[field.key] ?? null" :scale="field.scale" :unit-label="t(field.unitKey)" :labels="priceEditorLabels(field.labelKey)" @update:model-value="advancedDraft[field.key] = $event" @validity-change="advancedFieldValidity[field.key] = $event" /></fieldset></div>
        <div class="mt-5"><PricingIntervalsEditor v-model="advancedDraft.intervals" v-model:valid="advancedIntervalsValid" :billing-mode="advancedDraft.billing_mode" :labels="intervalEditorLabels" /></div>
        <p v-if="advancedFormError" class="mt-3 text-sm text-red-600">{{ advancedFormError }}</p>
        <div class="mt-5 flex justify-end gap-2"><button class="btn btn-secondary" type="button" @click="advancedEditorOpen = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="button" :disabled="savingRule || !advancedCanSave" @click="saveRule">{{ savingRule ? t('common.saving') : t('common.save') }}</button></div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { ModelPricingBillingMode, ModelPricingCatalogQuery, ModelPricingCatalogRow, ModelPricingInterval, ModelPricingOfficialSourceType, ModelPricingOverride, ModelPricingOverrideInput, ModelPricingSaleSource, ModelPricingValues, PlatformSalePricingInput } from '@/api/admin/modelPricing'
import PricingIntervalsEditor from '@/components/admin/modelPricing/PricingIntervalsEditor.vue'
import PricingValueEditor from '@/components/admin/modelPricing/PricingValueEditor.vue'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatScaled, TOKEN_PRICE_SCALE } from '@/utils/pricing'

const { t } = useI18n()
const appStore = useAppStore()
type PriceKey = 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price' | 'image_input_price' | 'image_output_price' | 'per_request_price'
interface PriceField { key: PriceKey; labelKey: string; scale: number; unitKey: string }
interface PricingDraft extends Record<PriceKey, number | null> { billing_mode: ModelPricingBillingMode; intervals: ModelPricingInterval[]; status: ModelPricingOverride['status'] }

const tokenFields: PriceField[] = [
  { key: 'input_price', labelKey: 'admin.modelPricing.inputPrice', scale: TOKEN_PRICE_SCALE, unitKey: 'admin.modelPricing.tokenUnit' },
  { key: 'output_price', labelKey: 'admin.modelPricing.outputPrice', scale: TOKEN_PRICE_SCALE, unitKey: 'admin.modelPricing.tokenUnit' },
  { key: 'cache_write_price', labelKey: 'admin.modelPricing.cacheWritePrice', scale: TOKEN_PRICE_SCALE, unitKey: 'admin.modelPricing.tokenUnit' },
  { key: 'cache_read_price', labelKey: 'admin.modelPricing.cacheReadPrice', scale: TOKEN_PRICE_SCALE, unitKey: 'admin.modelPricing.tokenUnit' },
]
const imageFields: PriceField[] = [
  { key: 'image_input_price', labelKey: 'admin.modelPricing.imageInputPrice', scale: 1, unitKey: 'admin.modelPricing.imageUnit' },
  { key: 'image_output_price', labelKey: 'admin.modelPricing.imageOutputPrice', scale: 1, unitKey: 'admin.modelPricing.imageUnit' },
  { key: 'per_request_price', labelKey: 'admin.modelPricing.perRequestPrice', scale: 1, unitKey: 'admin.modelPricing.requestUnit' },
]
const requestFields: PriceField[] = [{ key: 'per_request_price', labelKey: 'admin.modelPricing.perRequestPrice', scale: 1, unitKey: 'admin.modelPricing.requestUnit' }]
const allPriceKeys: PriceKey[] = ['input_price', 'output_price', 'cache_write_price', 'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price']
function primaryPriceFields(mode: ModelPricingBillingMode) { return mode === 'token' ? tokenFields.slice(0, 2) : mode === 'image' ? imageFields.slice(0, 2) : requestFields }
function editablePriceFields(mode: ModelPricingBillingMode) { return mode === 'token' ? tokenFields : mode === 'image' ? imageFields : requestFields }
function displayPrice(value: number | null | undefined, scale: number) { return value == null ? t('admin.modelPricing.noPrice') : formatScaled(value, scale) }
function displayDifference(official: number | null | undefined, sale: number | null | undefined, scale: number) { if (official == null || sale == null) return t('admin.modelPricing.notAvailable'); const value = sale - official; if (value === 0) return '$0'; return `${value > 0 ? '+' : '-'}${formatScaled(Math.abs(value), scale)}` }

const PriceLines = defineComponent({ props: { fields: { type: Array as PropType<PriceField[]>, required: true }, pricing: { type: Object as PropType<ModelPricingValues | null>, default: null } }, setup(props) { return () => h('div', { class: 'space-y-0.5 tabular-nums' }, props.fields.map(field => h('div', { class: 'flex min-w-0 justify-between gap-2 text-xs' }, [h('span', { class: 'truncate text-gray-500', title: t(field.labelKey) }, t(field.labelKey)), h('span', { class: 'shrink-0 font-mono' }, displayPrice(props.pricing?.[field.key], field.scale))]))) } })
const DifferenceLines = defineComponent({ props: { fields: { type: Array as PropType<PriceField[]>, required: true }, official: { type: Object as PropType<ModelPricingValues | null>, default: null }, sale: { type: Object as PropType<ModelPricingValues | null>, default: null } }, setup(props) { return () => h('div', { class: 'space-y-0.5 tabular-nums' }, props.fields.map(field => { const official = props.official?.[field.key]; const sale = props.sale?.[field.key]; const delta = official != null && sale != null ? sale - official : null; return h('div', { class: 'flex min-w-0 justify-between gap-2 text-xs' }, [h('span', { class: 'truncate text-gray-500' }, t(field.labelKey)), h('span', { class: delta == null || delta === 0 ? 'shrink-0 font-mono text-gray-500' : delta > 0 ? 'shrink-0 font-mono text-amber-600' : 'shrink-0 font-mono text-emerald-600' }, displayDifference(official, sale, field.scale))]) })) } })

function sourceTypeLabel(type: ModelPricingOfficialSourceType) { if (!type) return t('admin.modelPricing.sourceTypeUnavailable'); const suffix = type.split('_').map(part => part[0].toUpperCase() + part.slice(1)).join(''); return t(`admin.modelPricing.sourceType${suffix}`) }
function formatDate(value?: string | null) { if (!value) return t('admin.modelPricing.notAvailable'); const date = new Date(value); return Number.isNaN(date.getTime()) ? t('admin.modelPricing.notAvailable') : date.toLocaleString() }
function intervalPriceFields(mode: ModelPricingBillingMode) { return mode === 'token' ? tokenFields : requestFields }
function intervalFieldValue(interval: ModelPricingInterval, key: PriceKey) {
  if (key === 'input_price') return interval.input_price
  if (key === 'output_price') return interval.output_price
  if (key === 'cache_write_price') return interval.cache_write_price
  if (key === 'cache_read_price') return interval.cache_read_price
  if (key === 'per_request_price') return interval.per_request_price
  return null
}
const CatalogDetails = defineComponent({ props: { row: { type: Object as PropType<ModelPricingCatalogRow>, required: true } }, setup(props) { return () => h('div', { class: 'grid min-w-0 grid-cols-1 gap-5 lg:grid-cols-3' }, [
  h('div', [h('h3', { class: 'text-sm font-semibold' }, t('admin.modelPricing.details')), h('dl', { class: 'mt-2 grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm' }, editablePriceFields(props.row.billing_mode).flatMap(field => [h('dt', { class: 'text-gray-500' }, t(field.labelKey)), h('dd', { class: 'min-w-0 space-y-0.5 break-words font-mono text-xs' }, [h('div', `${t('admin.modelPricing.officialPrice')}: ${displayPrice(props.row.official_pricing?.[field.key], field.scale)}`), h('div', `${t('admin.modelPricing.salePrice')}: ${displayPrice(props.row.sale_pricing?.[field.key], field.scale)}`), h('div', { class: 'text-gray-500' }, t(field.unitKey))])]))]),
  h('div', [h('h3', { class: 'text-sm font-semibold' }, t('admin.modelPricing.officialSource')), h('dl', { class: 'mt-2 grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm' }, [h('dt', { class: 'text-gray-500' }, t('admin.modelPricing.sourceType')), h('dd', sourceTypeLabel(props.row.official_source.source_type)), h('dt', { class: 'text-gray-500' }, t('admin.modelPricing.sourceName')), h('dd', { class: 'break-words' }, props.row.official_source.source_url ? [h('a', { class: 'text-primary-600 hover:underline', href: props.row.official_source.source_url, target: '_blank', rel: 'noopener noreferrer' }, props.row.official_source.source_name || props.row.official_source.source_url)] : props.row.official_source.source_name || t('admin.modelPricing.notAvailable')), h('dt', { class: 'text-gray-500' }, t('admin.modelPricing.matchedModel')), h('dd', { class: 'break-words font-mono' }, props.row.official_source.matched_model || t('admin.modelPricing.notAvailable')), h('dt', { class: 'text-gray-500' }, t('admin.modelPricing.updatedAt')), h('dd', formatDate(props.row.official_source.updated_at))])]),
  h('div', [h('h3', { class: 'text-sm font-semibold' }, t('admin.modelPricing.matchedRule')), props.row.override ? h('p', { class: 'mt-2 break-words font-mono text-sm' }, `${props.row.override.adapter} / ${props.row.override.model_pattern}`) : h('p', { class: 'mt-2 text-sm text-gray-500' }, t('admin.modelPricing.notAvailable')), h('h3', { class: 'mt-4 text-sm font-semibold' }, t('admin.modelPricing.intervals')), props.row.intervals.length ? h('div', { class: 'mt-2 space-y-2 text-xs' }, props.row.intervals.map((interval, index) => h('div', { class: 'border-b border-gray-200 pb-2 last:border-0 last:pb-0', 'data-testid': `catalog-interval-${index}` }, [h('div', { class: 'mb-1 font-medium' }, `${interval.tier_label || `${interval.min_tokens}+`}: ${interval.min_tokens}-${interval.max_tokens ?? '∞'}`), h('dl', { class: 'grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)] gap-x-2 gap-y-1' }, intervalPriceFields(props.row.billing_mode).flatMap(field => [h('dt', { class: 'truncate text-gray-500', title: t(field.labelKey) }, t(field.labelKey)), h('dd', { class: 'break-words font-mono tabular-nums', 'data-testid': `interval-price-${field.key}` }, `${displayPrice(intervalFieldValue(interval, field.key), field.scale)} ${t(field.unitKey)}`)]))]))) : h('p', { class: 'mt-2 text-sm text-gray-500' }, t('admin.modelPricing.noIntervals'))]),
]) } })

const catalogRows = ref<ModelPricingCatalogRow[]>([]); const catalogLoading = ref(false); const catalogError = ref(false)
const rules = ref<ModelPricingOverride[]>([]); const rulesLoading = ref(false); const rulesError = ref(false); const advancedOpen = ref(false)
const filters = reactive({ platform_id: '', query: '' }); const knownPlatforms = ref<Array<{ id: number; name: string }>>([]); const platformOptions = computed(() => knownPlatforms.value); const expandedRows = reactive(new Set<string>())
async function loadCatalog() { catalogLoading.value = true; catalogError.value = false; const query: ModelPricingCatalogQuery = {}; if (filters.platform_id) query.platform_id = Number(filters.platform_id); if (filters.query.trim()) query.query = filters.query.trim(); try { const rows = await adminAPI.modelPricing.catalog(query); catalogRows.value = rows; if (!query.platform_id && !query.query) { const platforms = new Map<number, string>(); rows.forEach(row => platforms.set(row.platform_id, row.platform_name)); knownPlatforms.value = [...platforms].map(([id, name]) => ({ id, name })) } } catch (error) { catalogRows.value = []; catalogError.value = true; appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.loadFailed'))) } finally { catalogLoading.value = false } }
async function loadRules() { rulesLoading.value = true; rulesError.value = false; try { rules.value = await adminAPI.modelPricing.list() } catch (error) { rules.value = []; rulesError.value = true; appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.rulesLoadFailed'))) } finally { rulesLoading.value = false } }
async function loadAll() { await Promise.all([loadCatalog(), loadRules()]) }
function clearFilters() { filters.platform_id = ''; filters.query = ''; void loadCatalog() }
function catalogKey(row: ModelPricingCatalogRow) { return `${row.platform_id}:${row.model_pattern}` }
function isExpanded(row: ModelPricingCatalogRow) { return expandedRows.has(catalogKey(row)) }
function toggleDetails(row: ModelPricingCatalogRow) { const key = catalogKey(row); expandedRows.has(key) ? expandedRows.delete(key) : expandedRows.add(key) }
function detailsTitle(row: ModelPricingCatalogRow) { return isExpanded(row) ? t('admin.modelPricing.hideDetails') : t('admin.modelPricing.showDetails') }
function modeLabel(mode: ModelPricingBillingMode) { return mode === 'per_request' ? t('admin.modelPricing.perRequest') : mode === 'image' ? t('admin.modelPricing.image') : t('admin.modelPricing.token') }
function saleSourceLabel(source: ModelPricingSaleSource) { return t(`admin.modelPricing.${source}`) }
function saleSourceClass(source: ModelPricingSaleSource) { return source === 'custom' ? 'status-chip--custom' : source === 'official' ? 'status-chip--official' : 'status-chip--unavailable' }

function emptyPricing(mode: ModelPricingBillingMode = 'token'): PricingDraft { return { billing_mode: mode, status: 'active', intervals: [], input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null } }
function pricingFromOverride(item: ModelPricingOverride | null, mode: ModelPricingBillingMode) { const draft = emptyPricing(mode); if (!item) return draft; draft.status = item.status; draft.intervals = item.intervals.map(interval => ({ ...interval })); allPriceKeys.forEach(key => { draft[key] = item[key] ?? null }); return draft }
function resetValidity(target: Record<PriceKey, boolean>) { allPriceKeys.forEach(key => { target[key] = true }) }
function priceEditorLabels(labelKey: string) { return { modeGroup: t('admin.modelPricing.priceBehavior'), inherit: t('admin.modelPricing.inherit'), custom: t('admin.modelPricing.custom'), zero: t('admin.modelPricing.zero'), inputLabel: t(labelKey), required: t('admin.modelPricing.customPriceRequired'), invalid: t('admin.modelPricing.invalidPrice') } }
const intervalEditorLabels = computed(() => ({ title: t('admin.modelPricing.intervals'), interval: t('admin.modelPricing.interval'), add: t('admin.modelPricing.addInterval'), delete: t('admin.modelPricing.deleteInterval'), sort: t('admin.modelPricing.sortIntervals'), empty: t('admin.modelPricing.noIntervals'), minTokens: t('admin.modelPricing.minTokens'), maxTokens: t('admin.modelPricing.maxTokens'), tierLabel: t('admin.modelPricing.tierLabel'), unbounded: t('admin.modelPricing.unbounded'), inputPrice: t('admin.modelPricing.inputPrice'), outputPrice: t('admin.modelPricing.outputPrice'), cacheWritePrice: t('admin.modelPricing.cacheWritePrice'), cacheReadPrice: t('admin.modelPricing.cacheReadPrice'), perRequestPrice: t('admin.modelPricing.perRequestPrice'), invalidPrice: t('admin.modelPricing.invalidPrice') }))
const blankValidity = (): Record<PriceKey, boolean> => ({ input_price: true, output_price: true, cache_write_price: true, cache_read_price: true, image_input_price: true, image_output_price: true, per_request_price: true })

const saleEditorOpen = ref(false); const savingSale = ref(false); const editingCatalogRow = ref<ModelPricingCatalogRow | null>(null)
const saleDraft = reactive<PricingDraft & { platform_id: number; model_pattern: string }>({ ...emptyPricing(), platform_id: 0, model_pattern: '' }); const saleIntervalsValid = ref(true); const saleFieldValidity = reactive(blankValidity()); const saleCanSave = computed(() => saleIntervalsValid.value && editablePriceFields(saleDraft.billing_mode).every(field => saleFieldValidity[field.key]))
function openSaleEditor(row: ModelPricingCatalogRow) { editingCatalogRow.value = row; Object.assign(saleDraft, pricingFromOverride(row.override, row.billing_mode), { platform_id: row.platform_id, model_pattern: row.model_pattern }); resetValidity(saleFieldValidity); saleIntervalsValid.value = true; saleEditorOpen.value = true }
async function saveSale() { if (!saleCanSave.value) return; const payload: PlatformSalePricingInput = { platform_id: saleDraft.platform_id, model_pattern: saleDraft.model_pattern, billing_mode: saleDraft.billing_mode, status: saleDraft.status, intervals: saleDraft.intervals.map(interval => ({ ...interval })) }; allPriceKeys.forEach(key => { payload[key] = saleDraft[key] }); savingSale.value = true; try { await adminAPI.modelPricing.upsertPlatformSale(payload); appStore.showSuccess(t('admin.modelPricing.saved')); saleEditorOpen.value = false; await loadCatalog() } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.saveFailed'))) } finally { savingSale.value = false } }

const advancedEditorOpen = ref(false); const savingRule = ref(false); const editingRule = ref<ModelPricingOverride | null>(null); const advancedDraft = reactive<ModelPricingOverrideInput>({ adapter: '', model_pattern: '', ...emptyPricing() }); const advancedIntervalsValid = ref(true); const advancedFormError = ref(''); const advancedFieldValidity = reactive(blankValidity()); const advancedCanSave = computed(() => advancedIntervalsValid.value && editablePriceFields(advancedDraft.billing_mode).every(field => advancedFieldValidity[field.key]))
function resetAdvancedDraft(item?: ModelPricingOverride) { Object.assign(advancedDraft, pricingFromOverride(item ?? null, item?.billing_mode ?? 'token'), { adapter: item?.adapter ?? '', model_pattern: item?.model_pattern ?? '' }); resetValidity(advancedFieldValidity); advancedIntervalsValid.value = true; advancedFormError.value = '' }
function openAdvancedCreate() { editingRule.value = null; resetAdvancedDraft(); advancedEditorOpen.value = true }
function openAdvancedEdit(item: ModelPricingOverride) { editingRule.value = item; resetAdvancedDraft(item); advancedEditorOpen.value = true }
async function saveRule() { if (!advancedCanSave.value) { advancedFormError.value = t('admin.modelPricing.invalidIntervals'); return } if (!advancedDraft.adapter.trim() || !advancedDraft.model_pattern.trim()) { advancedFormError.value = t('admin.modelPricing.required'); return } const payload: ModelPricingOverrideInput = { ...advancedDraft, adapter: advancedDraft.adapter.trim(), model_pattern: advancedDraft.model_pattern.trim(), intervals: advancedDraft.intervals.map(interval => ({ ...interval })) }; savingRule.value = true; try { if (editingRule.value) await adminAPI.modelPricing.update(editingRule.value.id, payload); else await adminAPI.modelPricing.create(payload); appStore.showSuccess(t('admin.modelPricing.saved')); advancedEditorOpen.value = false; await Promise.all([loadRules(), loadCatalog()]) } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.saveFailed'))) } finally { savingRule.value = false } }
async function removeRule(item: ModelPricingOverride) { if (!window.confirm(t('admin.modelPricing.confirmDelete'))) return; try { await adminAPI.modelPricing.remove(item.id); appStore.showSuccess(t('admin.modelPricing.deleted')); await Promise.all([loadRules(), loadCatalog()]) } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.deleteFailed'))) } }
onMounted(loadAll)
</script>

<style scoped>
.status-chip { display: inline-flex; max-width: 100%; overflow: hidden; border: 1px solid rgb(229 231 235); border-radius: 4px; background: rgb(249 250 251); padding: 0.125rem 0.375rem; font-size: 0.75rem; line-height: 1rem; color: rgb(75 85 99); text-overflow: ellipsis; white-space: nowrap; }
.status-chip--custom { border-color: rgb(253 230 138); background: rgb(255 251 235); color: rgb(180 83 9); }
.status-chip--official { border-color: rgb(167 243 208); background: rgb(236 253 245); color: rgb(4 120 87); }
.status-chip--unavailable { color: rgb(107 114 128); }
:global(.dark) .status-chip { border-color: rgb(75 85 99); background: rgb(31 41 55); color: rgb(209 213 219); }
:global(.dark) .status-chip--custom { border-color: rgb(120 53 15); background: rgb(69 26 3 / 0.3); color: rgb(252 211 77); }
:global(.dark) .status-chip--official { border-color: rgb(6 78 59); background: rgb(2 44 34 / 0.3); color: rgb(110 231 183); }
</style>
