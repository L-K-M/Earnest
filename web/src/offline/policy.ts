export const shellCache = 'earnest-shell-v1'
export const readingCache = 'earnest-reading-v1'
export const contentLimit = 200
export const responseLimit = 100
export const maxResponseBytes = 2 * 1024 * 1024

// The header's brand images keep fixed public paths because the favicon
// links and the manifest name them too, so they are not fingerprinted assets.
export const brandImages = ['/icon-192.png','/wordmark.png']

// warmLimit bounds how many article bodies a river page prefetches for offline
// reading. A forward river page is ordered oldest-first, so its first entries
// are the top of the list and the ones the user reads next. Warming them keeps
// the next screen available offline without downloading the whole backlog on
// every page.
export const warmLimit = 10

export function cacheKind(raw: string, origin: string, method='GET'): 'shell' | 'content' | 'reading' | 'resource' | null {
  const url = new URL(raw,origin)
  if (method !== 'GET' || url.origin !== origin) return null
  if (url.pathname === '/' || brandImages.includes(url.pathname) || /^\/assets\/[^/]+\.(js|css|woff2?)$/.test(url.pathname)) return 'shell'
  if (/^\/v1\/entries\/[1-9]\d*\/(content|image)$/.test(url.pathname)) return 'content'
  if (/^\/v1\/(me|subscriptions|folders|labels|settings|saved-searches|pipeline|front-page)$/.test(url.pathname)) return 'resource'
  // A single article's metadata lets an article link reopen offline.
  if (url.pathname === '/v1/entries' || /^\/v1\/entries\/[1-9]\d*$/.test(url.pathname) || /^\/v1\/positions\/[^/]+$/.test(url.pathname)) return 'reading'
  return null
}

// Server-owned paths. Everything else that a browser navigates to is a
// client route served by the same index.html, so offline it can be served
// from the cached shell.
const SERVER_PATHS = /^\/(v1|api|web|mcp|push|content|img|openapi\.json|llms\.txt|metrics|healthz)(\/|$)/

export function appNavigation(raw: string, origin: string): boolean {
  const url = new URL(raw,origin)
  if (url.origin !== origin || url.pathname === '/') return false
  if (SERVER_PATHS.test(url.pathname)) return false
  // A path ending in a file name is a static file, not a route.
  return !/\.[a-z0-9]+$/i.test(url.pathname)
}

// Cache keys are in insertion order. Updating a key moves it to the end.
export function evictionKeys(keys: string[], incoming: string, limit: number): string[] {
  const ordered = [...keys.filter(key => key !== incoming),incoming]
  return ordered.slice(0,Math.max(0,ordered.length-Math.max(0,limit)))
}

export function warmContentPaths(page: unknown): string[] {
  if (!page || typeof page !== 'object' || !('entries' in page) || !Array.isArray(page.entries)) return []
  return [...new Set(page.entries.flatMap((entry: unknown) => {
    if (!entry || typeof entry !== 'object' || !('id' in entry) || !Number.isSafeInteger(entry.id) || Number(entry.id) <= 0 || ('deleted' in entry && entry.deleted)) return []
    return [`/v1/entries/${entry.id}/content`]
  }))].slice(0,warmLimit)
}
