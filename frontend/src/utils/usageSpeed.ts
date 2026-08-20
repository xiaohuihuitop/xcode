export function formatOutputSpeed(
  outputTokens?: number | null,
  durationMs?: number | null,
): string {
  if (!outputTokens || !durationMs || durationMs <= 0) return '-'
  return `${(outputTokens / (durationMs / 1000)).toFixed(1)} t/s`
}
