// @vitest-environment jsdom
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import type {Entry, EntryState, ReaderActions} from './reader'
import type {FrontPage as FrontPageData, FrontStory} from './front'
import {request} from './request'
import {FrontPage} from './FrontPage'
import {frontCacheKey} from './front-store'

const {online} = vi.hoisted(() => ({online: {value: true}}))
vi.mock('./request', () => ({request: vi.fn()}))
vi.mock('./offline/Offline', () => ({useOnline: () => online.value}))
vi.mock('./agents/Surfaces', () => ({TodayAgents: () => null, SessionChanges: () => null}))

function state(value: 'seen' | 'unseen' = 'unseen', shelf: 'none' | 'later' | 'done' = 'none'): EntryState {
  const register = (v: 'seen' | 'unseen' | 'none' | 'later' | 'done' | boolean) => ({value: v, rev: 1, actor: 1, occurred_at: '2026-11-12T00:00:00Z', source: ''})
  return {principal: 1, entry_id: 0, state_rev: 1, seen: register(value), shelf: register(shelf), kept: register(false)}
}
function entry(id: number, title = `Story ${id}`, overrides: Partial<Entry> = {}): Entry {
  return {id, title, url: `https://example.test/${id}`, subscription_id: 8, feed_id: 42, deleted: false, state: state(), ...overrides} as Entry
}
function story(id: string, title: string, overrides: Partial<FrontStory> = {}): FrontStory {
  const first = entry(Number(id.replace(/\D/g, '')) || 1, title)
  return {
    id,
    entry: first,
    source_title: 'Lead Source',
    published_at: '2026-11-12T09:00:00Z',
    feed_count: 1,
    excerpt: 'An attributed excerpt.',
    coverage: [{entry: first, source_title: 'Lead Source'}],
    ...overrides,
  }
}
function page(revision: string, stories: FrontStory[], name = 'Across your feeds'): FrontPageData {
  return {
    period: 'daily',
    window_start: '2026-11-11T10:00:00Z',
    window_end: '2026-11-12T10:00:00Z',
    selected_at: '2026-11-12T10:00:00Z',
    revision,
    sections: [{name, stories}],
  }
}

let current: FrontPageData
beforeEach(() => {
  current = page('rev-1', [story('entry:1', 'Lead story'), story('entry:2', 'Second story')])
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/front-page?period=daily') return current
    if (path === '/v1/front-page?period=weekly') return {...current, period: 'weekly'}
    throw new Error(`Unexpected request: ${path}`)
  })
})
afterEach(() => {cleanup(); vi.clearAllMocks(); localStorage.clear(); online.value = true})

function mount(period: 'daily' | 'weekly' = 'daily', userID = 1, actions?: ReaderActions) {
  const write = vi.fn().mockResolvedValue(undefined)
  const reader: ReaderActions = actions ?? {act: write, state: value => value.state, deleted: () => false}
  const onOpen = vi.fn()
  const onSelect = vi.fn()
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  const view = render(<QueryClientProvider client={client}><FrontPage period={period} userID={userID} actions={reader} selected={null} onSelect={onSelect} onOpen={onOpen} since="2026-01-01T00:00:00Z" sessionStart="2026-01-01T00:00:00Z" /></QueryClientProvider>)
  return {client, onOpen, onSelect, write, actions: reader, container: view.container}
}

