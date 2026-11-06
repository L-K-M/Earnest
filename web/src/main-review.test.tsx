// @vitest-environment jsdom
import type {ReactElement,ReactNode} from 'react'
import {afterEach,beforeAll,beforeEach,expect,it,vi} from 'vitest'
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import type {QueryClient} from '@tanstack/react-query'
import {request} from './request'
import {clearOffline} from './offline/Offline'
import {defaultSettings} from './Settings'
import type {Entry} from './reader'

const sync = vi.hoisted(() => ({refresh:null as null | ((reason?: 'generation') => void)}))
let app: ReactElement<{client: QueryClient}>
vi.mock('react-dom/client',() => ({createRoot:() => ({render:(node:ReactNode) => {app=node as typeof app}})}))
vi.mock('./request',() => ({request:vi.fn(),setCSRF:vi.fn()}))
vi.mock('./offline/Offline',() => ({OfflineStatus:() => null,registerOffline:vi.fn().mockResolvedValue(undefined),clearOffline:vi.fn()}))
vi.mock('./syncBridge',() => ({useReaderSync:(_identity:unknown,refresh:NonNullable<typeof sync.refresh>) => {sync.refresh=refresh;return {state:(entry:Entry) => entry.state,act:vi.fn(),retry:vi.fn(),status:'Synced',error:''}}}))
vi.mock('./River',() => ({River:() => <main>Today articles</main>}))
vi.mock('./ArticleReader',() => ({Reader:({entry}:{entry:Entry}) => <article>{entry.title}</article>}))

beforeAll(async () => {await import('./main')})
beforeEach(() => {
  app.props.client.clear()
  localStorage.setItem('earnest.csrf','test-session')
  vi.mocked(clearOffline).mockResolvedValue(undefined)
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id:1,principal_id:1}
    if (path === '/v1/session/login') return {csrf_token:'new-test-session'}
    if (path === '/v1/settings') return {...defaultSettings,rev:1}
    if (path === '/v1/notifications/42') return {entry_ids:[7]}
    if (path === '/v1/entries/7') return {id:7,title:'Notification article'}
    return []
  })
})
afterEach(() => {cleanup();app.props.client.clear();localStorage.clear();history.replaceState(null,'','/');vi.resetAllMocks()})

it('consumes only the notification parameter and does not reopen it after signing in again',async () => {
  history.replaceState(null,'','/?notification=42&keep=yes#workspace')
  render(app)
  await screen.findByText('Notification article')
  expect(location.search).toBe('?keep=yes')
  expect(location.hash).toBe('#workspace')
  fireEvent.click(screen.getByRole('button',{name:'Sign out'}))
  await screen.findByRole('button',{name:'Sign in'})
  fireEvent.change(screen.getByLabelText('Username'),{target:{value:'reader'}})
  fireEvent.change(screen.getByLabelText('Password'),{target:{value:'password'}})
  fireEvent.click(screen.getByRole('button',{name:'Sign in'}))
  await screen.findByRole('button',{name:'Sign out'})
  expect(vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/notifications/42')).toHaveLength(1)
  expect(screen.queryByText('Notification article')).toBeNull()
})

it('coalesces sync-driven resource refreshes into one burst', async () => {
  render(app)
  await screen.findByRole('button',{name:'Sign out'})
  const count = () => vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/subscriptions').length
  const before = count()
  act(() => { sync.refresh!(); sync.refresh!(); sync.refresh!() })
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 500)) })
  expect(count()).toBe(before)
  await act(async () => { await waitFor(() => expect(count()).toBe(before + 1), {timeout: 3000}) })
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 600)) })
  expect(count()).toBe(before + 1)
})

it('clears a generation reset error after a successful retry',async () => {
  render(app)
  await screen.findByRole('button',{name:'Sign out'})
  vi.mocked(clearOffline).mockRejectedValueOnce(new Error('Cache temporarily unavailable'))
  act(() => sync.refresh!('generation'))
  expect(await screen.findByRole('alert')).toHaveProperty('textContent','Cache temporarily unavailable Retry')
  fireEvent.click(screen.getByRole('button',{name:'Retry'}))
  await screen.findByRole('button',{name:'Sign out'})
  expect(screen.queryByRole('alert')).toBeNull()
})

it('retries cache clearing after sign-out without repeating session revocation',async () => {
  render(app)
  await screen.findByRole('button',{name:'Sign out'})
  let revocations = 0
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/session/logout' && ++revocations > 1) throw new Error('Session already revoked')
    return []
  })
  vi.mocked(clearOffline).mockRejectedValueOnce(new Error('Could not clear offline copies'))
  fireEvent.click(screen.getByRole('button',{name:'Sign out'}))
  await screen.findByText('Could not clear offline copies')
  expect(vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/session/logout')).toHaveLength(1)
  expect(localStorage.getItem('earnest.csrf')).toBe('test-session')
  fireEvent.click(screen.getByRole('button',{name:'Sign out'}))
  await waitFor(() => expect(screen.getByRole('button',{name:'Sign in'})).toBeTruthy())
  expect(vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/session/logout')).toHaveLength(1)
})
