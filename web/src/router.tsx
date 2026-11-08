// A small History API router. The URL is the source of truth for which
// screen is showing; components read it through useLocation and change it
// through navigate or <Link>. Each history entry carries a key and an index in
// history.state so scroll positions can be restored on Back and Forward, and
// so a guard can undo a Back it was asked to block.
import {useCallback, useEffect, useRef, useSyncExternalStore, type AnchorHTMLAttributes, type FocusEvent, type MouseEvent, type PointerEvent, type RefObject} from 'react'

export type NavigationAction = 'push' | 'replace' | 'pop'
export type Location = {pathname: string; search: string; hash: string; href: string; key: string; index: number; action: NavigationAction}
type EntryState = {key: string; index: number; from?: string}
type NavigateOptions = {replace?: boolean; from?: string}

const listeners = new Set<() => void>()
const blockers = new Set<{message: string}>()
let snapshot: Location | null = null
let lastAction: NavigationAction = 'pop'
// Set while the router itself reverses a blocked Back or Forward, so the
// popstate that reversal produces is not treated as a new navigation.
let reverting = false

function newKey(): string {
  return Math.random().toString(36).slice(2, 10)
}

function entryState(): EntryState | undefined {
  const state: unknown = history.state
  if (!state || typeof state !== 'object' || !('earnest' in state)) return undefined
  const value = (state as {earnest: unknown}).earnest
  if (!value || typeof value !== 'object') return undefined
  const {key, index, from} = value as Partial<EntryState>
  return typeof key === 'string' && typeof index === 'number' ? {key, index, ...(typeof from === 'string' ? {from} : {})} : undefined
}

function read(): Location {
  const href = `${location.pathname}${location.search}${location.hash}`
  const state = entryState()
  const key = state?.key ?? 'initial'
  if (snapshot && snapshot.href === href && snapshot.key === key && snapshot.action === lastAction) return snapshot
  snapshot = {pathname: location.pathname, search: location.search, hash: location.hash, href, key, index: state?.index ?? 0, action: lastAction}
  return snapshot
}

function notify() {
  for (const listener of listeners) listener()
}

function confirmLeave(): boolean {
  const [first] = blockers
  return first === undefined || window.confirm(first.message)
}

if (typeof window !== 'undefined') {
  if ('scrollRestoration' in history) history.scrollRestoration = 'manual'
  window.addEventListener('popstate', () => {
    if (reverting) {
      reverting = false
      return
    }
    const target = entryState()?.index ?? 0
    const committed = snapshot?.index ?? 0
    if (blockers.size > 0 && !confirmLeave()) {
      // History has already moved; step back to where the reader was.
      if (target !== committed) {
        reverting = true
        history.go(committed - target)
      }
      return
    }
    lastAction = 'pop'
    notify()
  })
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function useLocation(): Location {
  return useSyncExternalStore(subscribe, read, read)
}

// The path a pushed entry was opened from, such as the list behind an open
// article. Closing the article goes Back to it instead of stacking a new entry.
export function openedFrom(): string | undefined {
  return entryState()?.from
}

export function currentHref(): string {
  return `${location.pathname}${location.search}${location.hash}`
}

export function navigate(to: string, options: NavigateOptions = {}): boolean {
  const url = new URL(to, location.href)
  if (url.origin !== location.origin) {
    window.location.assign(url.href)
    return true
  }
  const href = `${url.pathname}${url.search}${url.hash}`
  if (href === currentHref() && !options.replace) return true
  if (!confirmLeave()) return false
  const index = (entryState()?.index ?? 0) + (options.replace ? 0 : 1)
  const state = {earnest: {key: newKey(), index, ...(options.from ? {from: options.from} : {})}}
  if (options.replace) history.replaceState(state, '', href)
  else history.pushState(state, '', href)
  lastAction = options.replace ? 'replace' : 'push'
  notify()
  return true
}

// Replace the current entry without asking any guard. Signing out uses it so
// the next account does not start on the previous account's article.
export function resetLocation(href: string): void {
  history.replaceState({earnest: {key: newKey(), index: entryState()?.index ?? 0}}, '', href)
  lastAction = 'replace'
  notify()
}

// Go back to the entry this one was opened from when it is directly behind,
// otherwise replace the current entry with it.
export function returnTo(href: string): void {
  if (openedFrom() === href) {
    history.back()
    return
  }
  navigate(href, {replace: true})
}

// Stamp the first entry so it has a key and index like every pushed entry.
export function initializeHistory(): void {
  if (entryState()) return
  history.replaceState({...(typeof history.state === 'object' && history.state ? history.state : {}), earnest: {key: newKey(), index: 0}}, '')
  snapshot = null
}

// useBlocker asks before leaving while `active`: for in-app navigation, Back
// and Forward, and closing or reloading the tab. It returns a release
// function for the moment the work is saved or deliberately discarded, so the
// navigation that follows does not ask.
export function useBlocker(active: boolean, message: string): () => void {
  const current = useRef<{message: string} | null>(null)
  useEffect(() => {
    if (!active) return
    const blocker = {message}
    current.current = blocker
    blockers.add(blocker)
    const unload = (event: BeforeUnloadEvent) => { if (blockers.has(blocker)) event.preventDefault() }
    window.addEventListener('beforeunload', unload)
    return () => {
      blockers.delete(blocker)
      current.current = null
      window.removeEventListener('beforeunload', unload)
    }
  }, [active, message])
  return useCallback(() => { if (current.current) blockers.delete(current.current) }, [])
}

type LinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & {to: string; replace?: boolean; from?: string; onIntent?: () => void}

function plainLeftClick(event: MouseEvent<HTMLAnchorElement>, target?: string): boolean {
  return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && (!target || target === '_self')
}

// A real anchor: middle-click, modifier-click, copy link and "open in new tab"
// keep working, and a plain click navigates in place. onIntent fires on hover
// or focus so a route's code can be fetched before the click.
export function Link({to, replace, from, onIntent, onClick, onPointerEnter, onFocus, target, ref, ...rest}: LinkProps & {ref?: RefObject<HTMLAnchorElement | null> | ((node: HTMLAnchorElement | null) => void)}) {
  return <a {...rest} ref={ref} href={to} target={target}
    onPointerEnter={(event: PointerEvent<HTMLAnchorElement>) => { onPointerEnter?.(event); onIntent?.() }}
    onFocus={(event: FocusEvent<HTMLAnchorElement>) => { onFocus?.(event); onIntent?.() }}
    onClick={event => {
      onClick?.(event)
      if (!plainLeftClick(event, target)) return
      event.preventDefault()
      navigate(to, {replace, from})
    }} />
}

const SCROLL_KEY = 'earnest.scroll'
const MAX_SCROLL_ENTRIES = 100

function savedScroll(): Record<string, number> {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(SCROLL_KEY) ?? '{}')
    return value && typeof value === 'object' ? value as Record<string, number> : {}
  } catch {
    return {}
  }
}

