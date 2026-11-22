// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import {useReaderSync} from './syncBridge'
import type {Entry, ReaderActions, View} from './reader'
import {request, requestText} from './request'

vi.mock('./request', () => ({request: vi.fn(), requestText: vi.fn()}))
vi.mock('./agents/Surfaces', () => ({TodayAgents: () => null, SessionChanges: () => null}))
vi.mock('./pipeline/Pipeline', () => ({Edition: () => null}))
vi.mock('./search/pipeline', () => ({usePipeline: () => ({status: {blurb: 'disabled'}})}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count}: {count: number}) => ({
  getTotalSize: () => count * 100,
  getVirtualItems: () => Array.from({length: count}, (_, index) => ({index, size: 100, start: index * 100})),
  scrollToIndex: vi.fn(), options: {scrollMargin: 0}, measureElement: vi.fn(),
})}))
// The real runtime and pending overlay, kept in memory.
vi.mock('./sync/runtime', async original => {
  const actual = await original<typeof import('./sync/runtime')>()
  const {MemorySyncStore} = await import('./sync/store')
  class ReaderSync extends actual.ReaderSync {
    constructor(options: ConstructorParameters<typeof actual.ReaderSync>[0]) {
      super({...options, store: new MemorySyncStore(), locks: new actual.LocalLocks(), channel: null, openEvents: () => null})
    }
  }
  return {...actual, ReaderSync}
})

const identity = {user_id: 1, principal_id: 1}
const feed: View = {kind: 'feed', id: 1, title: 'Example feed'}
const time = '2026-11-01T00:00:00Z'
const register = (value: string | boolean) => ({value, rev: 0, occurred_at: time, actor: 1, source: 'passive'})
const article = {
  id: 1, seq: 1, rev: 1, title: 'Article 1', subscription_id: 1, feed_id: 1, url: 'https://news.test/1', author: '', deleted: false,
  first_seen_at: time, published_at: time,
  state: {principal: 1, entry_id: 1, state_rev: 0, seen: register('unseen'), shelf: register('none'), kept: register(false)},
} as Entry

afterEach(() => {cleanup(); vi.clearAllMocks(); vi.unstubAllGlobals()})

// A write that reaches the outbox leaves the register's revision unchanged,
// so only a test on the real overlay sees the race.
it('keeps an unread from outside the list over the open that is waiting to be saved', async () => {
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), {locks: {}}))
  vi.stubGlobal('indexedDB', {})
  vi.mocked(request).mockImplementation(async path => path.startsWith('/v1/positions') ? {seq: 0} : {entries: [article]})
  vi.mocked(requestText).mockImplementation(async path => {
    if (path === '/v1/bootstrap') return '{"generation":"g1","rev":0}'
    if (path.startsWith('/v1/bootstrap/')) return '{"items":[],"has_more":false}'
    if (path.startsWith('/v1/state:batch')) return new Promise<string>(() => {}) // sync stays queued
    return '{"changes":[],"next":{"generation":"g1","rev":0}}'
  })
  function Harness() {
    const sync = useReaderSync(identity, () => {})
    const actions: ReaderActions = {act: sync.act, state: sync.state, seenChoices: sync.seenChoices}
    return <>
      <p>{sync.status}</p>
      <output>{sync.state(article).seen.value}</output>
      <button type="button" onClick={() => {void sync.act(article, 'seen', 'unseen')}}>Reader toolbar unread</button>
      <River view={feed} subscriptions={[]} actions={actions} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen: 0, done: 0, later: 0, kept: 0}} />
    </>
  }
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  const view = render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>)
  const seen = () => view.container.querySelector('output')?.textContent

  await screen.findByText('Reading actions synced.')
  fireEvent.click(await screen.findByText('Article 1'))
  fireEvent.click(screen.getByText('Reader toolbar unread'))
  await waitFor(() => expect(screen.getByText(/waiting to sync/)).toBeTruthy())
  await new Promise(resolve => window.setTimeout(resolve, 1300))
  expect(seen()).toBe('unseen')

  // Opening it again is a new intent.
  fireEvent.click(screen.getByText('Article 1'))
  await waitFor(() => expect(seen()).toBe('seen'), {timeout: 1500})
})
