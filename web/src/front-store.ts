// The last successful front page is kept per account and period so a revisit
// (or an offline open) starts with the page the reader last saw rather than a
// skeleton. Keys are namespaced by user id; logout clears the whole namespace.
import type {FrontPage, FrontPeriod} from './front'
import {normalizeFrontPage} from './front'

export type StoredFrontPage = {page: FrontPage; updated_at: string}
const PREFIX = 'earnest.front.'

export function frontCacheKey(userID: number, period: FrontPeriod): string {
  return `${PREFIX}${userID}.${period}`
}

export function loadFrontPage(userID: number, period: FrontPeriod): StoredFrontPage | undefined {
  try {
    const raw = localStorage.getItem(frontCacheKey(userID, period))
    if (!raw) return undefined
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object') return undefined
    const record = value as Record<string, unknown>
    const page = normalizeFrontPage(record.page)
    if (!page || typeof record.updated_at !== 'string') return undefined
    return {page, updated_at: record.updated_at}
  } catch {
    return undefined
  }
}

export function saveFrontPage(userID: number, period: FrontPeriod, page: FrontPage, updatedAt: string = new Date().toISOString()): void {
  try {
    localStorage.setItem(frontCacheKey(userID, period), JSON.stringify({page, updated_at: updatedAt}))
  } catch { /* Storage can be full or disabled; the page still renders. */ }
}

export function clearFrontCache(): void {
  try {
    const stale: string[] = []
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index)
      if (key?.startsWith(PREFIX)) stale.push(key)
    }
    for (const key of stale) localStorage.removeItem(key)
  } catch { /* Storage is unavailable; there is nothing to clear. */ }
}

export function clearFrontCacheForUser(userID: number): void {
  try {
    localStorage.removeItem(frontCacheKey(userID, 'daily'))
    localStorage.removeItem(frontCacheKey(userID, 'weekly'))
  } catch { /* Storage is unavailable; there is nothing to clear. */ }
}
