const storageAvailable = () => typeof window !== 'undefined' && !!window.localStorage

export const isValidDateRange = (start: unknown, end: unknown): start is string => {
  if (typeof start !== 'string' || typeof end !== 'string') return false
  return !Number.isNaN(new Date(`${start}T00:00:00`).getTime()) &&
    !Number.isNaN(new Date(`${end}T00:00:00`).getTime())
}

export const readPersistedViewState = <T>(
  key: string,
  fallback: T,
  isValid: (value: unknown) => value is T,
): T => {
  if (!storageAvailable()) return fallback
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return fallback
    const value: unknown = JSON.parse(raw)
    return isValid(value) ? value : fallback
  } catch {
    return fallback
  }
}

export const writePersistedViewState = <T>(key: string, value: T) => {
  if (!storageAvailable()) return
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Ignore unavailable or quota-limited browser storage.
  }
}
