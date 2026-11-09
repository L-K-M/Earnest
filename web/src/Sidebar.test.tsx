// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, render, screen} from '@testing-library/react'
import {Sidebar} from './Sidebar'
import {feedSwatch} from './EntryMedia'
import type {Subscription} from './reader'
import type {Route} from './routes'

const route: Route = {name: 'river', view: {kind: 'today'}}

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
  expect(screen.getByRole('link', {name: 'Today'}).getAttribute('data-tone')).toBe('orange')
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