describe('front page rendering', () => {
  it('keeps the server ordering, marks the lead, and labels the windows', async () => {
    mount()
    expect(await screen.findByText('Lead story')).toBeTruthy()
    const cards = document.querySelectorAll('.front-story')
    expect(cards).toHaveLength(2)
    expect(cards[0].classList.contains('lead')).toBe(true)
    expect(cards[0].getAttribute('data-story')).toBe('entry:1')
    expect(cards[1].classList.contains('lead')).toBe(false)
    expect(screen.getByText('Across your feeds')).toBeTruthy()
    expect(screen.getByRole('link', {name: /Past 24 hours/}).getAttribute('href')).toBe('/front')
    expect(screen.getByRole('link', {name: /Past 7 days/}).getAttribute('href')).toBe('/front?period=weekly')
    expect(screen.getByRole('link', {name: /Past 24 hours/}).getAttribute('aria-current')).toBe('page')
  })

  it('shows source, time and feed count without inventing a ranking', async () => {
    current = page('rev-1', [story('entry:3', 'Shared story', {source_title: 'Field Notes', feed_count: 4, published_at: '2026-11-12T08:30:00Z'})])
    mount()
    await screen.findByText('Shared story')
    expect(screen.getByText('Field Notes')).toBeTruthy()
    expect(screen.getByText('Covered by 4 feeds')).toBeTruthy()
    expect(document.querySelector('.front-meta time')?.getAttribute('dateTime')).toBe('2026-11-12T08:30:00Z')
  })

  it('shows the attributed excerpt when there is no cached summary', async () => {
    mount()
    expect(await screen.findAllByText('An attributed excerpt.')).toHaveLength(2)
    expect(screen.getAllByText('Excerpt from Lead Source')).toHaveLength(2)
  })

  it('renders a cached summary with provenance and expandable passages', async () => {
    current = page('rev-1', [story('entry:4', 'Summarized story', {
      summary: {
        model: 'test-model',
        prompt_version: 'front-page-v1',
        generated_at: '2026-11-12T10:05:00Z',
        excerpt_notice: 'Some sources gave excerpts only.',
        claims: [{text: 'A grounded sentence.', verified: true, citations: [{entry_id: 4, content_hash: 'h', verified: true, passages: ['The exact passage.']}]}],
      },
    })])
    mount()
    await screen.findByText('A grounded sentence.')
    expect(screen.getByText(/Summary of linked coverage/)).toBeTruthy()
    expect(screen.getByText('Some sources gave excerpts only.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: 'Citations'}))
    expect(screen.getByText('The exact passage.')).toBeTruthy()
  })

  it('expands coverage links to the other originals without opening them', async () => {
    const first = entry(5, 'Representative headline')
    const second = entry(6, 'Other original headline')
    current = page('rev-1', [story('entry:5', 'Representative headline', {
      coverage: [{entry: first, source_title: 'First Source'}, {entry: second, source_title: 'Second Source'}],
      feed_count: 2,
    })])
    const {onOpen} = mount()
    await screen.findByText('Representative headline')
    expect(screen.queryByText(/Second Source/)).toBeNull()
    fireEvent.click(screen.getByRole('button', {name: /Other sources/}))
    expect(screen.getByText(/Second Source/)).toBeTruthy()
    expect(onOpen).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('link', {name: /Second Source/}))
    expect(onOpen).toHaveBeenCalledWith(second)
  })

  it.each([
    [{seen: 'seen', shelf: 'none'}, {seen: 'unseen', shelf: 'done'}, true],
    [{seen: 'seen', shelf: 'none'}, {seen: 'unseen', shelf: 'none'}, false],
  ] as const)('marks a story read only when every coverage article is seen or Done', async (first, second, read) => {
    const a = entry(7, 'Partial story')
    const b = entry(8, 'Other original')
    current = page('rev-1', [story('entry:7', 'Partial story', {
      coverage: [{entry: a, source_title: 'First'}, {entry: b, source_title: 'Second'}],
      feed_count: 2,
    })])
    const states = new Map<number, EntryState>([[7, state(first.seen, first.shelf)], [8, state(second.seen, second.shelf)]])
    mount('daily', 1, {act: vi.fn(), state: value => states.get(value.id) ?? value.state, deleted: () => false})
    await screen.findByText('Partial story')
    if (read) expect(screen.getByText('Read')).toBeTruthy()
    else expect(screen.queryByText('Read')).toBeNull()
  })

  it('renders text only when the picture is absent or fails', async () => {
    current = page('rev-1', [story('entry:9', 'No picture', {image_url: undefined})])
    const {container} = render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}><FrontPage period="daily" userID={1} actions={{act: vi.fn(), state: value => value.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} since="" sessionStart="" /></QueryClientProvider>)
    await screen.findByText('No picture')
    expect(container.querySelector('img.front-image')).toBeNull()
    cleanup()

    current = page('rev-1', [story('entry:10', 'Broken picture', {image_url: '/v1/entries/10/image?v=hash'})])
    const {container: brokenContainer} = mount()
    await screen.findByText('Broken picture')
    const image = brokenContainer.querySelector('img.front-image')
    expect(image?.getAttribute('src')).toBe('/v1/entries/10/image?v=hash')
    fireEvent.error(image!)
    await waitFor(() => expect(brokenContainer.querySelector('img.front-image')).toBeNull())
  })
})

