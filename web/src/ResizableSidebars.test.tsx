// @vitest-environment jsdom
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {act, cleanup, fireEvent, render, screen} from '@testing-library/react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {App} from './App'
import {defaultSettings} from './Settings'
import {request} from './request'
import {resetLocation} from './router'
import type {Entry, View} from './reader'

vi.mock('./request', async original => ({...await original<typeof import('./request')>(), request: vi.fn(), setCSRF: vi.fn()}))
vi.mock('./syncBridge', () => ({useReaderSync: () => ({state: (entry: Entry) => entry.state, retry: vi.fn(), status: 'Synced', error: ''})}))
vi.mock('./offline/Offline', () => ({OfflineStatus: () => null, useOnline: () => true, clearOffline: vi.fn()}))
vi.mock('./River', () => ({River: ({view}: {view: View}) => <h2>{view.title} articles</h2>}))
vi.mock('./FrontPage', () => ({FrontPage: () => <h2>Front page articles</h2>}))

const media = new Map<string, EventTarget>()
function resize(width: number) {
  vi.stubGlobal('innerWidth', width)
  window.dispatchEvent(new Event('resize'))
  for (const list of media.values()) list.dispatchEvent(new Event('change'))
}
beforeEach(() => {
  vi.stubGlobal('matchMedia', (query: string) => {
    if (!media.has(query)) {
      const list = new EventTarget()
      Object.defineProperty(list, 'matches', {get: () => window.innerWidth <= Number(query.match(/\d+/)?.[0])})
      media.set(query, list)
    }
    return media.get(query)
  })
  resize(1600)
  localStorage.setItem('earnest.csrf', 'token')
  resetLocation('/front')
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id: 1, principal_id: 1}
    if (path === '/v1/settings') return {...defaultSettings, rev: 1}
    if (path === '/v1/subscriptions') return [{id: 8, feed_id: 42, title: 'Rust', url: 'https://feed.test', deleted: false, folder_id: 0, cadence: 'today', health: {consecutive_failures: 0}}]
    if (path === '/v1/pipeline') return {ask: 'disabled'}
    return []
  })
})
afterEach(() => {cleanup(); localStorage.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals(); media.clear()})

async function start() {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  const view = render(<QueryClientProvider client={client}><App /></QueryClientProvider>)
  await screen.findByRole('heading', {name: 'Front page articles'})
  return view
}

it('connects both accessible dividers to independent, persisted layout widths', async () => {
  const view = await start()
  const navigation = screen.getByRole('separator', {name: 'Navigation width'})
  const list = screen.getByRole('separator', {name: 'Article list width'})
  expect(document.getElementById(navigation.getAttribute('aria-controls')!)).toBeTruthy()
  expect(document.getElementById(list.getAttribute('aria-controls')!)).toBeTruthy()
  fireEvent.keyDown(navigation, {key: 'ArrowRight'})
  fireEvent.keyDown(list, {key: 'ArrowLeft'})
  const app = view.container.querySelector<HTMLElement>('.app')!
  expect(app.style.getPropertyValue('--sidebar')).toBe('264px')
  expect(app.style.getPropertyValue('--list-width')).toBe('416px')
  expect(JSON.parse(localStorage.getItem('earnest.pane-widths')!)).toEqual({navigation: 264, list: 416})
  fireEvent.doubleClick(navigation)
  fireEvent.keyDown(list, {key: 'Enter'})
  expect(app.style.getPropertyValue('--sidebar')).toBe('248px')
  expect(app.style.getPropertyValue('--list-width')).toBe('432px')
})

it('only exposes handles for visible side-by-side panes at each breakpoint', async () => {
  await start()
  expect(screen.getAllByRole('separator')).toHaveLength(2)
  fireEvent.keyDown(screen.getByRole('separator', {name: 'Article list width'}), {key: 'End'})
  act(() => resize(900))
  expect(screen.queryByRole('separator', {name: 'Navigation width'})).toBeNull()
  expect(screen.getByRole('separator', {name: 'Article list width'}).getAttribute('aria-valuenow')).toBe('580')
  expect(screen.getByRole('button', {name: 'Menu'})).toBeTruthy()
  act(() => resize(390))
  expect(screen.queryAllByRole('separator')).toHaveLength(0)
  act(() => resize(1600))
  expect(screen.getAllByRole('separator')).toHaveLength(2)
  expect(screen.getByRole('separator', {name: 'Article list width'}).getAttribute('aria-valuenow')).toBe('720')
})
