import {beforeEach, expect, it, vi, type Mock} from 'vitest'
import {readUnreadCounts} from './counts'
import {request} from './request'

vi.mock('./request', () => ({request: vi.fn()}))

beforeEach(() => {
  vi.mocked(request).mockReset()
})

it('maps the server tallies to per-feed counts, skipping idle feeds', async () => {
  vi.mocked(request).mockResolvedValue({feeds: [
    {subscription_id: 1, unread: 3, later: 1, kept: 2},
    {subscription_id: 2, unread: 0, later: 0, kept: 0},
    {subscription_id: 7, unread: 4, later: 0, kept: 0},
  ], later: 1, kept: 2})
  expect(await readUnreadCounts(new AbortController().signal)).toEqual({
    byFeed: {'1': 3, '7': 4}, later: 1, kept: 2,
  })
  expect((request as Mock).mock.calls[0][0]).toBe('/v1/unread-counts')
})

it('returns zero totals when the account is empty', async () => {
  vi.mocked(request).mockResolvedValue({feeds: [], later: 0, kept: 0})
  expect(await readUnreadCounts(new AbortController().signal)).toEqual({byFeed: {}, later: 0, kept: 0})
})