describe('front page opening and refresh', () => {
  it('opens the representative and never writes during render, tab switch or coverage expansion', async () => {
    const first = entry(11, 'Openable story')
    const second = entry(12, 'Coverage story')
    current = page('rev-1', [story('entry:11', 'Openable story', {
      coverage: [{entry: first, source_title: 'First'}, {entry: second, source_title: 'Second'}],
      feed_count: 2,
    })])
    const {onOpen, write} = mount()
    await screen.findByText('Openable story')
    expect(write).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', {name: /Other sources/}))
    expect(write).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('link', {name: /Coverage story/}).closest('a')!)
    expect(write).not.toHaveBeenCalled()
    expect(onOpen).toHaveBeenCalledWith(second)
    fireEvent.click(screen.getByRole('link', {name: 'Openable story'}))
    expect(onOpen).toHaveBeenCalledWith(first)
  })

  it('applies new coverage after one explicit refresh', async () => {
    mount()
    await screen.findByText('Lead story')
    current = page('rev-2', [story('entry:3', 'Updated lead')])
    fireEvent.click(screen.getByRole('button', {name: 'Refresh'}))
    await screen.findByText('Updated lead')
    expect(screen.queryByText('Lead story')).toBeNull()
    expect(screen.queryByText('New coverage available')).toBeNull()
  })

  it('removes a deleted source’s image, excerpt and summary while retaining other coverage', async () => {
    const first = entry(1, 'Deleted original')
    const second = entry(2, 'Remaining original', {feed_id: 43})
    current = page('rev-1', [story('cluster:1', 'Deleted original', {
      entry: first, image_url: '/v1/entries/1/image', feed_count: 2,
      coverage: [{entry: first, source_title: 'Removed source'}, {entry: second, source_title: 'Remaining source'}],
      summary: {model: 'test', prompt_version: 'v1', generated_at: '2026-11-12T10:00:00Z',
        claims: [{text: 'Summary from removed source', verified: true, citations: [{entry_id: 1, content_hash: 'hash', passages: ['Removed passage'], verified: true}]}]},
    })])
    mount('daily', 1, {act: vi.fn(), state: value => value.state, deleted: value => value.id === 1})
    await screen.findByText('Remaining original')
    expect(screen.queryByText('Summary from removed source')).toBeNull()
    expect(screen.queryByText('An attributed excerpt.')).toBeNull()
    expect(document.querySelector('.front-image')).toBeNull()
    expect(screen.queryByText('Covered by 2 feeds')).toBeNull()
    expect(screen.getByText('Remaining source')).toBeTruthy()
  })

  it('holds the visit order until New coverage available is explicitly refreshed', async () => {
    current = page('rev-1', [story('entry:13', 'Old coverage')])
    const {client} = mount()
    await screen.findByText('Old coverage')
    current = page('rev-2', [story('entry:14', 'New coverage')])
    await act(async () => {await client.refetchQueries({queryKey: ['front', 1, 'daily']})})
    await waitFor(() => expect(screen.getByText('New coverage available')).toBeTruthy())
    expect(screen.queryByText('New coverage')).toBeNull()
    expect(screen.getByText('Old coverage')).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', {name: 'Refresh'}).at(-1)!)
    await screen.findByText('New coverage')
    expect(screen.queryByText('Old coverage')).toBeNull()
  })

  it('uses the persisted page per account and period with its updated time', async () => {
    const cached = page('rev-cached', [story('entry:15', 'Cached story')])
    localStorage.setItem(frontCacheKey(1, 'daily'), JSON.stringify({page: cached, updated_at: '2026-11-12T08:00:00Z'}))
    vi.mocked(request).mockRejectedValue(new Error('Offline'))
    online.value = false
    mount()
    await screen.findByText('Cached story')
    expect(screen.getByText(/Updated/)).toBeTruthy()
    expect(screen.getByText(/Offline/)).toBeTruthy()
    expect(screen.queryByText('Lead story')).toBeNull()
  })

  it('does not show another account’s cached page', async () => {
    localStorage.setItem(frontCacheKey(1, 'daily'), JSON.stringify({page: page('rev-cached', [story('entry:16', 'Private cached story')]), updated_at: '2026-11-12T08:00:00Z'}))
    current = page('rev-other', [])
    mount('daily', 2)
    await screen.findByText('No stories in this window')
    expect(screen.queryByText('Private cached story')).toBeNull()
  })
})
