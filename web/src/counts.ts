import {request} from './request'
import type {Entry} from './reader'
export type UnreadCounts = {byFeed: Record<string, number>; later: number; kept: number}
export function countUnread(entries: Entry[], counts: UnreadCounts = {byFeed:{},later:0,kept:0}): UnreadCounts {
  for (const entry of entries) {
    if (entry.deleted || entry.state.seen.value !== 'unseen' || entry.state.shelf.value === 'done') continue
    counts.byFeed[String(entry.subscription_id)] = (counts.byFeed[String(entry.subscription_id)] ?? 0) + 1
    if (entry.state.shelf.value === 'later') counts.later++
    if (entry.state.kept.value === true) counts.kept++
  }
  return counts
}
export async function readUnreadCounts(signal: AbortSignal): Promise<UnreadCounts> {
  const latest = await request<{entries: Entry[]}>('/v1/entries?view=all&direction=backward&limit=1', {signal})
  const counts: UnreadCounts = {byFeed:{},later:0,kept:0}
  if (!latest.entries.length) return counts
  // Fix the upper boundary so a busy feed cannot keep this count running.
  const query = new URLSearchParams({view:'all',unread_only:'true',limit:'200',direction:'forward',before_seq:String(BigInt(latest.entries[0].seq)+1n)})
  let cursor=''
  for (;;) {
    if (cursor) query.set('cursor',cursor)
    const page=await request<{entries:Entry[];next_cursor?:string}>(`/v1/entries?${query}`,{signal})
    countUnread(page.entries,counts)
    if (!page.next_cursor) return counts
    if (page.next_cursor===cursor) throw new Error('The unread count could not advance its page cursor.')
    cursor=page.next_cursor
  }
}
