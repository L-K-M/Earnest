import {request} from './request'

export type UnreadCounts = {byFeed: Record<string, number>; later: number; kept: number}

type CountResponse = {by_feed: Record<string, number>; later: number; kept: number; total?: number}

// The server applies the same visibility rules as an all/unread_only entry
// page and returns one aggregate per subscription, so a large backlog costs a
// single scan instead of a page per 200 entries.
export async function readUnreadCounts(signal: AbortSignal): Promise<UnreadCounts> {
  const result = await request<CountResponse>('/v1/entries:counts', {signal})
  return {byFeed: result.by_feed ?? {}, later: result.later ?? 0, kept: result.kept ?? 0}
}
