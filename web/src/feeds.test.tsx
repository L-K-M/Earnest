// @vitest-environment jsdom
import {beforeEach, describe, expect, it, vi, type Mock} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request} from './request'
import {Feeds} from './FeedSettings'
import {
  defaultReadingSettings, explainHealth, groupByFolder, healthPending, isFirehose,
  normalizeFeedURLInput, parseCredentialHeaders, settingsBody, summarizeImport,
  type Folder, type OPMLResult, type Subscription,
} from './feeds'

vi.mock('./request', () => ({request: vi.fn()}))
const mocked = request as unknown as Mock<(path: string, options?: RequestInit) => Promise<unknown>>

type Routes = Record<string, unknown | ((options?: RequestInit) => unknown)>
function api(routes: Routes) {
  mocked.mockImplementation((path: string, options?: RequestInit) => {
    for (const [prefix, value] of Object.entries(routes)) {
      if (path.startsWith(prefix)) {
        return Promise.resolve(typeof value === 'function' ? value(options) : value)
      }
    }
    return Promise.reject(new Error(`unmocked request: ${path}`))
  })
}
const calls = (prefix: string, method?: string) => mocked.mock.calls.filter(([path, options]) => path.startsWith(prefix) && (!method || options?.method === method))
const bodyOf = (call: [string, RequestInit?]) => JSON.parse(String(call[1]?.body)) as Record<string, unknown>

const baseHealth = {next_fetch_at: '2026-11-02T01:00:00Z', consecutive_failures: 0, http_status: 200, posts_per_day: 2, firehose: false, last_attempt_at: '2026-11-02T00:00:00Z', last_success_at: '2026-11-02T00:00:00Z'}
function sub(overrides: Partial<Subscription> = {}): Subscription {
  return {id: 1, feed_id: 11, title: 'Feed One', url: 'https://a.example/feed', site_url: 'https://a.example/', folder_id: 0, cadence: 'today', extract: '', rev: 1, deleted: false, private: false, initial_through_seq: 0, initial_fetch_pending: false, health: {...baseHealth}, ...overrides}
}
function folder(overrides: Partial<Folder> = {}): Folder {
  return {id: 3, name: 'Tech', rev: 1, deleted: false, ...overrides}
}

beforeEach(() => {
  cleanup()
  mocked.mockReset()
})

describe('feed rules', () => {
  it('normalizes feed URLs like the service', () => {
    expect(normalizeFeedURLInput('  HTTPS://EXAMPLE.com  ')).toBe('https://example.com/')
    expect(normalizeFeedURLInput('https://example.com/feed#frag')).toBe('https://example.com/feed')
    expect(normalizeFeedURLInput('')).toBeNull()
    expect(normalizeFeedURLInput('not a url')).toBeNull()
    expect(normalizeFeedURLInput('ftp://example.com/feed')).toBeNull()
    expect(normalizeFeedURLInput('https://user:pw@example.com/feed')).toBeNull()
    expect(normalizeFeedURLInput(`https://example.com/${'x'.repeat(9000)}`)).toBeNull()
  })

  it('parses credential headers strictly', () => {
    expect(parseCredentialHeaders('Authorization: Bearer t\n\nX-Thing: 1')).toEqual({Authorization: 'Bearer t', 'X-Thing': '1'})
    expect(parseCredentialHeaders('')).toEqual({})
    expect(parseCredentialHeaders('no colon here')).toBeNull()
    expect(parseCredentialHeaders('Empty:')).toBeNull()
  })

  it('explains failures per class and stays quiet when healthy', () => {
    expect(explainHealth({...baseHealth, consecutive_failures: 0})).toBeNull()
    expect(explainHealth({...baseHealth, consecutive_failures: 2, error_class: 'gone'})).toContain('gone for good')
    expect(explainHealth({...baseHealth, consecutive_failures: 1, error_class: 'rate_limit'})).toContain('limiting')
    expect(explainHealth({...baseHealth, consecutive_failures: 1, error_class: 'denied_address'})).toContain('refused')
    expect(explainHealth({...baseHealth, consecutive_failures: 1, error_class: 'mystery'})).toBe('Fetch failed (mystery).')
    expect(explainHealth({...baseHealth, consecutive_failures: 1})).toBe('Fetch failed.')
  })

  it('summarizes an OPML import honestly', () => {
    const results: OPMLResult[] = [
      {path: [0], deferred: true, subscription: sub()},
      {path: [1], deferred: false, folder: folder()},
      {path: [2], deferred: false, error: 'invalid feed URL'},
    ]
    expect(summarizeImport(results)).toEqual({feeds: 1, folders: 1, deferred: 1, failed: ['invalid feed URL']})
  })

  it('detects firehoses and pending health', () => {
    expect(isFirehose(sub({health: {...baseHealth, firehose: true}}))).toBe(true)
    expect(isFirehose(sub({health: {...baseHealth, posts_per_day: 25}}))).toBe(true)
    expect(isFirehose(sub())).toBe(false)
    expect(healthPending(sub({health: {...baseHealth, last_attempt_at: undefined}}))).toBe(true)
    expect(healthPending(sub())).toBe(false)
  })

  it('groups subscriptions by folder with unfiled last', () => {
    const groups = groupByFolder([sub({folder_id: 3}), sub({id: 2}), sub({id: 4, deleted: true})], [folder()])
    expect(groups.map(group => [group.folder?.name ?? 'none', group.subscriptions.map(s => s.id)])).toEqual([['Tech', [1]], ['none', [2]]])
  })

  it('builds the documented settings body', () => {
    const settings = defaultReadingSettings()
    const body = settingsBody(settings, 'op-1')
    expect(body).toEqual({op_id: 'op-1', reading_mode: 'river', today_decay_days: 2, single_key_shortcuts: true, theme: 'system', measure: 66, font: 'serif', feed_overrides: {}})
  })
})

