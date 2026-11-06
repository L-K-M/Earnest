import {request} from './request'
import type {components} from './api.generated'

export type UnreadCounts = {byFeed: Record<string, number>; later: number; kept: number}

// The server aggregates the tallies in one grouped query. The previous
// implementation paged every unread entry through /v1/entries, which cost one
// round trip per 200 unread articles on every refresh.
export async function readUnreadCounts(signal: AbortSignal): Promise<UnreadCounts> {
  const totals = await request<components['schemas']['UnreadCounts']>('/v1/unread-counts', {signal})
  const byFeed: Record<string, number> = {}
  for (const feed of totals.feeds ?? []) {
    if (feed.unread > 0) byFeed[String(feed.subscription_id)] = feed.unread
  }
  return {byFeed, later: totals.later, kept: totals.kept}
}
