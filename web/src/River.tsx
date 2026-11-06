import {useEffect, useRef, useState} from 'react'
import {useInfiniteQuery, useQuery} from '@tanstack/react-query'
import {useVirtualizer} from '@tanstack/react-virtual'
import {emptyCopy} from './copy'
import {entryQuery, orderEntries, shortcutsAllowed, viewKey, type Entry, type ReaderActions, type Subscription, type View} from './reader'
import {request} from './request'
import {TodayAgents, SessionChanges} from './agents/Surfaces'
import {Edition} from './pipeline/Pipeline'
import {usePipeline} from './search/pipeline'

type Page = {entries: Entry[]; next_cursor?: string}
type PageParam = {direction: 'forward' | 'backward'; cursor?: string; after?: number}
export type SessionSummary = {done: number; later: number; kept: number; seen: number}

export function River({view, subscriptions, actions, selected, onSelect, onOpen, shortcuts, counts, summary, since = '1970-01-01T00:00:00Z', sessionStart = '1970-01-01T00:00:00Z'}: {since?:string; sessionStart?:string; view: View; subscriptions: Subscription[]; actions: ReaderActions; selected: Entry | null; onSelect: (entry: Entry) => void; onOpen: (entry: Entry) => void; shortcuts: boolean; counts: boolean; summary: SessionSummary}) {
  const pipeline = usePipeline()
  const key = viewKey(view)
  const scroller = useRef<HTMLDivElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const seen = useRef(new Set<number>())
  const [error, setError] = useState('')
  const position = useQuery({queryKey: ['position', key], queryFn: ({signal}) => request<{seq: number}>(`/v1/positions/${encodeURIComponent(key)}`, {signal})})
  const list = useInfiniteQuery({
    queryKey: ['river', key, position.data?.seq], enabled: position.isSuccess,
    initialPageParam: {direction: 'forward', after: position.data?.seq ?? 0} as PageParam,
    queryFn: ({pageParam, signal}) => {
      const query = entryQuery(view)
      query.set('direction', pageParam.direction)
      if (pageParam.cursor) query.set('cursor', pageParam.cursor)
      if (pageParam.after) query.set('after_seq', String(pageParam.after))
      return request<Page>(`/v1/entries?${query}`, {signal})
    },
    getNextPageParam: (last): PageParam | undefined => last.next_cursor ? {direction: 'forward', cursor: String(Math.max(...last.entries.map(e => e.seq)))} : undefined,
    getPreviousPageParam: (first): PageParam | undefined => first.entries.length ? {direction: 'backward', cursor: String(Math.min(...first.entries.map(e => e.seq)))} : position.data?.seq ? {direction: 'backward', cursor: String(position.data.seq + 1)} : undefined,
    refetchOnWindowFocus: false, staleTime: Infinity,
  })
  const entries = orderEntries(list.data?.pages.flatMap(page => page.entries) ?? [], view, subscriptions)
  // eslint-disable-next-line react-hooks/incompatible-library -- Virtual owns mutable scroll measurements; this app does not use React Compiler.
  const virtual = useVirtualizer({count: entries.length, getScrollElement: () => scroller.current, estimateSize: () => 100, overscan: 5, getItemKey: index => entries[index].id})
  const visible = virtual.getVirtualItems()
  const ids = visible.map(item => entries[item.index]?.id).join(',')
  const actionsRef = useRef(actions)
  useEffect(() => { actionsRef.current = actions }, [actions])
  const entriesRef = useRef(entries)
  useEffect(() => { entriesRef.current = entries }, [entries])
  useEffect(() => { heading.current?.focus() }, [key])

  useEffect(() => {
    const root = scroller.current
    if (!root || !('IntersectionObserver' in window)) return
    const pending = new Set<number>()
    const intersecting = new Set<number>()
    const observer = new IntersectionObserver(records => {
      for (const record of records) {
        const id = Number((record.target as HTMLElement).dataset.entry)
        if (record.isIntersecting && record.intersectionRatio >= 0.6) {
          intersecting.add(id)
          if (!seen.current.has(id)) pending.add(id)
        } else {intersecting.delete(id);pending.delete(id)}
      }
    }, {root, threshold: 0.6})
    for (const row of root.querySelectorAll('[data-entry]')) observer.observe(row)
    const timer = window.setInterval(() => {
      const rendered = [...pending].map(id => entriesRef.current.find(entry => entry.id === id)).filter((entry): entry is Entry => entry !== undefined)
      for (const entry of rendered) {
        pending.delete(entry.id)
        if (seen.current.has(entry.id)) continue
        seen.current.add(entry.id)
        if (actionsRef.current.state(entry).seen.value !== 'seen') {
          void actionsRef.current.act(entry, 'seen', 'seen', 'passive').then(() => setError('')).catch(error => {
            seen.current.delete(entry.id)
            if (intersecting.has(entry.id)) pending.add(entry.id)
            setError(error instanceof Error ? error.message : 'Could not save reading progress.')
          })
        }
      }
      const last = rendered.at(-1)
      if (last) void request(`/v1/positions/${encodeURIComponent(key)}`, {method: 'PUT', body: JSON.stringify({seq: last.seq, op_id: crypto.randomUUID()})}).catch(error => setError(error instanceof Error ? error.message : 'Could not save position.'))
    }, 600)
    return () => { observer.disconnect(); window.clearInterval(timer) }
  }, [ids, key])

  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || !shortcutsAllowed(event.target, shortcuts)) return
      const index = entries.findIndex(entry => entry.id === selected?.id)
      const entry = entries[Math.max(0, index)]
      if (!entry) return
      if (event.key === 'j' || event.key === 'k') {
        event.preventDefault()
        const next = index < 0 ? (event.key === 'j' ? 0 : entries.length - 1) : Math.max(0, Math.min(entries.length - 1, index + (event.key === 'j' ? 1 : -1)))
        onSelect(entries[next]); virtual.scrollToIndex(next, {align: 'auto'}); return
      }
      if (event.key === 'o' || event.key === 'Enter') { if (event.target instanceof Element && event.target.closest('button, a')) return; event.preventDefault(); onOpen(entry); return }
      if (event.key === 'v') { event.preventDefault(); window.open(entry.url, '_blank', 'noopener,noreferrer'); return }
      const state = actions.state(entry)
      const action = event.key === 'e' ? ['shelf', 'done'] as const : event.key === 's' ? ['shelf', state.shelf.value === 'later' ? 'none' : 'later'] as const : event.key === 'f' ? ['kept', state.kept.value !== true] as const : event.key === 'm' ? ['seen', 'unseen'] as const : null
      if (action) { event.preventDefault(); void actions.act(entry, action[0], action[1]).catch(error => setError(error instanceof Error ? error.message : 'Could not save action.')) }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [entries, selected, shortcuts, onSelect, onOpen, virtual, actions])

  const unread = entries.filter(entry => { const state = actions.state(entry); return state.seen.value === 'unseen' && state.shelf.value !== 'done' }).length
  return <main className="river" aria-label="Articles">
    <header><h2 ref={heading} tabIndex={-1}>{view.title}</h2><button disabled={!list.isSuccess || list.isRefetching} onClick={() => {void list.refetch()}}>Refresh articles</button>{counts && <p>{unread} unread in loaded articles</p>}</header>
    {(error || position.error || list.error) && <p role="alert">{error || position.error?.message || list.error?.message} <button onClick={() => { void (position.isError ? position.refetch() : list.refetch()) }}>Try again</button></p>}
    {(position.isPending || list.isPending) && <p role="status">Finding your place…</p>}
    {view.kind === 'edition' && <Edition onOpen={onOpen}/>}
    <div className="river-scroll" ref={scroller} tabIndex={0} aria-label="Article list">
      {view.kind === 'today' && <TodayAgents since={since} onOpen={onOpen}/>}
      <button disabled={!list.isSuccess || list.isFetchingPreviousPage} onClick={() => { void list.fetchPreviousPage() }}>Earlier articles</button>
      <div className="virtual-list" style={{height: virtual.getTotalSize()}} role="list">
        {visible.map(row => {
          const entry = entries[row.index]
          const state = actions.state(entry)
          const source = subscriptions.find(sub => sub.id === entry.subscription_id)?.title || 'Feed'
          const group = view.kind === 'edition' && (row.index === 0 || entries[row.index - 1].subscription_id !== entry.subscription_id)
          return <div className="virtual-row" key={entry.id} role="listitem" data-entry={entry.id} style={{height: row.size, transform: `translateY(${row.start}px)`}}>
            {group && <h3 className="feed-group">{source}</h3>}
            <button className="entry" aria-current={selected?.id === entry.id ? 'true' : undefined} onFocus={() => onSelect(entry)} onClick={() => onOpen(entry)}><strong>{entry.title || 'Untitled'}</strong>{pipeline.status.blurb === 'enabled' && entry.blurb && <span>{entry.blurb}</span>}<span>{source} · {state.shelf.value === 'none' ? (state.seen.value === 'seen' ? 'Seen' : 'Unread') : state.shelf.value}{state.kept.value === true ? ' · Kept' : ''}</span></button>
          </div>
        })}
      </div>
      {list.hasNextPage && <button disabled={list.isFetchingNextPage} onClick={() => { void list.fetchNextPage() }}>More articles</button>}
      {list.isSuccess && !list.hasNextPage && <section className="session-end" aria-label="Session end"><h3>{entries.length ? 'That is everything in this view.' : emptyCopy()}</h3><p>Your choices this session: {summary.seen} seen, {summary.done} Done, {summary.later} Later, {summary.kept} Keep. Sync status shows any actions still waiting.</p><SessionChanges since={sessionStart}/><p>You can leave the rest here.</p></section>}
    </div>
  </main>
}
