// @vitest-environment jsdom
import {useState} from 'react'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import type {Entry, EntryState, ReaderActions, View} from './reader'
import {request} from './request'

vi.mock('./request', () => ({request: vi.fn()}))
vi.mock('./agents/Surfaces', () => ({TodayAgents: () => null, SessionChanges: () => null}))
vi.mock('./pipeline/Pipeline', () => ({Edition: () => null}))
vi.mock('./search/pipeline', () => ({usePipeline: () => ({status: {blurb: 'disabled'}})}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count}: {count: number}) => ({
  getTotalSize: () => count * 100,
  getVirtualItems: () => Array.from({length: count}, (_, index) => ({index, size: 100, start: index * 100})),
  scrollToIndex: vi.fn(), options: {scrollMargin: 0}, measureElement: vi.fn(),
})}))

afterEach(() => {cleanup(); vi.clearAllMocks()})
const article = (id: number, seen = 'unseen', shelf = 'none') => ({
  id, seq: id, title: `Article ${id}`, subscription_id: 1, deleted: false,
  state: {principal: 1, entry_id: id, state_rev: 0, seen: {value: seen, rev: 0}, shelf: {value: shelf, rev: 0}, kept: {value: false, rev: 0}},
}) as Entry
const feed: View = {kind: 'feed', id: 1, title: 'Example feed'}

function mount(view: View = feed, unreadOnly = true) {
  let rows = [article(1, 'seen'), article(2), article(3, 'unseen', 'done')]
  const states = new Map<number, EntryState>()
  const deleted = new Set<number>()
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  vi.mocked(request).mockImplementation(async path => path.startsWith('/v1/positions') ? {seq: 0} : {entries: rows})
  const actions: ReaderActions = {act: vi.fn().mockResolvedValue(undefined), state: entry => states.get(entry.id) ?? entry.state, deleted: entry => entry.deleted || deleted.has(entry.id)}
  const onOpen = vi.fn()
  function Harness({visit, reading, selected}: {visit: number; reading: boolean; selected: Entry | null}) {
    const [mode, setMode] = useState(unreadOnly)
    return <QueryClientProvider client={client}><River key={visit} view={view} subscriptions={[]} actions={actions} selected={selected} onSelect={vi.fn()} onOpen={onOpen} reading={reading} shortcuts counts={false} unreadOnly={mode} onUnreadOnlyChange={setMode} summary={{seen: 0, done: 0, later: 0, kept: 0}} /></QueryClientProvider>
  }
  function tree(visit = 0, reading = false, selected: Entry | null = null) {
    return <Harness visit={visit} reading={reading} selected={selected} />
  }
  const rendered = render(tree())
  return {client, states, deleted, actions, onOpen, tree, ...rendered, setRows: (next: Entry[]) => {rows = next}}
}

