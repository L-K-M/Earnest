// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {Sidebar} from './Sidebar'
import {feedSwatch} from './EntryMedia'
import type {Subscription} from './reader'
import type {Route} from './routes'

const route: Route = {name: 'front', period: 'daily'}

function mount(subscriptions: Subscription[] = []) {
  return render(<Sidebar route={route} subscriptions={subscriptions} folders={[]} labels={[]} savedSearches={[]} askEnabled={false} sync={{text: 'Synced', tone: 'quiet'}} onPalette={vi.fn()} onHelp={vi.fn()} onSignOut={vi.fn()} prefetch={vi.fn()} />)
}

afterEach(cleanup)

it('hashes feed titles to the shared swatch index', () => {
  expect(feedSwatch('The Morning Ledger')).toBe(1)
  expect(feedSwatch('Field Notes')).toBe(6)
  expect(feedSwatch('The Quiet Engineer')).toBe(3)
  expect(feedSwatch('Kitchen Science')).toBe(2)
  expect(feedSwatch('Night Sky Weekly')).toBe(5)
  expect(feedSwatch('Ars Botanica')).toBe(2)
  expect(feedSwatch('')).toBe(0)
  expect(feedSwatch('Café 😀')).toBe(6)
  expect(feedSwatch('https://example.com/feed.xml')).toBe(0)
})

it('tones the reading destinations and leaves settings neutral', () => {
  mount()
  expect(screen.getByRole('link', {name: 'Front page'}).getAttribute('data-tone')).toBe('orange')
  expect(screen.getByRole('link', {name: 'All articles'}).getAttribute('data-tone')).toBe('blue')
  expect(screen.getByRole('link', {name: 'Settings'}).hasAttribute('data-tone')).toBe(false)
})

it('shows a feed\'s stored icon and falls back to its letter mark', () => {
  const health = {consecutive_failures: 0, firehose: false, http_status: 200, posts_per_day: 1} as Subscription['health']
  const feeds = [
    {id: 1, title: '404 Media', url: 'https://example.com/a', icon_url: '/v1/subscriptions/1/icon?v=abc', health, deleted: false, folder_id: 0},
    {id: 2, title: 'Plain Feed', url: 'https://example.com/b', health, deleted: false, folder_id: 0},
  ] as Subscription[]
  mount(feeds)
  const withIcon = screen.getByRole('link', {name: '404 Media'})
  expect(withIcon.querySelector('img.feed-icon')?.getAttribute('src')).toBe('/v1/subscriptions/1/icon?v=abc')
  expect(withIcon.querySelector('.feed-mark')).toBeNull()
  const plain = screen.getByRole('link', {name: 'Plain Feed'})
  expect(plain.querySelector('img')).toBeNull()
  expect(plain.querySelector('.feed-mark')?.textContent).toBe('P')
})

