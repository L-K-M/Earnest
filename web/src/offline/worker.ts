/// <reference lib="webworker" />
import {appNavigation, brandImages, cacheKind, contentLimit, evictionKeys, maxResponseBytes, readingCache, responseLimit, shellCache, warmContentPaths} from './policy'

const worker = self as unknown as ServiceWorkerGlobalScope
const generationKey = new URL('/__earnest_offline_generation',worker.location.origin).href
let epoch = 0
let writes: Promise<unknown> = Promise.resolve()
function serialize<T>(job: () => Promise<T>): Promise<T> {
  const next = writes.then(job,job)
  writes = next.catch(() => undefined)
  return next
}
async function purge() {
  epoch++
  await serialize(async () => {await caches.delete(readingCache)})
}
async function bindGeneration(generation: unknown, version: number) {
  if (typeof generation !== 'string' || generation === '') return
  await serialize(async () => {
    if (version !== epoch) return
    let cache = await caches.open(readingCache)
    const previous = await cache.match(generationKey)
    if (previous && await previous.text() === generation) return
    // The cache has its own binding, independent of IndexedDB and worker restarts.
    epoch++
    await caches.delete(readingCache)
    cache = await caches.open(readingCache)
    await cache.put(generationKey,new Response(generation))
  })
}
async function store(request: Request, response: Response, version: number) {
  const kind = cacheKind(request.url,worker.location.origin,request.method)
  if (!kind || !response.ok || response.type === 'opaque' || response.redirected) return
  const body = await response.clone().arrayBuffer()
  if (body.byteLength > maxResponseBytes) return
  await serialize(async () => {
    if (version !== epoch) return
    const cache = await caches.open(kind === 'shell' ? shellCache : readingCache)
    const keys = (await cache.keys()).filter(key => kind === 'shell' || cacheKind(key.url,worker.location.origin) === kind).map(key => key.url)
    const limit = kind === 'content' ? contentLimit : responseLimit
    for (const key of evictionKeys(keys,request.url,limit)) await cache.delete(key)
    await cache.delete(request)
    await cache.put(request,response)
  })
}

async function precache() {
  const response = await fetch('/',{cache:'reload'})
  if (!response.ok || !(response.headers.get('Content-Type') ?? '').includes('text/html')) throw new Error('The app shell is unavailable.')
  const html = await response.clone().text()
  const assets = [...new Set(Array.from(html.matchAll(/(?:src|href)=["'](\/assets\/[^"']+)["']/g),match => match[1]))]
  const cache = await caches.open(shellCache)
  await cache.put('/',response)
  await cache.addAll([...assets,...brandImages])
}
worker.addEventListener('install',event => {event.waitUntil(precache())})
worker.addEventListener('activate',event => {event.waitUntil(worker.clients.claim())})
worker.addEventListener('message',event => {
  if (event.data?.type === 'earnest:clear-offline') event.waitUntil(purge().then(() => event.ports[0]?.postMessage('cleared')))
})

async function warmEntries(response: Response, version: number) {
  const paths = warmContentPaths(await response.json())
  // Read only stored content. Never fetch publisher URLs or start extraction.
  for (const path of paths) {
    if (version !== epoch) return
    const request = new Request(new URL(path,worker.location.origin),{credentials:'same-origin'})
    try {
      const content = await fetch(request)
      if (content.status === 401) {await purge();return}
      await store(request,content,version)
    } catch {return} // The visited page remains usable if prefetch loses the network.
  }
}

worker.addEventListener('fetch',event => {
  const request = event.request
  const url = new URL(request.url)
  if (url.origin !== worker.location.origin) return
  if (url.pathname === '/v1/session/logout' || url.pathname === '/v1/session/login') {
    event.respondWith((async () => {const response = await fetch(request);if(response.ok) await purge();return response})())
    return
  }
  // Deep links such as /feeds/3/articles/9 are the same app shell. Online, the
  // fresh shell also refreshes the cached one; offline, the cached one opens
  // the route and the app reads its data from the reading cache.
  if (request.mode === 'navigate' && request.method === 'GET' && appNavigation(url.href,worker.location.origin)) {
    event.respondWith((async () => {
      try {
        const response = await fetch(request)
        const html = (response.headers.get('Content-Type') ?? '').includes('text/html')
        if (response.ok && html && !response.redirected) {
          const copy = response.clone()
          event.waitUntil(serialize(async () => {await (await caches.open(shellCache)).put('/',copy)}).catch(() => undefined))
        }
        return response
      } catch (error) {
        const shell = await (await caches.open(shellCache)).match('/')
        if (shell) return shell
        throw error
      }
    })())
    return
  }
  const kind = cacheKind(url.href,worker.location.origin,request.method)
  const generationRead = request.method === 'GET' && (url.pathname === '/v1/bootstrap' || url.pathname === '/v1/changes')
  if (!kind && !generationRead) return
  const version = epoch
  // Register the lifetime promise synchronously; prefetch may outlive respondWith.
  let finish: () => void = () => undefined
  const lifetime = new Promise<void>(resolve => {finish=resolve})
  event.waitUntil(lifetime)
  event.respondWith((async () => {
    let response: Response
    try {
      response = await fetch(request)
    } catch (error) {
      finish()
      if (generationRead) throw error
      await writes
      if (kind !== 'shell' && version !== epoch) throw error
      const cache = await caches.open(kind === 'shell' ? shellCache : readingCache)
      const cached = (await cache.match(request)) ?? (kind === 'shell' && request.mode === 'navigate' ? await cache.match('/') : undefined)
      if (cached) return cached
      throw error
    }
    let warming = false
    try {
      if (response.status === 401) {await purge();return response}
      if (generationRead && response.ok) {
        const cursor = await response.clone().json()
        await bindGeneration(url.pathname === '/v1/bootstrap' ? cursor.generation : cursor.next?.generation,version)
      }
      if (url.pathname === '/v1/me' && response.ok && version === epoch) {
        const cache = await caches.open(readingCache)
        const old = await cache.match(request)
        const current = await response.clone().text()
        if (old && await old.text() !== current) {await purge();await store(request,response.clone(),epoch);return response}
      }
      await store(request,response.clone(),version)
      if (response.ok && url.pathname === '/v1/entries') {
        warming = true
        void warmEntries(response.clone(),version).catch(() => undefined).finally(finish)
      }
    } catch {
      // Cache quota and eviction failures must not hide a successful network read.
    } finally {
      if (!warming) finish()
    }
    return response
  })().catch(error => {finish();throw error}))
})
