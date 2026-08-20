import type { AccountPlatform, PlatformModelRule } from '@/types'
import { getModelsByPlatform, isValidWildcardPattern, type ModelMappingEntry } from '@/composables/useModelWhitelist'

export type PlatformModelMapping = ModelMappingEntry

export function splitPlatformModelRules(rules: PlatformModelRule[]): {
  allowedModels: string[]
  mappings: PlatformModelMapping[]
  disabledRules: PlatformModelRule[]
} {
  const allowed = new Set<string>()
  const mappingBySource = new Map<string, string>()
  const disabledRules: PlatformModelRule[] = []
  for (const rule of rules) {
    const from = rule.model_pattern.trim()
    const to = rule.upstream_model.trim()
    if (!from) continue
    if (!rule.enabled) {
      disabledRules.push({ ...rule, model_pattern: from, upstream_model: to })
      continue
    }
    if (!to || from === to) {
      allowed.add(from)
      continue
    }
    mappingBySource.set(from, to)
  }

  for (const source of mappingBySource.keys()) allowed.delete(source)
  return {
    allowedModels: [...allowed].sort(),
    mappings: [...mappingBySource.entries()]
      .map(([from, to]) => ({ from, to }))
      .sort((left, right) => left.from.localeCompare(right.from)),
    disabledRules,
  }
}

export function buildPlatformModelRules(
  allowedModels: string[],
  mappings: PlatformModelMapping[],
  disabledRules: PlatformModelRule[] = [],
): PlatformModelRule[] {
  const rules = new Map<string, PlatformModelRule>()
  for (const rawModel of allowedModels) {
    const model = rawModel.trim()
    if (!model || !isValidWildcardPattern(model)) continue
    rules.set(model, { model_pattern: model, upstream_model: '', enabled: true })
  }

  for (const mapping of mappings) {
    const from = mapping.from.trim()
    const to = mapping.to.trim()
    if (!from || !to || !isValidWildcardPattern(from) || to.includes('*')) continue
    rules.set(from, { model_pattern: from, upstream_model: to, enabled: true })
  }

  for (const rule of disabledRules) {
    const model = rule.model_pattern.trim()
    if (!model || !isValidWildcardPattern(model) || rules.has(model)) continue
    rules.set(model, {
      id: rule.id,
      model_pattern: model,
      upstream_model: rule.upstream_model.trim(),
      enabled: false,
    })
  }

  return [...rules.values()].sort((left, right) => left.model_pattern.localeCompare(right.model_pattern))
}

export function resolvePlatformModelPreset(code: string, accountPlatform: AccountPlatform): string {
  const normalizedCode = code.trim().toLowerCase()
  if (normalizedCode.startsWith('glm') || normalizedCode.startsWith('zhipu')) return 'glm'
  if (normalizedCode.startsWith('grok') || normalizedCode.startsWith('xai')) return 'grok'
  return accountPlatform
}

export { getModelsByPlatform, isValidWildcardPattern }
