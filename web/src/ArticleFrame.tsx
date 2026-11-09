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

export type FrameMessage = {earnest: 'height'; height: number} | {earnest: 'key'; key: string; shift: boolean}

// Validates a message from the article page. Anything unexpected is ignored.
export function frameMessage(data: unknown): FrameMessage | null {
  if (!data || typeof data !== 'object') return null
  const value = data as Record<string, unknown>
  if (value.earnest === 'height' && typeof value.height === 'number' && Number.isFinite(value.height) && value.height >= 0) {
    return {earnest: 'height', height: Math.min(Math.ceil(value.height), MAX_FRAME_HEIGHT)}
  }
  if (value.earnest === 'key' && typeof value.key === 'string' && value.key.length > 0 && value.key.length <= 16) {
    return {earnest: 'key', key: value.key, shift: value.shift === true}
  }
  return null
}

export function ArticleFrame({src}: {src: string}) {
  const frame = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState<number | null>(null)
  useEffect(() => {
    function receive(event: MessageEvent) {
      const node = frame.current
      // The sandboxed page has an opaque origin ("null"), so its window, not
      // its origin, identifies it.
      if (!node || event.source === null || event.source !== node.contentWindow) return
      const message = frameMessage(event.data)
      if (!message) return
      if (message.earnest === 'height') {
        setHeight(message.height)
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
  }, [])
  return <iframe ref={frame} title="Article body" src={src} sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox" style={height === null ? undefined : {height}} />
}
