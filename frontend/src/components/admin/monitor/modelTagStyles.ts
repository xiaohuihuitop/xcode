export function getPlatformTagClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    case 'openai': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'gemini': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
    case 'antigravity': return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    case 'grok': return 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
    default: return 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400'
  }
}

export function getPlatformTextClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'text-orange-700 dark:text-orange-400'
    case 'openai': return 'text-emerald-700 dark:text-emerald-400'
    case 'gemini': return 'text-blue-700 dark:text-blue-400'
    case 'antigravity': return 'text-purple-700 dark:text-purple-400'
    case 'grok': return 'text-slate-700 dark:text-slate-300'
    default: return ''
  }
}