describe('Feeds', () => {
  it('lists subscriptions and explains a failing feed in plain words', async () => {
    api({
      '/v1/subscriptions': [sub(), sub({id: 2, title: 'Broken Feed', health: {...baseHealth, consecutive_failures: 3, error_class: 'gone'}})],
      '/v1/folders': [],
      '/v1/mutes': [],
    })
    render(<Feeds onChanged={() => {}} />)
    await screen.findByText('Broken Feed')
    expect(screen.getByText(/reports itself gone/).textContent).toContain('3 times')
    expect(screen.getByText('Feed One')).toBeTruthy()
  })

  it('rejects an invalid feed URL without calling the API', async () => {
    api({'/v1/subscriptions': [], '/v1/folders': [], '/v1/mutes': []})
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Feed or site URL'), {target: {value: 'ftp://example.com/feed'}})
    fireEvent.click(screen.getByRole('button', {name: 'Subscribe'}))
    await screen.findByText(/does not look like a feed address/)
    expect(mocked.mock.calls.filter(([, options]) => options?.method && options.method !== 'GET')).toHaveLength(0)
  })

  it('patches cadence with an op_id', async () => {
    api({'/v1/subscriptions': [sub()], '/v1/folders': [], '/v1/mutes': []})
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Cadence'), {target: {value: 'week'}})
    await waitFor(() => expect(calls('/v1/subscriptions/1', 'PATCH')).toHaveLength(1))
    expect(bodyOf(calls('/v1/subscriptions/1', 'PATCH')[0])).toMatchObject({cadence: 'week'})
    expect(bodyOf(calls('/v1/subscriptions/1', 'PATCH')[0]).op_id).toBeTruthy()
  })

  it('creates a folder then assigns it', async () => {
    api({'/v1/subscriptions': [sub()], '/v1/folders': (options?: RequestInit) => options?.method === 'POST' ? folder() : [folder()], '/v1/mutes': []})
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('New folder'), {target: {value: 'Tech'}})
    fireEvent.click(screen.getByRole('button', {name: 'Add folder'}))
    await waitFor(() => expect(calls('/v1/folders', 'POST')).toHaveLength(1))
    expect(bodyOf(calls('/v1/folders', 'POST')[0])).toMatchObject({name: 'Tech'})
  })

  it('subscribes with credentials and plain errors surface', async () => {
    api({
      '/v1/subscriptions': (options?: RequestInit) => {
        if (options?.method === 'POST') return Promise.reject(new Error('probe failed (dns): unavailable'))
        return []
      },
      '/v1/folders': [],
      '/v1/mutes': [],
    })
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Feed or site URL'), {target: {value: 'https://feeds.example/rss'}})
    fireEvent.click(screen.getByLabelText('This feed needs credentials'))
    fireEvent.change(screen.getByLabelText('Username'), {target: {value: 'u'}})
    fireEvent.change(screen.getByLabelText('Password'), {target: {value: 'p'}})
    fireEvent.click(screen.getByRole('button', {name: 'Subscribe'}))
    await screen.findByText(/probe failed \(dns\)/)
    const post = calls('/v1/subscriptions', 'POST')[0]
    expect(bodyOf(post)).toMatchObject({url: 'https://feeds.example/rss', credentials: {username: 'u', password: 'p'}})
  })

  it('adds and removes mutes', async () => {
    api({'/v1/subscriptions': [], '/v1/folders': [], '/v1/mutes': (options?: RequestInit) => options?.method === 'POST' ? {id: 9, kind: 'keyword', value: 'crypto', rev: 1, deleted: false} : []})
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Value'), {target: {value: ' crypto '}})
    fireEvent.click(screen.getByRole('button', {name: 'Add mute'}))
    await waitFor(() => expect(calls('/v1/mutes', 'POST')).toHaveLength(1))
    expect(bodyOf(calls('/v1/mutes', 'POST')[0])).toMatchObject({kind: 'keyword', value: 'crypto'})
  })

  it('unsubscribes only after confirmation', async () => {
    api({'/v1/subscriptions': [sub()], '/v1/folders': [], '/v1/mutes': []})
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    render(<Feeds onChanged={() => {}} />)
    fireEvent.click(await screen.findByRole('button', {name: 'Unsubscribe'}))
    expect(calls('/v1/subscriptions/1', 'DELETE')).toHaveLength(0)
    confirm.mockReturnValue(true)
    fireEvent.click(screen.getByRole('button', {name: 'Unsubscribe'}))
    await waitFor(() => expect(calls('/v1/subscriptions/1', 'DELETE')).toHaveLength(1))
    expect(calls('/v1/subscriptions/1', 'DELETE')[0][0]).toContain('op_id=')
    confirm.mockRestore()
  })

  it('imports OPML and reports the outcome', async () => {
    api({
      '/v1/subscriptions': [],
      '/v1/folders': [],
      '/v1/mutes': [],
      '/v1/opml:import': [{path: [0], deferred: true, subscription: sub()}, {path: [1], deferred: false, error: 'invalid feed URL'}],
    })
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Import a subscription file'), {target: {files: [new window.File(['<opml/>'], 'feeds.opml')]}})
    fireEvent.click(screen.getByRole('button', {name: 'Import'}))
    await screen.findByText(/1 feed subscribed/)
    await screen.findByText(/1 could not be imported/)
    const post = calls('/v1/opml:import', 'POST')[0]
    expect(post[0]).toBe('/v1/opml:import')
    expect(new Headers(post[1]?.headers).get('Content-Type')).toBe('text/x-opml')
  })
})
