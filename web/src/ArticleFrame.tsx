import {useEffect, useRef, useState} from 'react'

// The article page reports its height and forwards keys pressed while focus
// is inside it (frameScript in internal/web/content.go). Sizing the frame to
// the article leaves one scroller, the reading pane, so Space, Page Down and
// the arrow keys move through the whole article wherever focus is. Until the
// page reports, the frame keeps the pane's height and scrolls on its own.

// A cap, so a misbehaving page cannot demand an absurd layout.
const MAX_FRAME_HEIGHT = 1_000_000
const LINE_STEP = 40
// Space and Page Down keep a little of the previous screen in view, as
// browsers do.
const PAGE_FRACTION = 0.875

export type ScrollMove = {by: number} | {to: 'top' | 'bottom'}

export function scrollMove(key: string, shift: boolean, page: number): ScrollMove | null {
  const step = Math.max(LINE_STEP, Math.round(page * PAGE_FRACTION))
  switch (key) {
  case ' ': return {by: shift ? -step : step}
  case 'PageDown': return {by: step}
  case 'PageUp': return {by: -step}
  case 'ArrowDown': return {by: LINE_STEP}
  case 'ArrowUp': return {by: -LINE_STEP}
  case 'Home': return {to: 'top'}
  case 'End': return {to: 'bottom'}
  default: return null
  }
}

// The keys frameScript forwards: its scroll keys, single characters and
// Escape. Nothing else reaches the app's shortcuts.
const SCROLL_KEYS = new Set([' ', 'PageDown', 'PageUp', 'Home', 'End', 'ArrowDown', 'ArrowUp'])

function forwardedKey(key: string): boolean {
  return key.length === 1 || key === 'Escape' || SCROLL_KEYS.has(key)
}

function freshToken(): string {
  // randomUUID needs a secure context and a current browser; the sync layer
  // needs the same, but a fallback keeps the reader rendering where only
  // this API is missing (Chrome before 92).
  return typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, '0')).join('')
}

export type FrameMessage = {earnest: 'height'; height: number} | {earnest: 'key'; key: string; shift: boolean}

// Validates a message from the article page. Anything unexpected is ignored.
// The nonce binds the message to the document the frame was pointed at: the
// page echoes it from frameScript's closure, so a message from an earlier
// article, another window, or a page that is not the one this component
// asked for carries the wrong token and is dropped.
export function frameMessage(data: unknown, nonce: string): FrameMessage | null {
  if (!nonce || !data || typeof data !== 'object') return null
  const value = data as Record<string, unknown>
  if (value.nonce !== nonce) return null
  if (value.earnest === 'height' && typeof value.height === 'number' && Number.isFinite(value.height) && value.height >= 0) {
    return {earnest: 'height', height: Math.min(Math.ceil(value.height), MAX_FRAME_HEIGHT)}
  }
  if (value.earnest === 'key' && typeof value.key === 'string' && forwardedKey(value.key)) {
    return {earnest: 'key', key: value.key, shift: value.shift === true}
  }
  return null
}

export function ArticleFrame({src}: {src: string}) {
  const frame = useRef<HTMLIFrameElement>(null)
  const [sized, setSized] = useState<{nonce: string; height: number} | null>(null)
  // A fresh token per article, echoed by the page it loads, held in state
  // rather than a memo: the token is load-bearing and a memo cache may be
  // discarded without a prop change. The height is kept with the load that
  // measured it: a new article starts unsized until its own page reports.
  const [load, setLoad] = useState(() => ({src, nonce: freshToken()}))
  if (load.src !== src) setLoad({src, nonce: freshToken()})
  const nonce = load.nonce
  const url = new URL(src, window.location.href)
  url.searchParams.set('frame_nonce', nonce)
  const framed = url.href
  // The height belongs to the load that measured it, not just the address:
  // a later visit to the same article starts unsized like any other load.
  const height = sized && sized.nonce === nonce ? sized.height : null
  useEffect(() => {
    function receive(event: MessageEvent) {
      const node = frame.current
      // The sandboxed page has an opaque origin ("null"), so its window, not
      // its origin, identifies it.
      if (!node || event.source === null || event.source !== node.contentWindow) return
      const message = frameMessage(event.data, nonce)
      if (!message) return
      if (message.earnest === 'height') {
        setSized({nonce, height: message.height})
        return
      }
      const pane = node.closest<HTMLElement>('.reader-scroll')
      const move = pane ? scrollMove(message.key, message.shift, pane.clientHeight) : null
      if (pane && move) {
        if ('by' in move) pane.scrollBy({top: move.by})
        else pane.scrollTo({top: move.to === 'top' ? 0 : pane.scrollHeight})
        return
      }
      // Other keys reach the app's shortcuts as if pressed beside the frame.
      // The page forwards only keys pressed without Ctrl, Alt or Meta (Shift
      // aside), so a modified key never arrives here as a bare one.
      node.dispatchEvent(new KeyboardEvent('keydown', {key: message.key, shiftKey: message.shift, bubbles: true, cancelable: true}))
    }
    window.addEventListener('message', receive)
    return () => window.removeEventListener('message', receive)
  }, [src, nonce])
  return <iframe ref={frame} title="Article body" src={framed} sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox" style={height === null ? undefined : {height}} />
}
