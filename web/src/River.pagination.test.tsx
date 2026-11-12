// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import type {Entry, View} from './reader'
import {request} from './request'

vi.mock('./request', () => ({request: vi.fn()}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count}: {count: number}) => ({
  getTotalSize: () => count * 100,
  getVirtualItems: () => Array.from({length: count}, (_, index) => ({index, size: 100, start: index * 100})),
  scrollToIndex: vi.fn(), options: {scrollMargin: 0}, measureElement: vi.fn(),
})}))

afterEach(() => {cleanup(); vi.resetAllMocks()})

function entry(seq: number): Entry {
  return {id: seq, seq, title: `Article ${seq}`, subscription_id: 7, deleted: false,
    state: {principal: 1, entry_id: seq, state_rev: 0, seen: {value: 'unseen', rev: 0}, shelf: {value: 'none', rev: 0}, kept: {value: false, rev: 0}},
  } as Entry
}

type Page = {entries: Entry[]; next_cursor?: string}
function serve(page: (query: URLSearchParams) => Page, position: () => Promise<{seq: number}> = async () => ({seq: 9999})) {
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/pipeline') return {blurb: 'disabled'}
    if (path.startsWith('/v1/positions/')) return position()
    if (path.startsWith('/v1/entries?')) return page(new URL(path, 'https://earnest.test').searchParams)
    throw new Error(`Unexpected request: ${path}`)
  })
}

function mount(view: View = {kind: 'folder', id: 7, title: 'Folder'}) {
  const write = vi.fn().mockResolvedValue(undefined)
  render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}>
    <River view={view} subscriptions={[]} actions={{act: write, state: entry => entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts={false} counts summary={{seen: 0, done: 0, later: 0, kept: 0}} />
  </QueryClientProvider>)
  return write
}

function queries(): URLSearchParams[] {
  return vi.mocked(request).mock.calls.map(([path]) => path).filter(path => path.startsWith('/v1/entries?'))
    .map(path => new URL(path, 'https://earnest.test').searchParams)
}
function listed(): number[] {
  return [...document.querySelectorAll<HTMLElement>('[data-entry]')].map(row => Number(row.dataset.entry))
}

it.each(['ahead', 'pending', 'failed'] as const)('loads newest articles independently of an %s position read', async state => {
  serve(() => ({entries: [entry(103), entry(102), entry(101)]}), async () => {
    if (state === 'pending') return new Promise(() => {})
    if (state === 'failed') throw new Error('Position unavailable')
    return {seq: 9999}
  })
  const write = mount()
  await screen.findByText('Article 103')
  expect(listed()).toEqual([103, 102, 101])
  expect(screen.getByText('3 unread in loaded articles')).toBeTruthy()
  expect(screen.getByLabelText('Article list').getAttribute('aria-busy')).toBeNull()
  expect(queries()).toHaveLength(1)
  expect(Object.fromEntries(queries()[0])).toEqual({view: 'all', limit: '100', direction: 'backward', folder_id: '7'})
  expect(write).not.toHaveBeenCalled()
})

it('loads All articles newest-first through the existing reader actions', async () => {
  serve(() => ({entries: [entry(202), entry(201), entry(200)]}))
  const write = mount({kind: 'all', title: 'All articles'})
  await screen.findByText('Article 202')
  expect(listed()).toEqual([202, 201, 200])
  expect(Object.fromEntries(queries()[0])).toEqual({view: 'all', limit: '100', direction: 'backward'})
  expect(write).not.toHaveBeenCalled()
})

it.each([false, true])('appends older pages using backward cursors (grouped: %s)', async grouped => {
  serve(query => ({entries: query.has('cursor') ? [entry(101), entry(100)] : [entry(103), entry(102)], next_cursor: query.has('cursor') ? undefined : '102'}))
  mount()
  await screen.findByText('Article 103')
  if (grouped) {
    fireEvent.click(screen.getByRole('button', {name: 'One per story'}))
    await waitFor(() => expect(queries().at(-1)?.get('group')).toBe('cluster'))
  }
  fireEvent.click(await screen.findByRole('button', {name: 'More articles'}))
  await screen.findByText('Article 100')
  expect(listed()).toEqual([103, 102, 101, 100])
  expect(new Set(listed()).size).toBe(4)
  expect(Object.fromEntries(queries().at(-1)!)).toEqual({view: 'all', limit: '100', direction: 'backward', folder_id: '7', cursor: '102', ...(grouped ? {group: 'cluster'} : {})})
  expect(screen.queryByRole('button', {name: 'More articles'})).toBeNull()
  expect(screen.queryByRole('button', {name: 'Earlier articles'})).toBeNull()
})

it('refreshes from the newest page and follows its updated older-page cursor', async () => {
  let newest = 103
  serve(query => {
    const first = query.has('cursor') ? Number(query.get('cursor')) - 1 : newest
    return {entries: [entry(first), entry(first - 1)], next_cursor: query.has('cursor') ? undefined : String(first - 1)}
  })
  mount()
  fireEvent.click(await screen.findByRole('button', {name: 'More articles'}))
  await screen.findByText('Article 100')
  expect(listed()).toEqual([103, 102, 101, 100])
  newest = 104
  fireEvent.click(screen.getByRole('button', {name: 'Refresh articles'}))
  await screen.findByText('Article 104')
  await waitFor(() => expect(listed()).toEqual([104, 103, 102, 101]))
  expect(queries().map(query => query.get('cursor'))).toEqual([null, '102', null, '103'])
  expect(queries().every(query => query.get('direction') === 'backward' && !query.has('after_seq'))).toBe(true)
})