describe('unread-only list visits', () => {
  it.each([feed, {kind: 'folder', id: 4, title: 'Folder'}, {kind: 'label', id: 2, title: 'Label'}, {kind: 'edition', title: 'Edition'}] as View[])('hides already read or Done articles when $kind loads', async view => {
    mount(view)
    await screen.findByText('Article 2')
    expect(screen.queryByText('Article 1')).toBeNull()
    expect(screen.queryByText('Article 3')).toBeNull()
    expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('unread_only=true'))).toBe(true)
  })

  it.each([feed, {kind: 'today', title: 'Today'}, {kind: 'folder', id: 4, title: 'Folder'}, {kind: 'label', id: 2, title: 'Label'}, {kind: 'edition', title: 'Edition'}] as View[])('switches between unread and all in the $kind list header', async view => {
    const {actions, onOpen} = mount(view)
    await screen.findByText('Article 2')
    expect(screen.getByRole('group', {name: 'Article visibility'})).toBeTruthy()
    expect(screen.getByRole('button', {name: 'Show only unread', pressed: true})).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: 'Show all', pressed: false}))
    await screen.findByText('Article 1')
    expect(screen.getByText('Article 3')).toBeTruthy()
    expect(screen.getByRole('button', {name: 'Show all', pressed: true})).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: 'Show only unread', pressed: false}))
    await waitFor(() => expect(screen.queryByText('Article 1')).toBeNull())
    expect(screen.queryByText('Article 3')).toBeNull()
    expect(screen.getByText('Article 2')).toBeTruthy()
    expect(actions.act).not.toHaveBeenCalled()
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('starts a fresh unread snapshot on switching modes without losing grouping', async () => {
    const {states, setRows, rerender, tree, actions, onOpen} = mount()
    await screen.findByText('Article 2')
    fireEvent.click(screen.getByRole('button', {name: 'One per story'}))
    await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('group=cluster'))).toBe(true))
    await screen.findByText('Article 2')
    states.set(2, article(2, 'seen').state)
    rerender(tree(0, true, article(2)))
    expect(screen.getByText('Article 2')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: 'Show all'}))
    await screen.findByText('Article 1')
    setRows([article(1, 'seen'), article(2, 'seen'), article(4)])
    fireEvent.click(screen.getByRole('button', {name: 'Show only unread'}))
    // Reusing the unread query must still fetch changes made while in All.
    await screen.findByText('Article 4')
    expect(screen.queryByText('Article 2')).toBeNull()
    expect(screen.getByRole('button', {name: 'One per story', pressed: true})).toBeTruthy()
    expect(vi.mocked(request).mock.calls.filter(([path]) => path.includes('group=cluster') && path.includes('unread_only=true'))).toHaveLength(2)
    expect(actions.act).not.toHaveBeenCalled()
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('flushes an opened article even when the new filter page omits it', async () => {
    const {actions} = mount(feed, false)
    fireEvent.click(await screen.findByText('Article 2'))
    vi.mocked(request).mockImplementation(async path => path.startsWith('/v1/positions') ? {seq: 0} : {entries: [article(4)], next_cursor: 'older'})
    fireEvent.click(screen.getByRole('button', {name: 'Show only unread'}))
    await screen.findByText('Article 4')
    await waitFor(() => expect(actions.act).toHaveBeenCalledExactlyOnceWith(article(2), 'seen', 'seen', 'passive'), {timeout: 1500})
    await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path, options]) => path.startsWith('/v1/positions') && options?.method === 'PUT' && JSON.parse(String(options.body)).seq === 2)).toBe(true))
  })

  it.each(['remote unread', 'deletion'])('cancels a filtered-out pending open after %s', async reason => {
    const {actions, states, deleted} = mount(feed, false)
    fireEvent.click(await screen.findByText('Article 2'))
    if (reason === 'remote unread') {
      const state = article(2).state
      states.set(2, {...state, seen: {...state.seen, value: 'unseen', rev: state.seen.rev + 1}})
    } else {
      deleted.add(2)
    }
    vi.mocked(request).mockImplementation(async path => path.startsWith('/v1/positions') ? {seq: 0} : {entries: [article(4)], next_cursor: 'older'})
    fireEvent.click(screen.getByRole('button', {name: 'Show only unread'}))
    await screen.findByText('Article 4')
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(actions.act).not.toHaveBeenCalled()
  })

  it('can leave a failed unread filter for the all-articles view', async () => {
    mount(feed, false)
    await screen.findByText('Article 1')
    vi.mocked(request).mockImplementation(async path => {
      if (path.includes('unread_only=true')) throw new Error('Unread list unavailable')
      return path.startsWith('/v1/positions') ? {seq: 0} : {entries: [article(1, 'seen'), article(2)]}
    })
    fireEvent.click(screen.getByRole('button', {name: 'Show only unread'}))
    await screen.findByRole('alert')
    fireEvent.click(screen.getByRole('button', {name: 'Show all'}))
    await screen.findByText('Article 1')
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('keeps newly read articles through reader open, selection changes and closing', async () => {
    const {states, rerender, tree} = mount()
    const entry = article(2)
    await screen.findByText('Article 2')
    states.set(2, {...entry.state, seen: {...entry.state.seen, value: 'seen', rev: 1}})
    rerender(tree(0, true, entry))
    expect(screen.getByText('Article 2').closest('a')?.hasAttribute('data-unread')).toBe(false)
    rerender(tree(0, true, article(4)))
    rerender(tree())
    expect(screen.getByText('Article 2')).toBeTruthy()
  })

  it('retains admitted articles when a background refetch omits them', async () => {
    const {client, states, setRows} = mount({kind: 'today', title: 'Today'})
    await screen.findByText('Article 2')
    const entry = article(2, 'seen')
    states.set(2, entry.state)
    setRows([])
    await act(async () => {await client.invalidateQueries()})
    expect(screen.getByText('Article 2')).toBeTruthy()
    await waitFor(() => expect(screen.getByText('Article 2').closest('a')?.hasAttribute('data-unread')).toBe(false))
  })

  it('honors sync tombstones without keeping deleted metadata for the visit', async () => {
    const {deleted, rerender, tree} = mount()
    await screen.findByText('Article 2')
    deleted.add(2)
    rerender(tree())
    expect(screen.queryByText('Article 2')).toBeNull()
  })

  it('prunes newly read articles only after a successful explicit refresh', async () => {
    const {states, setRows} = mount()
    await screen.findByText('Article 2')
    states.set(2, article(2, 'seen').state)
    setRows([article(1, 'seen'), article(2, 'seen')])
    fireEvent.click(screen.getByRole('button', {name: 'Refresh articles'}))
    await waitFor(() => expect(screen.queryByText('Article 2')).toBeNull())
    expect(screen.queryByText('Article 1')).toBeNull()
  })

  it('keeps the current visit if an explicit refresh fails', async () => {
    mount()
    await screen.findByText('Article 2')
    vi.mocked(request).mockRejectedValueOnce(new Error('Offline'))
    fireEvent.click(screen.getByRole('button', {name: 'Refresh articles'}))
    await screen.findByRole('alert')
    expect(screen.getByText('Article 2')).toBeTruthy()
  })

  it('reapplies eligibility on a new parent-list visit, even with cached query data', async () => {
    const {states, rerender, tree} = mount()
    await screen.findByText('Article 2')
    states.set(2, article(2, 'seen').state)
    rerender(tree(1))
    expect(screen.queryByText('Article 2')).toBeNull()
  })

  it('admits unread articles on later pages without pruning earlier read rows', async () => {
    const {states, client} = mount()
    await screen.findByText('Article 2')
    states.set(2, article(2, 'seen').state)
    vi.mocked(request).mockImplementation(async path => {
      if (path.startsWith('/v1/positions')) return {seq: 0}
      return path.includes('cursor=next') ? {entries: [article(4, 'seen'), article(5)]} : {entries: [article(2, 'seen')], next_cursor: 'next'}
    })
    // A background refresh can reveal another page, without ending the visit.
    await act(async () => {await client.invalidateQueries({queryKey: ['river']})})
    fireEvent.click(await screen.findByRole('button', {name: 'More articles'}))
    expect(await screen.findByText('Article 5')).toBeTruthy()
    expect(screen.getByText('Article 2')).toBeTruthy()
    expect(screen.queryByText('Article 4')).toBeNull()
  })

  it('keeps each representation’s admitted rows when toggling grouping', async () => {
    const {states} = mount()
    await screen.findByText('Article 2')
    fireEvent.click(screen.getByRole('button', {name: 'One per story'}))
    await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('group=cluster'))).toBe(true))
    await screen.findByText('Article 2')
    states.set(2, article(2, 'seen').state)
    fireEvent.click(screen.getByRole('button', {name: 'One per story'}))
    expect(await screen.findByText('Article 2')).toBeTruthy()
  })

  it('keeps a read story representative when a background fetch selects another article in that story', async () => {
    const {client, states} = mount()
    await screen.findByText('Article 2')
    vi.mocked(request).mockResolvedValue({entries: [article(2)], groups: [{entry: article(2), cluster_id: 9, also_in: 1}]})
    fireEvent.click(screen.getByRole('button', {name: 'One per story'}))
    await screen.findByText('Feed · also in 1')
    states.set(2, article(2, 'seen').state)
    vi.mocked(request).mockResolvedValue({entries: [article(4)], groups: [{entry: article(4), cluster_id: 9, also_in: 1}]})
    await act(async () => {await client.invalidateQueries({queryKey: ['river']}); await new Promise(resolve => setTimeout(resolve, 20))})
    expect(screen.getByText('Article 2')).toBeTruthy()
    expect(screen.queryByText('Article 4')).toBeNull()
    expect(screen.getByText('Feed · also in 1')).toBeTruthy()
  })

  it.each(['later', 'kept', 'done'] as const)('does not filter the %s shelf', async kind => {
    mount({kind, title: kind})
    expect(await screen.findByText('Article 1')).toBeTruthy()
    expect(screen.getByText('Article 3')).toBeTruthy()
    expect(screen.queryByRole('group', {name: 'Article visibility'})).toBeNull()
  })

  it('leaves the default all-articles mode unchanged', async () => {
    mount(feed, false)
    expect(await screen.findByText('Article 1')).toBeTruthy()
    expect(screen.getByText('Article 2')).toBeTruthy()
    expect(screen.getByText('Article 3')).toBeTruthy()
  })
})
