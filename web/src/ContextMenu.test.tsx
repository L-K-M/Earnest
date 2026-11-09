// @vitest-environment jsdom
import {useState, type MouseEvent as ReactMouseEvent} from 'react'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {ContextMenu, menuRequest, SEPARATOR, tidy, type MenuItem} from './ContextMenu'

afterEach(() => { cleanup(); vi.restoreAllMocks() })

function Harness({items, onError = vi.fn()}: {items: MenuItem[]; onError?: (message: string) => void}) {
  const [request, setRequest] = useState<ReturnType<typeof menuRequest> | null>(null)
  return <>
    <button type="button" onContextMenu={(event: ReactMouseEvent<HTMLElement>) => { event.preventDefault(); setRequest(menuRequest(event, 'Rust actions', items)) }}>Rust</button>
    <button type="button">Elsewhere</button>
    <ContextMenu request={request} onClose={() => setRequest(null)} onError={onError} />
  </>
}

describe('tidy', () => {
  it('drops empty slots and stray separators', () => {
    const a = {id: 'a', label: 'A', run: vi.fn()}
    const b = {id: 'b', label: 'B', run: vi.fn()}
    expect(tidy([SEPARATOR, null, a, SEPARATOR, false, SEPARATOR, b, SEPARATOR])).toEqual([a, SEPARATOR, b])
    expect(tidy([SEPARATOR, null])).toEqual([])
  })
})

describe('menuRequest', () => {
  it('opens at the pointer, or below the element for the keyboard', () => {
    const element = document.createElement('a')
    element.getBoundingClientRect = () => ({left: 20, bottom: 90, top: 60, right: 200, width: 180, height: 30, x: 20, y: 60, toJSON: () => ({})})
    const pointer = menuRequest({currentTarget: element, clientX: 300, clientY: 400} as unknown as ReactMouseEvent<HTMLElement>, 'Menu', [])
    expect([pointer.x, pointer.y, pointer.invoker]).toEqual([300, 400, element])
    const keyboard = menuRequest({currentTarget: element, clientX: 0, clientY: 0} as unknown as ReactMouseEvent<HTMLElement>, 'Menu', [])
    expect([keyboard.x, keyboard.y]).toEqual([28, 90])
  })
})

