// @vitest-environment jsdom
import type {MouseEvent as ReactMouseEvent, ReactNode} from 'react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {createRoot} from 'react-dom/client'
import {request} from './request'
import {defaultSettings} from './Settings'
import type {Entry, View} from './reader'

const {act, actRange, entries} = vi.hoisted(() => ({
  act: vi.fn().mockResolvedValue(undefined),
  actRange: vi.fn().mockResolvedValue(undefined),
  entries: [4, 5].map(id => ({id, seq: id * 10, title: `Article ${id}`, subscription_id: 8, feed_id: 42, url: `https://example.test/${id}`, state: {shelf: {value: 'none'}, kept: {value: false}, seen: {value: 'unseen'}}})),
}))
vi.mock('react-dom/client', () => ({createRoot: vi.fn()}))
vi.mock('./request', async original => ({...await original<typeof import('./request')>(), request: vi.fn(), setCSRF: vi.fn()}))
vi.mock('./syncBridge', () => ({useReaderSync: () => ({state: (value: Entry) => value.state, act, actRange, newestSeq: () => 120, retry: vi.fn(), status: 'Reading actions synced.', error: ''})}))
vi.mock('./offline/Offline', () => ({OfflineStatus: () => null, useOnline: () => true, registerOffline: vi.fn().mockResolvedValue(undefined), clearOffline: vi.fn().mockResolvedValue(undefined)}))
vi.mock('./River', () => ({River: ({view, onOpen, onMenu}: {view: View; onOpen: (entry: Entry) => void; onMenu?: (event: ReactMouseEvent<HTMLElement>, entry: Entry, items: []) => void}) => <section><h2>{view.title} list</h2>{entries.map(entry => <button key={entry.id} onClick={() => onOpen(entry as Entry)} onContextMenu={event => onMenu?.(event, entry as Entry, [])}>Open {entry.title}</button>)}</section>}))
vi.mock('./ArticleReader', () => ({Reader: ({entry}: {entry: Entry}) => <article><h2>{entry.title}</h2></article>}))

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function() {this.setAttribute('open', '')}
  HTMLDialogElement.prototype.close = function() {this.removeAttribute('open'); this.dispatchEvent(new Event('close'))}
  localStorage.setItem('earnest.csrf', 'token')
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id: 1, principal_id: 1}
    if (path === '/v1/settings') return {...defaultSettings, rev: 1}
    if (path === '/v1/subscriptions') return [{id: 8, feed_id: 42, title: 'Rust', url: 'https://feed.test', deleted: false, folder_id: 0, cadence: 'today', health: {consecutive_failures: 0}}]
    if (path === '/v1/entries/4') return entries[0]
    if (path === '/v1/pipeline') return {ask: 'disabled'}
    return []
  })
  vi.mocked(createRoot).mockImplementation(() => ({render: (node: ReactNode) => {render(node)}, unmount: () => undefined}))
})
const confirms: {mockRestore: () => void}[] = []
afterEach(() => {cleanup(); localStorage.clear(); vi.clearAllMocks(); for (const spy of confirms.splice(0)) spy.mockRestore()})
function answer(value: boolean) {
  const spy = vi.spyOn(window, 'confirm').mockReturnValue(value)
  confirms.push(spy)
  return spy
}

async function start(path: string) {
  history.replaceState(null, '', path)
  vi.resetModules()
  await import('./main')
}

it('goes home to Today from the logo, closing an open article', async () => {
  await start('/feeds/8/articles/4')
  await screen.findByRole('heading', {name: 'Article 4'}, {timeout: 5000})
  fireEvent.click(screen.getByRole('link', {name: 'Earnest'}))
  await waitFor(() => expect(location.pathname).toBe('/today'))
  expect(await screen.findByRole('heading', {name: 'Today list'})).toBeTruthy()
  expect(screen.queryByRole('heading', {name: 'Article 4'})).toBeNull()
})

it('marks a feed read from its sidebar menu after a confirmation', async () => {
  await start('/today')
  const feed = await screen.findByRole('link', {name: 'Rust'}, {timeout: 5000})
  const confirm = answer(true)
  fireEvent.contextMenu(feed, {clientX: 30, clientY: 40})
  fireEvent.click(screen.getByRole('menuitem', {name: 'Mark all as read'}))
  await waitFor(() => expect(actRange).toHaveBeenCalledWith({kind: 'subscriptions', ids: [8]}, 120))
  expect(confirm).toHaveBeenCalledWith('Mark every article in Rust read? Articles that arrive later stay unread.')
  expect(await screen.findByText('Marked everything in Rust read.')).toBeTruthy()
  expect(screen.queryByRole('menu')).toBeNull()
})

it('leaves the browser menu to Shift and right click', async () => {
  await start('/today')
  const feed = await screen.findByRole('link', {name: 'Rust'}, {timeout: 5000})
  expect(fireEvent.contextMenu(feed, {clientX: 30, clientY: 40, shiftKey: true})).toBe(true)
  expect(screen.queryByRole('menu')).toBeNull()
})

it('opens the focused feed\'s menu below it with Shift+F10 or the Menu key', async () => {
  await start('/today')
  const feed = await screen.findByRole('link', {name: 'Rust'}, {timeout: 5000})
  feed.getBoundingClientRect = () => ({left: 12, bottom: 200, top: 180, right: 200, width: 188, height: 20, x: 12, y: 180, toJSON: () => ({})})
  feed.focus()
  expect(fireEvent.keyDown(feed, {key: 'F10', shiftKey: true})).toBe(false)
  const menu = screen.getByRole('menu', {name: 'Rust actions'})
  expect([menu.style.left, menu.style.top]).toEqual(['20px', '200px'])
  fireEvent.keyDown(screen.getByRole('menuitem', {name: 'Mark all as read'}), {key: 'Escape'})
  fireEvent.keyDown(feed, {key: 'ContextMenu'})
  expect(screen.getByRole('menu', {name: 'Rust actions'})).toBeTruthy()
  fireEvent.keyDown(screen.getByRole('menuitem', {name: 'Mark all as read'}), {key: 'Escape'})
  // Elements without a menu keep the browser's behaviour.
  const later = screen.getByRole('link', {name: 'Later'})
  expect(fireEvent.keyDown(later, {key: 'F10', shiftKey: true})).toBe(true)
  expect(screen.queryByRole('menu')).toBeNull()
})

it('closes a menu with Escape without closing the open article', async () => {
  await start('/feeds/8/articles/4')
  await screen.findByRole('heading', {name: 'Article 4'}, {timeout: 5000})
  const today = screen.getByRole('link', {name: 'Today'})
  fireEvent.contextMenu(today, {clientX: 30, clientY: 40})
  const item = screen.getByRole('menuitem', {name: 'Mark all as read'})
  expect(document.activeElement).toBe(item)
  fireEvent.keyDown(item, {key: 'Escape'})
  expect(screen.queryByRole('menu')).toBeNull()
  expect(location.pathname).toBe('/feeds/8/articles/4')
  expect(document.activeElement).toBe(today)
})

it('marks a list read up to an article from its row menu', async () => {
  await start('/feeds/8')
  const row = await screen.findByRole('button', {name: 'Open Article 5'}, {timeout: 5000})
  answer(true)
  fireEvent.contextMenu(row, {clientX: 30, clientY: 40})
  expect(screen.getByRole('menu', {name: 'Article 5 actions'})).toBeTruthy()
  fireEvent.click(screen.getByRole('menuitem', {name: 'Mark read up to here'}))
  await waitFor(() => expect(actRange).toHaveBeenCalledWith({kind: 'subscriptions', ids: [8]}, 50))
})
