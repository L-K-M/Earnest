// @vitest-environment jsdom
import {beforeEach, describe, expect, it, vi, type Mock} from 'vitest'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request} from './request'
import {Feeds} from './FeedSettings'
import {
  defaultReadingSettings, explainHealth, groupByFolder, healthPending, isFirehose,
  normalizeFeedURLInput, parseCredentialHeaders, settingsBody, summarizeImport, summarizeKeptImport,
  type Folder, type KeptImportReport, type OPMLResult, type Subscription,
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
  sessionStorage.clear()
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

  it('summarizes a kept import by status', () => {
    const report: KeptImportReport = {
      results: [
        {index: 0, status: 'created', title: 'New', labels: ['Tech']},
        {index: 1, status: 'matched', title: 'Found'},
        {index: 2, status: 'unchanged', title: 'Kept'},
        {index: 3, status: 'unkept', title: 'Removed'},
        {index: 4, status: 'gone', title: 'Expired'},
        {index: 5, status: 'not_subscribed', feed_url: 'https://b.example/rss', title: 'Six'},
        {index: 6, status: 'not_subscribed', feed_url: 'https://a.example/feed', title: 'Seven'},
        {index: 7, status: 'not_subscribed', feed_url: 'https://b.example/rss', title: 'Eight'},
        {index: 8, status: 'not_subscribed', title: 'Nine'},
        {index: 9, status: 'failed', title: '', error: ''},
        {index: 10, status: 'failed', title: 'Broken', error: 'no link'},
      ],
      subscribed: [sub(), sub(), sub()],
    }
    expect(summarizeKeptImport(report)).toEqual({
      added: 1,
      existing: 2,
      unkept: 1,
      gone: 1,
      skipped: [
        {feedURL: 'https://b.example/rss', count: 2},
        {feedURL: '', count: 1},
        {feedURL: 'https://a.example/feed', count: 1},
      ],
      failed: ['Article 10: could not be imported', 'Broken: no link'],
      subscribed: 3,
      labelled: 1,
    })
  })

  it('treats null lists as empty in a kept import', () => {
    expect(summarizeKeptImport({results: null, subscribed: null})).toEqual({
      added: 0, existing: 0, unkept: 0, gone: 0, skipped: [], failed: [], subscribed: 0, labelled: 0,
    })
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

  it('opens the edit details of a failing feed by default', async () => {
    api({
      '/v1/subscriptions': [sub(), sub({id: 2, title: 'Broken Feed', health: {...baseHealth, consecutive_failures: 2, error_class: 'gone'}})],
      '/v1/folders': [],
      '/v1/mutes': [],
    })
    render(<Feeds onChanged={() => {}} />)
    const summaries = await screen.findAllByText('Edit')
    expect(summaries[0].closest('details')?.hasAttribute('open')).toBe(false)
    expect(summaries[1].closest('details')?.hasAttribute('open')).toBe(true)
  })

  it('validates the feed URL on blur without calling the API', async () => {
    api({'/v1/subscriptions': [], '/v1/folders': [], '/v1/mutes': []})
    render(<Feeds onChanged={() => {}} />)
    const input = await screen.findByLabelText('Feed or site URL')
    fireEvent.change(input, {target: {value: 'not a url'}})
    fireEvent.blur(input)
    expect(input.getAttribute('aria-invalid')).toBe('true')
    const error = await screen.findByText(/does not look like a feed address/)
    expect(error.id).toBe('subscribe-url-error')
    const describedby = input.getAttribute('aria-describedby') ?? ''
    expect(describedby).toContain('subscribe-url-hint')
    expect(describedby).toContain('subscribe-url-error')
    expect(mocked.mock.calls.filter(([, options]) => options?.method && options.method !== 'GET')).toHaveLength(0)
    fireEvent.change(input, {target: {value: 'https://example.com/feed'}})
    await waitFor(() => expect(input.getAttribute('aria-invalid')).toBeNull())
    expect(screen.queryByText(/does not look like a feed address/)).toBeNull()
  })

  it('restores the subscribe draft after remount and clears it after subscribing', async () => {
    api({
      '/v1/subscriptions': (options?: RequestInit) => options?.method === 'POST' ? sub({id: 2}) : [],
      '/v1/folders': [folder()],
      '/v1/mutes': [],
    })
    const first = render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await first.findByLabelText('Feed or site URL'), {target: {value: 'https://example.com/feed'}})
    fireEvent.change(first.getByLabelText('Title (optional)'), {target: {value: 'My feed'}})
    fireEvent.change(first.getByLabelText('Folder'), {target: {value: '3'}})
    fireEvent.click(first.getByLabelText(/This week/))
    fireEvent.change(first.getByLabelText('Full text'), {target: {value: 'always'}})
    expect(JSON.parse(sessionStorage.getItem('earnest.subscribe.draft') ?? '')).toEqual({url: 'https://example.com/feed', title: 'My feed', folderID: 3, cadence: 'week', extract: 'always'})
    first.unmount()
    const second = render(<Feeds onChanged={() => {}} />)
    expect((await second.findByLabelText('Feed or site URL') as HTMLInputElement).value).toBe('https://example.com/feed')
    expect((second.getByLabelText('Title (optional)') as HTMLInputElement).value).toBe('My feed')
    expect((second.getByLabelText('Folder') as HTMLSelectElement).value).toBe('3')
    expect((second.getByLabelText(/This week/) as HTMLInputElement).checked).toBe(true)
    expect((second.getByLabelText('Full text') as HTMLSelectElement).value).toBe('always')
    fireEvent.click(second.getByRole('button', {name: 'Subscribe'}))
    await second.findByText('Subscribed. The first fetch is on its way.')
    await waitFor(() => expect(sessionStorage.getItem('earnest.subscribe.draft')).toBeNull())
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

  it('imports starred articles and reports the outcome', async () => {
    api({
      '/v1/subscriptions': [],
      '/v1/folders': [],
      '/v1/mutes': [],
      '/v1/kept:import': {
        results: [
          {index: 0, status: 'created', title: 'New', labels: ['Tech']},
          {index: 1, status: 'matched', title: 'Found'},
          {index: 2, status: 'not_subscribed', feed_url: 'https://b.example/rss', title: 'Skipped'},
          {index: 3, status: 'failed', title: 'Broken', error: 'no link'},
        ],
        subscribed: [sub()],
      },
    })
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Import starred articles'), {target: {files: [new window.File(['{"items":[]}'], 'starred.json', {type: 'application/json'})]}})
    fireEvent.click(screen.getByRole('button', {name: 'Import starred'}))
    await screen.findByText(/2 articles in Kept: 1 added from the export, 1 already in Earnest/)
    await screen.findByText(/Subscribed to 1 feed/)
    await screen.findByText("Added the export's labels to 1 article.")
    await screen.findByText(/1 skipped because you don/)
    expect(screen.getByText('https://b.example/rss (1)')).toBeTruthy()
    await screen.findByText(/1 could not be imported: Broken: no link/)
    const post = calls('/v1/kept:import', 'POST')[0]
    expect(post[0]).toBe('/v1/kept:import?missing_feeds=skip')
    expect(new Headers(post[1]?.headers).get('Content-Type')).toBe('application/json')
    expect(post[1]?.body).toBe('{"items":[]}')
    fireEvent.click(screen.getByRole('button', {name: 'Subscribe to these feeds and import again'}))
    await waitFor(() => expect(calls('/v1/kept:import', 'POST')).toHaveLength(2))
    const again = calls('/v1/kept:import', 'POST')[1]
    expect(again[0]).toBe('/v1/kept:import?missing_feeds=subscribe')
    expect(again[1]?.body).toBe('{"items":[]}')
  })

  it('subscribes to missing feeds when the option is checked before importing', async () => {
    api({
      '/v1/subscriptions': [],
      '/v1/folders': [],
      '/v1/mutes': [],
      '/v1/kept:import': {results: [], subscribed: []},
    })
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Import starred articles'), {target: {files: [new window.File(['{"items":[]}'], 'starred.json', {type: 'application/json'})]}})
    fireEvent.click(screen.getByLabelText("Also subscribe to feeds you don't follow"))
    fireEvent.click(screen.getByRole('button', {name: 'Import starred'}))
    await waitFor(() => expect(calls('/v1/kept:import', 'POST')).toHaveLength(1))
    expect(calls('/v1/kept:import', 'POST')[0][0]).toBe('/v1/kept:import?missing_feeds=subscribe')
  })

  it('imports once per click and forgets the report when another file is chosen', async () => {
    let finish: (value: unknown) => void = () => {}
    api({
      '/v1/subscriptions': [],
      '/v1/folders': [],
      '/v1/mutes': [],
      '/v1/kept:import': () => new Promise(resolve => { finish = resolve }),
    })
    render(<Feeds onChanged={() => {}} />)
    const input = await screen.findByLabelText('Import starred articles')
    fireEvent.change(input, {target: {files: [new window.File(['{"items":[]}'], 'starred.json', {type: 'application/json'})]}})
    const button = screen.getByRole('button', {name: 'Import starred'})
    fireEvent.click(button)
    fireEvent.click(button)
    await waitFor(() => expect(calls('/v1/kept:import', 'POST')).toHaveLength(1))
    expect((button as HTMLButtonElement).disabled).toBe(true)
    expect((input as HTMLInputElement).disabled).toBe(true)
    await act(async () => { finish({results: [{index: 0, status: 'not_subscribed', feed_url: 'https://b.example/rss'}], subscribed: []}) })
    await screen.findByRole('button', {name: 'Subscribe to these feeds and import again'})
    expect(calls('/v1/kept:import', 'POST')).toHaveLength(1)
    fireEvent.change(input, {target: {files: [new window.File(['{"items":[{}]}'], 'other.json', {type: 'application/json'})]}})
    expect(screen.queryByRole('button', {name: 'Subscribe to these feeds and import again'})).toBeNull()
    expect(screen.queryByText(/skipped because you don/)).toBeNull()
  })

  it('replaces an earlier report when a retry fails', async () => {
    let attempt = 0
    api({
      '/v1/subscriptions': [],
      '/v1/folders': [],
      '/v1/mutes': [],
      '/v1/kept:import': () => {
        attempt++
        return attempt === 1 ? {results: [{index: 0, status: 'not_subscribed', feed_url: 'https://b.example/rss'}], subscribed: []} : Promise.reject(new Error('Server unavailable.'))
      },
    })
    render(<Feeds onChanged={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Import starred articles'), {target: {files: [new window.File(['{"items":[]}'], 'starred.json', {type: 'application/json'})]}})
    fireEvent.click(screen.getByRole('button', {name: 'Import starred'}))
    fireEvent.click(await screen.findByRole('button', {name: 'Subscribe to these feeds and import again'}))
    await screen.findByText('Server unavailable.')
    expect(screen.queryByText(/skipped because you don/)).toBeNull()
  })

  it('updates the title input when the feed title changes externally', async () => {
    const routes: Routes = {'/v1/subscriptions': [sub()], '/v1/folders': [], '/v1/mutes': []}
    api(routes)
    render(<Feeds onChanged={() => {}} />)
    expect((await screen.findByLabelText('Title') as HTMLInputElement).value).toBe('Feed One')
    routes['/v1/subscriptions'] = [sub({title: 'Renamed', rev: 2})]
    fireEvent.click(screen.getByRole('button', {name: 'Refresh now'}))
    await waitFor(() => expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Renamed'))
  })

  it('keeps an unsaved draft title when the feed title changes externally', async () => {
    const routes: Routes = {'/v1/subscriptions': [sub()], '/v1/folders': [], '/v1/mutes': []}
    api(routes)
    render(<Feeds onChanged={() => {}} />)
    const input = await screen.findByLabelText('Title') as HTMLInputElement
    input.focus()
    fireEvent.change(input, {target: {value: 'My draft'}})
    // Clicking a button moves focus in a real browser; the draft must survive.
    input.blur()
    routes['/v1/subscriptions'] = [sub({title: 'Renamed', rev: 2})]
    fireEvent.click(screen.getByRole('button', {name: 'Refresh now'}))
    await waitFor(() => expect((screen.getByRole('button', {name: 'Refresh now'}) as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('My draft')
  })

  it('revokes the OPML download URL only after the download starts', async () => {
    api({'/v1/subscriptions': [sub()], '/v1/folders': [], '/v1/mutes': [], '/v1/opml': '<opml/>'})
    render(<Feeds onChanged={() => {}} />)
    const button = await screen.findByRole('button', {name: 'Export subscriptions'})
    const createObjectURL = vi.fn(() => 'blob:opml')
    const revokeObjectURL = vi.fn()
    const originalCreate = URL.createObjectURL
    const originalRevoke = URL.revokeObjectURL
    URL.createObjectURL = createObjectURL
    URL.revokeObjectURL = revokeObjectURL
    vi.useFakeTimers()
    try {
      fireEvent.click(button)
      await act(async () => { await vi.advanceTimersByTimeAsync(0) })
      expect(createObjectURL).toHaveBeenCalled()
      expect(revokeObjectURL).not.toHaveBeenCalled()
      await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
      expect(revokeObjectURL).toHaveBeenCalledWith('blob:opml')
    } finally {
      vi.useRealTimers()
      URL.createObjectURL = originalCreate
      URL.revokeObjectURL = originalRevoke
    }
  })
})
