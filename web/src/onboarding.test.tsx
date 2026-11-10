// @vitest-environment jsdom
import {beforeEach, describe, expect, it, vi, type Mock} from 'vitest'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request} from './request'
import {Onboarding} from './Onboarding'
import type {Subscription} from './feeds'

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
const calls = (prefix: string) => mocked.mock.calls.filter(([path]) => path.startsWith(prefix))
const bodyOf = (call: [string, RequestInit?]) => JSON.parse(String(call[1]?.body)) as Record<string, unknown>

const baseHealth = {next_fetch_at: '2026-11-02T01:00:00Z', consecutive_failures: 0, http_status: 200, posts_per_day: 2, firehose: false, last_attempt_at: '2026-11-02T00:00:00Z', last_success_at: '2026-11-02T00:00:00Z'}
function sub(overrides: Partial<Subscription> = {}): Subscription {
  return {id: 7, feed_id: 11, title: 'Busy Feed', url: 'https://a.example/feed', site_url: 'https://a.example/', folder_id: 0, cadence: 'today', extract: '', rev: 1, deleted: false, private: false, muted: false, initial_through_seq: 0, initial_fetch_pending: false, health: {...baseHealth}, ...overrides}
}

beforeEach(() => {
  cleanup()
  mocked.mockReset()
})

describe('Onboarding', () => {
  it('imports OPML, proposes the week for firehoses, then starts a river at now', async () => {
    const firehose = sub({health: {...baseHealth, firehose: true, posts_per_day: 40}})
    api({
      '/v1/opml:import': [{path: [0], deferred: true, subscription: firehose}],
      '/v1/subscriptions': (options?: RequestInit) => options?.method === 'PATCH' ? firehose : [firehose],
      '/v1/entries': {entries: [{seq: 42}]},
      '/v1/positions/': {},
      '/v1/settings': {},
    })
    const done = vi.fn()
    render(<Onboarding onComplete={done} />)

    fireEvent.change(screen.getByLabelText('OPML subscription file'), {target: {files: [new window.File(['<opml/>'], 'feeds.opml')]}})
    fireEvent.click(screen.getByRole('button', {name: 'Import and continue'}))
    await screen.findByText('Import complete')
    await screen.findByText(/more than 20 a day/)
    fireEvent.click(screen.getByRole('button', {name: 'Move to This week'}))

    await screen.findByText('How do you like to read?')
    const moved = calls('/v1/subscriptions/7').find(([, options]) => options?.method === 'PATCH')
    expect(moved && bodyOf(moved)).toMatchObject({cadence: 'week'})

    fireEvent.click(screen.getByRole('button', {name: 'Start reading'}))
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1))

    const today = calls('/v1/positions/today')
    const edition = calls('/v1/positions/edition')
    expect(today).toHaveLength(1)
    expect(edition).toHaveLength(1)
    expect(bodyOf(today[0])).toMatchObject({seq: 42})
    expect(bodyOf(edition[0])).toMatchObject({seq: 42})

    const settings = calls('/v1/settings')
    expect(settings).toHaveLength(1)
    expect(settings[0][1]?.method).toBe('PUT')
    expect(bodyOf(settings[0])).toEqual({reading_mode: 'river', today_decay_days: 2, single_key_shortcuts: true, theme: 'system', measure: 66, font: 'serif', feed_overrides: {}, op_id: expect.any(String)})
  })

  it('skips import and saves unread-counts mode without touching positions', async () => {
    api({'/v1/settings': {}})
    const done = vi.fn()
    render(<Onboarding onComplete={done} />)

    fireEvent.click(screen.getByRole('button', {name: /Skip/}))
    await screen.findByText('How do you like to read?')
    fireEvent.click(screen.getByLabelText(/Unread counts/))
    fireEvent.click(screen.getByRole('button', {name: 'Start reading'}))
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1))

    expect(calls('/v1/positions')).toHaveLength(0)
    expect(calls('/v1/entries')).toHaveLength(0)
    expect(bodyOf(calls('/v1/settings')[0])).toMatchObject({reading_mode: 'unread_counts', today_decay_days: 2})
  })
})

describe('decay input and volume polling', () => {
  it('keeps today_decay_days numeric when the decay input is cleared', async () => {
    api({'/v1/entries': {entries: []}, '/v1/settings': {}})
    const done = vi.fn()
    render(<Onboarding onComplete={done} />)
    fireEvent.click(screen.getByRole('button', {name: /Skip/}))
    await screen.findByText('How do you like to read?')
    const input = screen.getByLabelText(/Today items stay for this many days/) as HTMLInputElement
    fireEvent.change(input, {target: {value: ''}})
    // Clearing the field falls back to the default instead of NaN.
    expect(input.value).toBe('2')
    fireEvent.change(input, {target: {value: '5'}})
    fireEvent.click(screen.getByRole('button', {name: 'Start reading'}))
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1))
    const settings = calls('/v1/settings')
    expect(settings).toHaveLength(1)
    expect(bodyOf(settings[0]).today_decay_days).toBe(5)
  })

  it('stops measuring feed volume after unmount', async () => {
    vi.useFakeTimers()
    // jsdom's FileReader completes on a real task; resolve it synchronously so
    // the import flow advances under fake timers.
    class InstantFileReader {
      result = '<opml/>'
      onload: (() => void) | null = null
      onerror: ((e: unknown) => void) | null = null
      readAsText() { this.onload?.() }
    }
    vi.stubGlobal('FileReader', InstantFileReader)
    try {
      const pending = sub({health: {...baseHealth, last_attempt_at: ''}})
      api({
        '/v1/opml:import': [{path: [0], deferred: true, subscription: pending}],
        '/v1/subscriptions': [pending],
      })
      const done = vi.fn()
      const view = render(<Onboarding onComplete={done} />)
      fireEvent.change(screen.getByLabelText('OPML subscription file'), {target: {files: [new window.File(['<opml/>'], 'feeds.opml')]}})
      fireEvent.click(screen.getByRole('button', {name: 'Import and continue'}))
      await act(async () => {})
      expect(screen.getByText('Import complete')).toBeTruthy()
      const before = calls('/v1/subscriptions').length
      view.unmount()
      await vi.advanceTimersByTimeAsync(60000)
      const after = calls('/v1/subscriptions').length
      // A cancelled poll stops after at most one more in-flight request; an
      // uncancelled one would run the full HEALTH_POLLS (8) iterations.
      expect(after).toBeLessThan(8)
      expect(after).toBeLessThanOrEqual(before + 1)
    } finally {
      vi.useRealTimers()
      vi.unstubAllGlobals()
    }
  })
})
