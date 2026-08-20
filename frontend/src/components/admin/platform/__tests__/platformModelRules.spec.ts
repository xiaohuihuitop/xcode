import { describe, expect, it } from 'vitest'
import { buildPlatformModelRules, resolvePlatformModelPreset, splitPlatformModelRules } from '../platformModelRules'

describe('platformModelRules', () => {
  it('splits self mappings from explicit mappings', () => {
    expect(splitPlatformModelRules([
      { model_pattern: 'gpt-5.6', upstream_model: 'gpt-5.6', enabled: true },
      { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
    ])).toEqual({
      allowedModels: ['gpt-5.6'],
      mappings: [{ from: 'gpt-latest', to: 'gpt-5.6' }],
      disabledRules: [],
    })
  })

  it('lets an explicit mapping replace a same-name self mapping', () => {
    expect(buildPlatformModelRules(
      ['gpt-latest'],
      [{ from: 'gpt-latest', to: 'gpt-5.6' }],
    )).toEqual([
      { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
    ])
  })

  it('keeps allowed wildcard models on the requested upstream model', () => {
    expect(buildPlatformModelRules(['glm*'], [])).toEqual([
      { model_pattern: 'glm*', upstream_model: '', enabled: true },
    ])
  })

  it('preserves disabled rules when an existing platform is edited', () => {
    const split = splitPlatformModelRules([
      { id: 9, model_pattern: 'legacy-*', upstream_model: '', enabled: false },
    ])

    expect(split.disabledRules).toEqual([
      { id: 9, model_pattern: 'legacy-*', upstream_model: '', enabled: false },
    ])
    expect(buildPlatformModelRules(split.allowedModels, split.mappings, split.disabledRules)).toEqual([
      { id: 9, model_pattern: 'legacy-*', upstream_model: '', enabled: false },
    ])
  })

  it('uses GLM presets for an OpenAI-compatible GLM platform', () => {
    expect(resolvePlatformModelPreset('glm-primary', 'openai')).toBe('glm')
  })
})
