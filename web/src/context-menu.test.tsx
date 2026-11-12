// @vitest-environment jsdom
import type {MouseEvent as ReactMouseEvent, ReactNode} from 'react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
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
vi.mock('./FrontPage', () => ({FrontPage: () => <section><h2>Front page list</h2></section>}))
vi.mock('./ArticleReader', () => ({Reader: ({entry}: {entry: Entry}) => <article><h2>{entry.title}</h2></article>}))

beforeEach(() => {
  // jsdom does not implement scrolling; the router focuses a #fragment target.
  Element.prototype.scrollIntoView = () => {}
  HTMLDialogElement.prototype.showModal = function() {this.setAttribute('open', '')}
  HTMLDialogElement.prototype.close = function() {this.removeAttribute('open'); this.dispatchEvent(new Event('close'))}
  localStorage.setItem('earnest.csrf', 'token')
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/me') return {user_id: 1, principal_id: 1}
    if (path === '/v1/settings') return {...defaultSettings, rev: 1}
    if (path === '/v1/subscriptions') {
      if (options?.method === 'POST') return {id: 2, feed_id: 43, title: 'New', url: 'https://example.com/feed', deleted: false, folder_id: 3, cadence: 'today', health: {consecutive_failures: 0}}
      return [{id: 8, feed_id: 42, title: 'Rust', url: 'https://feed.test', deleted: false, folder_id: 0, cadence: 'today', health: {consecutive_failures: 0}}]
    }
    if (path === '/v1/folders/3' && options?.method === 'PATCH') return {id: 3, name: 'Updated', parent_id: 0, rev: 2, deleted: false}
    if (path === '/v1/folders') {
      if (options?.method === 'POST') return {id: 7, name: 'Child', parent_id: 3, rev: 1, deleted: false}
      return [{id: 3, name: 'News', parent_id: 0, rev: 1, deleted: false}]
    }
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
const calls = (path: string, method?: string) => vi.mocked(request).mock.calls.filter(([to, options]) => to.startsWith(path) && (!method || options?.method === method))
const bodyOf = (call: [string, RequestInit?]) => JSON.parse(String(call[1]?.body)) as Record<string, unknown>

async function start(path: string) {
  history.replaceState(null, '', path)
  vi.resetModules()
  await import('./main')
}

it('goes home to the front page from the logo, closing an open article', async () => {
  await start('/feeds/8/articles/4')
  await screen.findByRole('heading', {name: 'Article 4'}, {timeout: 5000})
  fireEvent.click(screen.getByRole('link', {name: 'Earnest'}))
  await waitFor(() => expect(location.pathname).toBe('/front'))
  expect(await screen.findByRole('heading', {name: 'Front page list'})).toBeTruthy()
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
  const feed = screen.getByRole('link', {name: 'Rust'})
  fireEvent.contextMenu(feed, {clientX: 30, clientY: 40})
  const item = screen.getByRole('menuitem', {name: 'Mark all as read'})
  expect(document.activeElement).toBe(item)
  fireEvent.keyDown(item, {key: 'Escape'})
  expect(screen.queryByRole('menu')).toBeNull()
  expect(location.pathname).toBe('/feeds/8/articles/4')
  expect(document.activeElement).toBe(feed)
})

it('offers folder management only for a folder', async () => {
  await start('/today')
  const folder = await screen.findByRole('link', {name: 'News'}, {timeout: 5000})
  fireEvent.contextMenu(folder, {clientX: 30, clientY: 40})
  expect(await screen.findByRole('menu', {name: 'News actions'})).toBeTruthy()
  for (const name of ['Mark all as read', 'Rename folder', 'Add feed inside this folder', 'New subfolder', 'Open in new tab']) {
    expect(screen.getByRole('menuitem', {name})).toBeTruthy()
  }
  expect(screen.queryByRole('menuitem', {name: 'Refresh feed'})).toBeNull()
  fireEvent.keyDown(screen.getByRole('menuitem', {name: 'Open in new tab'}), {key: 'Escape'})

  const feed = await screen.findByRole('link', {name: 'Rust'})
  fireEvent.contextMenu(feed, {clientX: 30, clientY: 40})
  expect(await screen.findByRole('menu', {name: 'Rust actions'})).toBeTruthy()
  for (const name of ['Rename folder', 'Add feed inside this folder', 'New subfolder']) {
    expect(screen.queryByRole('menuitem', {name})).toBeNull()
  }
})

it('renames a folder from its menu without moving it', async () => {
  await start('/today')
  const folder = await screen.findByRole('link', {name: 'News'}, {timeout: 5000})
  fireEvent.contextMenu(folder, {clientX: 30, clientY: 40})
  fireEvent.click(await screen.findByRole('menuitem', {name: 'Rename folder'}))
  fireEvent.change(await screen.findByLabelText('Folder name'), {target: {value: 'Updated'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(calls('/v1/folders/3', 'PATCH')).toHaveLength(1))
  expect(bodyOf(calls('/v1/folders/3', 'PATCH')[0])).toEqual({name: 'Updated', op_id: expect.any(String)})
  expect(await screen.findByText('Folder renamed.')).toBeTruthy()
  expect(screen.queryByRole('dialog', {name: 'Rename folder'})).toBeNull()
})

it('creates a subfolder under the folder from its menu', async () => {
  await start('/today')
  const folder = await screen.findByRole('link', {name: 'News'}, {timeout: 5000})
  fireEvent.contextMenu(folder, {clientX: 30, clientY: 40})
  fireEvent.click(await screen.findByRole('menuitem', {name: 'New subfolder'}))
  fireEvent.change(await screen.findByLabelText('Folder name'), {target: {value: 'Child'}})
  fireEvent.click(screen.getByRole('button', {name: 'Create'}))
  await waitFor(() => expect(calls('/v1/folders', 'POST')).toHaveLength(1))
  expect(bodyOf(calls('/v1/folders', 'POST')[0])).toEqual({name: 'Child', parent_id: 3, op_id: expect.any(String)})
  expect(calls('/v1/folders/7', 'PATCH')).toHaveLength(0)
  expect(await screen.findByText('Subfolder created.')).toBeTruthy()
})

it('prefills the subscribe form with the folder and submits its id', async () => {
  await start('/today')
  const folder = await screen.findByRole('link', {name: 'News'}, {timeout: 5000})
  fireEvent.contextMenu(folder, {clientX: 30, clientY: 40})
  fireEvent.click(await screen.findByRole('menuitem', {name: 'Add feed inside this folder'}))
  const input = await screen.findByLabelText('Feed or site URL', {}, {timeout: 5000})
  const subscribe = within(screen.getByRole('region', {name: 'Subscribe'}))
  expect((subscribe.getByLabelText('Folder') as HTMLSelectElement).value).toBe('3')
  fireEvent.change(input, {target: {value: 'https://example.com/feed'}})
  fireEvent.click(screen.getByRole('button', {name: 'Subscribe'}))
  await waitFor(() => expect(calls('/v1/subscriptions', 'POST')).toHaveLength(1))
  expect(bodyOf(calls('/v1/subscriptions', 'POST')[0])).toMatchObject({url: 'https://example.com/feed', folder_id: 3})
})

it('consumes the folder prefill instead of overriding later subscribe drafts', async () => {
  await start('/today')
  const folder = await screen.findByRole('link', {name: 'News'}, {timeout: 5000})
  fireEvent.contextMenu(folder)
  fireEvent.click(await screen.findByRole('menuitem', {name: 'Add feed inside this folder'}))
  const region = await screen.findByRole('region', {name: 'Subscribe'})
  fireEvent.change(within(region).getByLabelText('Folder'), {target: {value: '0'}})
  fireEvent.click(screen.getByRole('link', {name: 'Front page'}))
  await screen.findByRole('heading', {name: 'Front page list'})
  fireEvent.click(screen.getByRole('link', {name: 'Manage feeds'}))
  const again = await screen.findByRole('region', {name: 'Subscribe'})
  expect((within(again).getByLabelText('Folder') as HTMLSelectElement).value).toBe('0')
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