const folders = [
  {id: 1, name: 'Parent', parent_id: 0, rev: 1, deleted: false},
  {id: 2, name: 'Child', parent_id: 1, rev: 2, deleted: false},
  {id: 3, name: 'Destination', parent_id: 0, rev: 3, deleted: false},
]
const feeds = [{id: 10, title: 'Feed', folder_id: 2, url: 'https://example.com', deleted: false}] as Subscription[]
function mountTree(onMove = vi.fn().mockResolvedValue(undefined)) {
  localStorage.clear()
  render(<Sidebar route={route} subscriptions={feeds} folders={folders} labels={[]} savedSearches={[]} askEnabled={false} sync={{text: 'Synced', tone: 'quiet'}} onPalette={vi.fn()} onHelp={vi.fn()} onSignOut={vi.fn()} prefetch={vi.fn()} onMove={onMove} />)
  return onMove
}
function startDrag(name: string) {
  const dataTransfer = {setData: vi.fn(), effectAllowed: '', dropEffect: ''}
  fireEvent.dragStart(screen.getByRole('link', {name}), {dataTransfer})
  return dataTransfer
}
function dropTarget(name: string) {
  return screen.getByRole('link', {name}).closest('.nav-folder-row')!
}
it('renders nested folders and hides their descendants when collapsed', () => {
  mountTree()
  const parent = screen.getByRole('link', {name:'Parent'}).closest('li')!
  expect(within(parent).getByRole('link', {name:'Child'})).toBeTruthy()
  expect(within(parent).getByRole('link', {name:'Feed'})).toBeTruthy()
  fireEvent.click(screen.getByRole('button', {name:'Collapse Parent'}))
  expect(screen.queryByRole('link', {name:'Child'})).toBeNull()
  expect(screen.queryByRole('link', {name:'Feed'})).toBeNull()
})
it('moves a feed onto a collapsed folder and expands the destination after saving', async () => {
  const onMove = mountTree()
  fireEvent.click(screen.getByRole('button', {name:'Collapse Destination'}))
  const dataTransfer = startDrag('Feed')
  const target = dropTarget('Destination')
  fireEvent.dragOver(target, {dataTransfer})
  expect(target.getAttribute('data-drop-target')).toBe('true')
  expect(dataTransfer.dropEffect).toBe('move')
  fireEvent.drop(target, {dataTransfer})
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Moved.'))
  expect(onMove).toHaveBeenCalledExactlyOnceWith({kind:'feed', id:10}, 3)
  expect(screen.getByRole('button', {name:'Collapse Destination'})).toBeTruthy()
})
it('moves a folder with its subtree and allows returning it to the root', async () => {
  const onMove = mountTree()
  let dataTransfer = startDrag('Child')
  fireEvent.drop(dropTarget('Destination'), {dataTransfer})
  await waitFor(() => expect(onMove).toHaveBeenCalledWith({kind:'folder', id:2}, 3))
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Moved.'))
  dataTransfer = startDrag('Child')
  fireEvent.drop(screen.getByText('Drop here to move to the top level'), {dataTransfer})
  await waitFor(() => expect(onMove).toHaveBeenCalledWith({kind:'folder', id:2}, 0))
})
it('rejects cycles, unchanged feeds, external drags and cancelled drags', () => {
  const onMove = mountTree()
  fireEvent.drop(dropTarget('Destination'), {dataTransfer:{getData: () => 'external'}})
  let dataTransfer = startDrag('Parent')
  fireEvent.dragOver(dropTarget('Child'), {dataTransfer})
  expect(dataTransfer.dropEffect).toBe('none')
  fireEvent.drop(dropTarget('Child'), {dataTransfer})
  dataTransfer = startDrag('Feed')
  fireEvent.drop(dropTarget('Child'), {dataTransfer})
  dataTransfer = startDrag('Feed')
  fireEvent.dragEnd(screen.getByRole('link', {name:'Feed'}))
  fireEvent.drop(dropTarget('Destination'), {dataTransfer})
  expect(onMove).not.toHaveBeenCalled()
})
it('reports a failed move and keeps the original tree usable', async () => {
  const onMove = mountTree(vi.fn().mockRejectedValue(new Error('Offline')))
  const dataTransfer = startDrag('Feed')
  fireEvent.drop(dropTarget('Destination'), {dataTransfer})
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Could not move: Offline'))
  expect(screen.getByRole('link', {name:'Feed'}).getAttribute('draggable')).toBe('true')
  expect(onMove).toHaveBeenCalledTimes(1)
})

it('shows empty folders and prevents another move until the current save finishes', async () => {
  let finish!: () => void
  const onMove = mountTree(vi.fn(() => new Promise<void>(resolve => { finish = resolve })))
  const dataTransfer = startDrag('Feed')
  fireEvent.drop(dropTarget('Destination'), {dataTransfer})
  expect(screen.getByRole('link', {name:'Destination'}).getAttribute('draggable')).toBe('false')
  startDrag('Child')
  fireEvent.drop(dropTarget('Destination'), {dataTransfer})
  expect(onMove).toHaveBeenCalledTimes(1)
  finish()
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Moved.'))
})