function saveScroll(key: string, top: number) {
  const all = savedScroll()
  delete all[key]
  all[key] = Math.round(top)
  const keys = Object.keys(all)
  for (const old of keys.slice(0, Math.max(0, keys.length - MAX_SCROLL_ENTRIES))) delete all[old]
  try { sessionStorage.setItem(SCROLL_KEY, JSON.stringify(all)) } catch { /* Storage can be full or disabled; scrolling still works. */ }
}

// Retry for a short while because lazily loaded screens and their data can
// arrive a few frames after the route changes.
export function whenPresent<T>(find: () => T | null | undefined, apply: (value: T) => void, frames = 60): () => void {
  let frame = 0
  let handle = 0
  const attempt = () => {
    const value = find()
    if (value) { apply(value); return }
    if (++frame < frames) handle = window.requestAnimationFrame(attempt)
  }
  handle = window.requestAnimationFrame(attempt)
  return () => window.cancelAnimationFrame(handle)
}

// useScrollRestoration remembers a scroll container's position per history
// entry. Back and Forward restore it, a pushed navigation starts at the top,
// and a #fragment scrolls its target into view and moves focus there.
export function useScrollRestoration(container: RefObject<HTMLElement | null>, current: Location, scope: string): void {
  useEffect(() => {
    const node = container.current
    if (!node) return
    let frame = 0
    const remember = () => {
      window.cancelAnimationFrame(frame)
      frame = window.requestAnimationFrame(() => saveScroll(current.key, node.scrollTop))
    }
    node.addEventListener('scroll', remember, {passive: true})
    return () => {
      window.cancelAnimationFrame(frame)
      node.removeEventListener('scroll', remember)
    }
  }, [container, current.key])

  useEffect(() => {
    const node = container.current
    if (!node) return
    if (current.hash.length > 1) {
      let id = current.hash.slice(1)
      try { id = decodeURIComponent(id) } catch { /* Keep the raw fragment. */ }
      return whenPresent(() => document.getElementById(id), target => {
        target.scrollIntoView({block: 'start'})
        if (!target.hasAttribute('tabindex') && !target.matches('a[href], button, input, select, textarea')) target.setAttribute('tabindex', '-1')
        target.focus({preventScroll: true})
      })
    }
    // A replaced entry (a changed filter) keeps its place; only a new page
    // starts at the top.
    if (current.action === 'push') {
      node.scrollTo?.({top: 0})
      return
    }
    if (current.action === 'replace') return
    const top = savedScroll()[current.key]
    if (!top) return
    return whenPresent(() => node.scrollHeight - node.clientHeight >= top ? node : null, target => { target.scrollTo?.({top}) }, 30)
  }, [container, current.key, current.hash, current.action, scope])
}
