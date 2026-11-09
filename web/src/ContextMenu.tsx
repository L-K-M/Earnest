import {useEffect, useLayoutEffect, useRef, type FocusEvent as ReactFocusEvent, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent} from 'react'
import {createPortal} from 'react-dom'
import {Icon, type IconName} from './Icon'

// Context menus offer the actions for a feed, folder, list or article where
// the reader points at it: a right click, a long press, Shift+F10 or the
// Menu key. Shift with a right click keeps the browser's own menu.

export type MenuAction = {id: string; label: string; icon?: IconName; hint?: string; disabled?: boolean; run: () => void | Promise<void>}
export const SEPARATOR = 'separator'
export type MenuItem = MenuAction | typeof SEPARATOR
export type MenuRequest = {label: string; items: MenuItem[]; x: number; y: number; invoker: HTMLElement}

// Keeps the menu this far from the viewport's edges.
const EDGE = 8
// Where popovers work, the menu sits in the top layer above every pane;
// elsewhere it is a fixed element on the page.
const POPOVER = typeof HTMLElement !== 'undefined' && 'showPopover' in HTMLElement.prototype

// The keyboard opens a menu with no pointer position; it then opens below
// the element instead of in the viewport's corner.
export function menuRequest(event: ReactMouseEvent<HTMLElement>, label: string, items: MenuItem[]): MenuRequest {
  const invoker = event.currentTarget
  if (event.clientX === 0 && event.clientY === 0) {
    const box = invoker.getBoundingClientRect()
    return {label, items, x: box.left + EDGE, y: box.bottom, invoker}
  }
  return {label, items, x: event.clientX, y: event.clientY, invoker}
}

// Marks an element whose context menu the keyboard can open.
export const MENU_ATTRIBUTE = 'data-context-menu'

// Shift+F10 and the Menu key open the focused element's menu on every
// platform; macOS browsers do not turn them into a contextmenu event. Other
// elements keep the browser's behaviour.
export function useContextMenuKey() {
  useEffect(() => {
    function keydown(event: KeyboardEvent) {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey) return
      if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
      const target = event.target instanceof Element ? event.target.closest<HTMLElement>(`[${MENU_ATTRIBUTE}]`) : null
      if (!target) return
      event.preventDefault()
      // No pointer position, so the menu opens below the element.
      target.dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, cancelable: true}))
    }
    window.addEventListener('keydown', keydown)
    return () => window.removeEventListener('keydown', keydown)
  }, [])
}

// Drops leading, trailing and doubled separators left by optional items.
export function tidy(items: (MenuItem | null | false)[]): MenuItem[] {
  const out: MenuItem[] = []
  for (const item of items) {
    if (!item) continue
    if (item === SEPARATOR && (out.length === 0 || out.at(-1) === SEPARATOR)) continue
    out.push(item)
  }
  if (out.at(-1) === SEPARATOR) out.pop()
  return out
}

export function ContextMenu({request, onClose, onError}: {request: MenuRequest | null; onClose: () => void; onError: (message: string) => void}) {
  const menu = useRef<HTMLDivElement>(null)

  // Placed after layout so the menu's own size keeps it inside the viewport,
  // and shown in the top layer where the browser supports popovers.
  useLayoutEffect(() => {
    const node = menu.current
    if (!request || !node) return
    // A menu reopened in place is already showing; showing it again throws.
    if (POPOVER && !node.matches(':popover-open')) node.showPopover()
    const box = node.getBoundingClientRect()
    const left = Math.max(EDGE, Math.min(request.x, window.innerWidth - box.width - EDGE))
    const top = request.y + box.height + EDGE > window.innerHeight ? Math.max(EDGE, request.y - box.height) : request.y
    node.style.left = `${left}px`
    node.style.top = `${top}px`
    node.querySelector<HTMLButtonElement>('[role="menuitem"]:not(:disabled)')?.focus({preventScroll: true})
  }, [request])

  // Pointing or scrolling elsewhere, resizing or leaving the window closes
  // the menu without moving focus.
  useEffect(() => {
    if (!request) return
    const outside = (event: Event) => { if (!(event.target instanceof Node && menu.current?.contains(event.target))) onClose() }
    const dismiss = () => onClose()
    document.addEventListener('pointerdown', outside, true)
    document.addEventListener('scroll', outside, true)
    window.addEventListener('resize', dismiss)
    window.addEventListener('blur', dismiss)
    return () => {
      document.removeEventListener('pointerdown', outside, true)
      document.removeEventListener('scroll', outside, true)
      window.removeEventListener('resize', dismiss)
      window.removeEventListener('blur', dismiss)
    }
  }, [request, onClose])

  if (!request) return null

  function close() {
    onClose()
    if (request?.invoker.isConnected) request.invoker.focus({preventScroll: true})
  }
  function choose(item: MenuAction) {
    close()
    void Promise.resolve().then(item.run).catch((failure: unknown) => onError(failure instanceof Error ? failure.message : `${item.label} did not finish.`))
  }
  function keys(event: ReactKeyboardEvent<HTMLDivElement>) {
    const items = [...(menu.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])]
    const index = items.findIndex(item => item === document.activeElement)
    const focus = (next: number) => items[(next + items.length) % items.length]?.focus({preventScroll: true})
    if (event.key === 'Escape' || event.key === 'Tab') { event.preventDefault(); close(); return }
    if (event.key === 'ArrowDown') { event.preventDefault(); focus(index + 1); return }
    if (event.key === 'ArrowUp') { event.preventDefault(); focus(index < 0 ? -1 : index - 1); return }
    if (event.key === 'Home') { event.preventDefault(); focus(0); return }
    if (event.key === 'End') { event.preventDefault(); focus(-1); return }
    // A letter moves to the next item that starts with it.
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      const letter = event.key.toLocaleLowerCase()
      const order = [...items.slice(index + 1), ...items.slice(0, index + 1)]
      const match = order.find(item => item.textContent?.trim().toLocaleLowerCase().startsWith(letter))
      if (match) { event.preventDefault(); match.focus({preventScroll: true}) }
    }
  }
  function blur(event: ReactFocusEvent<HTMLDivElement>) {
    // Focus moving to another control (the palette, another window) closes
    // the menu; a pointer elsewhere is handled above.
    if (event.relatedTarget instanceof Node && !event.currentTarget.contains(event.relatedTarget)) onClose()
  }

  // A modal dialog (the narrow layout's navigation drawer) makes everything
  // outside it inert, so a menu opened from inside one lives there.
  const host = request.invoker.closest('dialog[open]') ?? document.body
  return createPortal(<div ref={menu} role="menu" aria-label={request.label} className="context-menu" popover={POPOVER ? 'manual' : undefined} onKeyDown={keys} onBlur={blur} onContextMenu={event => event.preventDefault()}>
    {request.items.map((item, index) => item === SEPARATOR
      ? <div key={`separator-${index}`} role="separator" className="context-menu-separator" />
      : <button key={item.id} type="button" role="menuitem" tabIndex={-1} disabled={item.disabled} onClick={() => choose(item)}>
        {item.icon ? <Icon name={item.icon} size={16} /> : <span className="context-menu-gap" aria-hidden="true" />}
        <span className="context-menu-label">{item.label}</span>
        {item.hint && <kbd aria-hidden="true">{item.hint}</kbd>}
      </button>)}
  </div>, host)
}
