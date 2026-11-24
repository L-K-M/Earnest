// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, render, screen, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {Reader, type KeyHint} from './ArticleReader'
import {defaultSettings} from './Settings'
import {request} from './request'
import type {Entry} from './reader'

vi.mock('./request', () => ({request: vi.fn().mockResolvedValue({url: 'https://content.test/content/1'})}))
vi.mock('./offline/Offline', () => ({useOnline: () => true, OfflineArticle: () => null}))
vi.mock('./pipeline/Pipeline', () => ({Summary: () => null}))
vi.mock('./search/Search', () => ({MoreLikeThis: () => null}))
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('opens the article title in a new tab without giving the publisher an opener', () => {
  vi.stubGlobal('matchMedia', () => ({matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn()}))
  const entry = {id: 1, feed_id: 2, title: 'An article', url: 'https://publisher.test/story', state: {shelf: {value: 'none'}, kept: {value: false}}} as Entry
  render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}>
    <Reader entry={entry} settings={defaultSettings} actions={{state: e => e.state, act: vi.fn()}} onClose={vi.fn()} onSettings={vi.fn()} renderBody={() => null} />
  </QueryClientProvider>)
  const heading = screen.getByRole('heading', {name: 'An article'})
  const link = within(heading).getByRole('link', {name: 'An article'})
  expect(link.getAttribute('href')).toBe(entry.url)
  expect(link.getAttribute('target')).toBe('_blank')
  expect(link.getAttribute('rel')).toBe('noopener noreferrer')
})

it('names Shift+U beside m on Mark unread and shows the shortcut hints it is given', () => {
  vi.stubGlobal('matchMedia', () => ({matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn()}))
  const entry = {id: 1, feed_id: 2, title: 'An article', url: 'https://publisher.test/story', state: {shelf: {value: 'none'}, kept: {value: false}}} as Entry
  const reader = (hints?: KeyHint[]) => <QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}>
    <Reader entry={entry} settings={defaultSettings} actions={{state: e => e.state, act: vi.fn()}} onClose={vi.fn()} onSettings={vi.fn()} renderBody={() => null} hints={hints} />
  </QueryClientProvider>
  const {container, rerender} = render(reader([{keys: ['j', 'k'], label: 'Next or previous'}, {keys: ['?'], label: 'All shortcuts'}]))
  const toolbar = screen.getByRole('group', {name: 'Article actions'})
  const unread = within(toolbar).getByRole('button', {name: 'Mark unread'})
  expect(unread.getAttribute('aria-keyshortcuts')).toBe('m Shift+U')
  expect(unread.getAttribute('title')).toBe('Mark unread (m or Shift+U)')
  // Sit between the article's actions and the links out, hidden from
  // assistive technology, which hears each button's own keys instead.
  const strip = container.querySelector('.tool-hints')!
  expect(strip.getAttribute('aria-hidden')).toBe('true')
  expect(unread.compareDocumentPosition(strip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(strip.compareDocumentPosition(within(toolbar).getByRole('link', {name: 'Open original'})) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect([...strip.querySelectorAll('.tool-hint')].map(hint => [[...hint.querySelectorAll('kbd')].map(key => key.textContent), hint.textContent])).toEqual([[['j', 'k'], 'jkNext or previous'], [['?'], '?All shortcuts']])
  rerender(reader())
  expect(container.querySelector('.tool-hints')!.childElementCount).toBe(0)
})

it('offers a configured integration send in the article toolbar, but not an unconfigured one', async () => {
  vi.stubGlobal('matchMedia', () => ({matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn()}))
  const original = vi.mocked(request).getMockImplementation()
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return {
      karakeep: {url: 'https://karakeep.test', default_list_id: '', default_label: '', token_set: true},
      hister: {url: '', default_list_id: '', default_label: '', token_set: false},
    }
    return {url: 'https://content.test/content/1'}
  })
  try {
    const entry = {id: 1, feed_id: 2, title: 'An article', url: 'https://publisher.test/story', state: {shelf: {value: 'none'}, kept: {value: false}}} as Entry
    render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}>
      <Reader entry={entry} settings={defaultSettings} actions={{state: e => e.state, act: vi.fn()}} onClose={vi.fn()} onSettings={vi.fn()} renderBody={() => null} />
    </QueryClientProvider>)
    expect(await screen.findByRole('button', {name: 'Send to Karakeep'})).toBeTruthy()
    expect(screen.queryByRole('button', {name: 'Send to Hister'})).toBeNull()
  } finally {
    if (original) vi.mocked(request).mockImplementation(original)
  }
})
