import {request} from './request'

export type UnreadCounts = {bySubscription: Record<string, number>; later: number; kept: number}

type CountResponse = {by_subscription: Record<string, number>; later: number; kept: number; total?: number}

// The server applies the same visibility rules as an all/unread_only entry
// page and returns one aggregate per subscription, so a large backlog costs a
// single scan instead of a page per 200 entries.
export async function readUnreadCounts(signal: AbortSignal): Promise<UnreadCounts> {
  const result = await request<CountResponse>('/v1/entries:counts', {signal})
  return {bySubscription: result.by_subscription ?? {}, later: result.later ?? 0, kept: result.kept ?? 0}
}
