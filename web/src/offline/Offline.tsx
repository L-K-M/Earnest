import {useEffect, useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {request} from '../request'
import type {components} from '../api.generated'
import {readingCache} from './policy'

export function useOnline() {
  const [online,setOnline] = useState(() => navigator.onLine)
  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online',update);window.addEventListener('offline',update)
    return () => {window.removeEventListener('online',update);window.removeEventListener('offline',update)}
  },[])
  return online
}

export function OfflineStatus() {
  return useOnline() ? null : <p role="status">Offline. Changes will sync.</p>
}

export function OfflineArticle({entryID}: {entryID: number}) {
  const content = useQuery({queryKey:['offline-content',entryID], networkMode:'always', retry:false, queryFn:({signal}) => request<components['schemas']['Content']>(`/v1/entries/${entryID}/content`,{signal})})
  return <section aria-label="Offline article"><p>Offline reading copy. Images and formatting are available when you reconnect.</p>
    {content.isPending && <p role="status">Opening saved article…</p>}
    {content.error && <p role="alert">This article has no offline copy. Open this view while online to save it.</p>}
    {content.data && <div className="offline-article">{content.data.text}</div>}
  </section>
}

export async function registerOffline(): Promise<void> {
  if (!('serviceWorker' in navigator)) return
  // Push owns the root registration when present; it must import this worker's
  // handlers. Never replace an existing push worker from a different script.
  const existing = await navigator.serviceWorker.getRegistration('/')
  const script = existing?.active?.scriptURL ?? existing?.waiting?.scriptURL ?? existing?.installing?.scriptURL
  if (script && new URL(script).pathname !== '/offline-worker.js') throw new Error('Offline reading needs the existing push worker to include /offline-worker.js. The push registration was preserved.')
  await navigator.serviceWorker.register('/offline-worker.js',{scope:'/',type:'module'})
}

export async function clearOffline(): Promise<void> {
  // The acknowledgement fences in-flight prefetch writes before sign-out ends.
  // The push worker can own the root registration, so look every registration
  // up by script URL instead of assuming the controlling worker is offline.
  const controller = navigator.serviceWorker?.controller
  const registrations = await navigator.serviceWorker?.getRegistrations() ?? []
  const workers = registrations
    .map(registration => registration.active ?? registration.waiting ?? registration.installing)
    .filter((worker): worker is ServiceWorker => worker !== null)
  const worker = (controller && new URL(controller.scriptURL).pathname === '/offline-worker.js' ? controller : undefined)
    ?? workers.find(worker => new URL(worker.scriptURL).pathname === '/offline-worker.js')
  if (worker) await new Promise<void>((resolve,reject) => {
    const channel = new MessageChannel()
    const timeout = window.setTimeout(() => {channel.port1.close();reject(new Error('Could not clear offline reading copies. Retry sign out.'))},5000)
    channel.port1.onmessage = () => {window.clearTimeout(timeout);channel.port1.close();resolve()}
    worker.postMessage({type:'earnest:clear-offline'},[channel.port2])
  })
  if ('caches' in window) await caches.delete(readingCache)
}
