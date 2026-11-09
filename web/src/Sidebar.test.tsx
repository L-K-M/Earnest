// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, render, screen} from '@testing-library/react'
import {Sidebar, feedSwatch} from './Sidebar'
import type {Route} from './routes'

const route: Route = {name: 'river', view: {kind: 'today'}}

function mount() {
  return render(<Sidebar route={route} subscriptions={[]} folders={[]} labels={[]} savedSearches={[]} askEnabled={false} sync={{text: 'Synced', tone: 'quiet'}} onPalette={vi.fn()} onHelp={vi.fn()} onSignOut={vi.fn()} prefetch={vi.fn()} />)
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
