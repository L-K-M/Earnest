export const shellCache = 'earnest-shell-v1'
export const readingCache = 'earnest-reading-v1'
export const contentLimit = 200
export const responseLimit = 100
export const maxResponseBytes = 2 * 1024 * 1024

// The header's brand images keep fixed public paths because the favicon
// links and the manifest name them too, so they are not fingerprinted assets.
export const brandImages = ['/icon-192.png','/wordmark.png']

export function cacheKind(raw: string, origin: string, method='GET'): 'shell' | 'content' | 'reading' | 'resource' | null {
  const url = new URL(raw,origin)
  if (method !== 'GET' || url.origin !== origin) return null
  if (url.pathname === '/' || brandImages.includes(url.pathname) || /^\/assets\/[^/]+\.(js|css|woff2?)$/.test(url.pathname)) return 'shell'
  if (/^\/v1\/entries\/[1-9]\d*\/content$/.test(url.pathname)) return 'content'
  if (/^\/v1\/(me|subscriptions|folders|labels|settings|saved-searches|pipeline)$/.test(url.pathname)) return 'resource'
  if (url.pathname === '/v1/entries' || /^\/v1\/positions\/[^/]+$/.test(url.pathname)) return 'reading'
  return null
}

// Cache keys are in insertion order. Updating a key moves it to the end.
export function evictionKeys(keys: string[], incoming: string, limit: number): string[] {
  const ordered = [...keys.filter(key => key !== incoming),incoming]
  return ordered.slice(0,Math.max(0,ordered.length-Math.max(0,limit)))
}

export function visitedContentPaths(page: unknown): string[] {
  if (!page || typeof page !== 'object' || !('entries' in page) || !Array.isArray(page.entries)) return []
  return [...new Set(page.entries.flatMap((entry: unknown) => {
    if (!entry || typeof entry !== 'object' || !('id' in entry) || !Number.isSafeInteger(entry.id) || Number(entry.id) <= 0 || ('deleted' in entry && entry.deleted)) return []
    return [`/v1/entries/${entry.id}/content`]
  }))].slice(-contentLimit)
}