describe('ContextMenu', () => {
  const items = (run: () => void | Promise<void> = vi.fn()): MenuItem[] => [
    {id: 'mark', label: 'Mark all as read', run},
    {id: 'off', label: 'Unavailable', disabled: true, run: vi.fn()},
    SEPARATOR,
    {id: 'refresh', label: 'Refresh feed', hint: 'r', run: vi.fn()},
  ]

  it('opens a labelled menu with focus on the first available item', () => {
    render(<Harness items={items()} />)
    fireEvent.contextMenu(screen.getByRole('button', {name: 'Rust'}), {clientX: 40, clientY: 50})
    const menu = screen.getByRole('menu', {name: 'Rust actions'})
    expect(screen.getAllByRole('menuitem').map(item => item.textContent)).toEqual(['Mark all as read', 'Unavailable', 'Refresh feedr'])
    expect(screen.getByRole('separator')).toBeTruthy()
    expect(document.activeElement).toBe(screen.getByRole('menuitem', {name: 'Mark all as read'}))
    expect([menu.style.left, menu.style.top]).toEqual(['40px', '50px'])
  })

  it('moves through available items with the arrow keys, Home, End and letters', () => {
    render(<Harness items={items()} />)
    fireEvent.contextMenu(screen.getByRole('button', {name: 'Rust'}), {clientX: 40, clientY: 50})
    const menu = screen.getByRole('menu')
    const mark = screen.getByRole('menuitem', {name: 'Mark all as read'})
    const refresh = screen.getByRole('menuitem', {name: /Refresh feed/})
    fireEvent.keyDown(menu, {key: 'ArrowDown'})
    expect(document.activeElement).toBe(refresh)
    fireEvent.keyDown(menu, {key: 'ArrowDown'})
    expect(document.activeElement).toBe(mark)
    fireEvent.keyDown(menu, {key: 'ArrowUp'})
    expect(document.activeElement).toBe(refresh)
    fireEvent.keyDown(menu, {key: 'Home'})
    expect(document.activeElement).toBe(mark)
    fireEvent.keyDown(menu, {key: 'End'})
    expect(document.activeElement).toBe(refresh)
    fireEvent.keyDown(menu, {key: 'm'})
    expect(document.activeElement).toBe(mark)
  })

  it('closes on Escape and returns focus to what it was opened from', () => {
    render(<Harness items={items()} />)
    const invoker = screen.getByRole('button', {name: 'Rust'})
    fireEvent.contextMenu(invoker, {clientX: 40, clientY: 50})
    fireEvent.keyDown(screen.getByRole('menu'), {key: 'Escape'})
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(invoker)
  })

  it('lets Tab leave the menu from the element it was opened from', () => {
    render(<Harness items={items()} />)
    const invoker = screen.getByRole('button', {name: 'Rust'})
    fireEvent.contextMenu(invoker, {clientX: 40, clientY: 50})
    // Not cancelled, so the browser moves focus on from the invoker.
    expect(fireEvent.keyDown(screen.getByRole('menu'), {key: 'Tab'})).toBe(true)
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(invoker)
  })

  it('keeps a menu with nothing available closable from the keyboard', () => {
    render(<Harness items={[{id: 'off', label: 'Unavailable', disabled: true, run: vi.fn()}]} />)
    const invoker = screen.getByRole('button', {name: 'Rust'})
    fireEvent.contextMenu(invoker, {clientX: 40, clientY: 50})
    expect(document.activeElement).toBe(screen.getByRole('menu'))
    fireEvent.keyDown(screen.getByRole('menu'), {key: 'Escape'})
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(invoker)
  })

  it('closes when the pointer goes elsewhere', () => {
    render(<Harness items={items()} />)
    fireEvent.contextMenu(screen.getByRole('button', {name: 'Rust'}), {clientX: 40, clientY: 50})
    fireEvent.pointerDown(screen.getByRole('button', {name: 'Elsewhere'}))
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('closes, then runs the chosen item', async () => {
    const run = vi.fn()
    render(<Harness items={items(run)} />)
    fireEvent.contextMenu(screen.getByRole('button', {name: 'Rust'}), {clientX: 40, clientY: 50})
    fireEvent.click(screen.getByRole('menuitem', {name: 'Mark all as read'}))
    expect(screen.queryByRole('menu')).toBeNull()
    await waitFor(() => expect(run).toHaveBeenCalledOnce())
  })

  it('opens inside the modal dialog it was opened from, which leaves the rest inert', () => {
    function InDialog() {
      const [request, setRequest] = useState<ReturnType<typeof menuRequest> | null>(null)
      return <>
        <dialog open><a href="/feeds/1" onContextMenu={event => { event.preventDefault(); setRequest(menuRequest(event, 'Rust actions', items())) }}>Rust</a></dialog>
        <ContextMenu request={request} onClose={() => setRequest(null)} onError={vi.fn()} />
      </>
    }
    render(<InDialog />)
    fireEvent.contextMenu(screen.getByRole('link', {name: 'Rust'}), {clientX: 10, clientY: 10})
    expect(screen.getByRole('menu').parentElement?.tagName).toBe('DIALOG')
  })

  it('reports an item that fails', async () => {
    const onError = vi.fn()
    render(<Harness items={items(() => Promise.reject(new Error('Reading state is not ready.')))} onError={onError} />)
    fireEvent.contextMenu(screen.getByRole('button', {name: 'Rust'}), {clientX: 40, clientY: 50})
    fireEvent.click(screen.getByRole('menuitem', {name: 'Mark all as read'}))
    await waitFor(() => expect(onError).toHaveBeenCalledWith('Reading state is not ready.'))
  })
})
