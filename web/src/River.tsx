import {useCallback, useEffect, useRef, useState} from 'react'
import {useInfiniteQuery, useQuery} from '@tanstack/react-query'
import {useVirtualizer} from '@tanstack/react-virtual'
import {emptyCopy} from './copy'
import {entryQuery, orderEntries, shortcutsAllowed, viewKey, type Entry, type EntryState, type ReaderActions, type Subscription, type View} from './reader'
import {request} from './request'
import {TodayAgents, SessionChanges} from './agents/Surfaces'
import {Edition} from './pipeline/Pipeline'
import {usePipeline} from './search/pipeline'

type ClusterGroup = {entry: Entry; also_in: number}
type Page = {entries: Entry[]; next_cursor?: string; groups?: ClusterGroup[]}
type PageParam = {direction: 'forward' | 'backward'; cursor?: string; after?: number}
export type SessionSummary = {done: number; later: number; kept: number; seen: number}
const seenWriteInterval = 600

// Shelf values are stored lowercase; row copy is title case.
function statusLabel(state: EntryState): string {
  if (state.shelf.value === 'done') return 'Done'
  if (state.shelf.value === 'later') return 'Later'
  return state.seen.value === 'seen' ? 'Seen' : 'Unread'
}

export function River({view, subscriptions, actions, selected, onSelect, onOpen, shortcuts, counts, summary, since = '1970-01-01T00:00:00Z', sessionStart = '1970-01-01T00:00:00Z'}: {since?:string; sessionStart?:string; view: View; subscriptions: Subscription[]; actions: ReaderActions; selected: Entry | null; onSelect: (entry: Entry) => void; onOpen: (entry: Entry) => void; shortcuts: boolean; counts: boolean; summary: SessionSummary}) {
  const pipeline = usePipeline()
  const key = viewKey(view)
  const scroller = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const [scrollMargin, setScrollMargin] = useState(0)
  const heading = useRef<HTMLHeadingElement>(null)
  const seen = useRef(new Set<number>())
  const pending = useRef(new Set<number>())
  const rendered = useRef(new Map<number, {start: number; size: number}>())
  const passed = useRef(new Set<number>())
  const [error, setError] = useState('')
  const [cluster, setCluster] = useState(false)
  const position = useQuery({queryKey: ['position', key], queryFn: ({signal}) => request<{seq: number}>(`/v1/positions/${encodeURIComponent(key)}`, {signal})})
  const list = useInfiniteQuery({
    queryKey: ['river', key, position.data?.seq, cluster], enabled: position.isSuccess,
    initialPageParam: {direction: 'forward', after: position.data?.seq ?? 0} as PageParam,
    queryFn: ({pageParam, signal}) => {
      const query = entryQuery(view, cluster ? 'cluster' : undefined)
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
  const clusterInfo = new Map<number, number>()
  for (const page of list.data?.pages ?? []) {
    for (const group of page.groups ?? []) clusterInfo.set(group.entry.id, group.also_in)
  }
  // Rows are measured (measureElement on each row) so long titles, blurbs and
  // zoomed text are never clipped; estimateSize is only the pre-measure guess.
  // eslint-disable-next-line react-hooks/incompatible-library -- Virtual owns mutable scroll measurements; this app does not use React Compiler.
  const virtual = useVirtualizer({count: entries.length, getScrollElement: () => scroller.current, estimateSize: () => 100, overscan: 5, scrollMargin, getItemKey: index => entries[index].id})
  // The Today panel sits above the list inside the scroller, so the list's own
  // top is offset from the scroller's content origin. The virtualizer needs
  // that offset as scrollMargin to report row positions in scroller
  // coordinates; the seen logic and row transforms then line up.
  const measureScrollMargin = useCallback(() => {
    const root = scroller.current
    const element = listRef.current
    if (!root || !element) return
    setScrollMargin(element.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop)
  }, [])
  useEffect(() => {
    measureScrollMargin()
    const root = scroller.current
    const element = listRef.current
    if (typeof ResizeObserver === 'undefined' || !root || !element) return
    const observer = new ResizeObserver(measureScrollMargin)
    observer.observe(element)
    const panel = root.firstElementChild
    if (panel && panel !== element) observer.observe(panel)
    return () => observer.disconnect()
  }, [measureScrollMargin])
  const visible = virtual.getVirtualItems()
  const actionsRef = useRef(actions)
  useEffect(() => { actionsRef.current = actions }, [actions])
  const entriesRef = useRef(entries)
  useEffect(() => { entriesRef.current = entries }, [entries])
  useEffect(() => { heading.current?.focus() }, [key])

  // Remember which rendered rows have been inside the viewport. A row that
  // mounts already above the fold (a jump or a prepended page) never enters
  // this set, so it stays unseen until the reader actually scrolls to it.
  useEffect(() => {
    const root = scroller.current
    const top = root?.scrollTop ?? 0
    const bottom = top + (root?.clientHeight || window.innerHeight)
    const next = new Map<number, {start: number; size: number}>()
    for (const row of visible) {
      const entry = entries[row.index]
      if (!entry) continue
      next.set(entry.id, {start: row.start, size: row.size})
      if (row.start < bottom && row.start + row.size > top) passed.current.add(entry.id)
    }
    rendered.current = next
  })

  // The interval drains the queue so passive writes are batched and a failed
  // write is retried without blocking scrolling.
  useEffect(() => {
    const timer = window.setInterval(() => {
      const due = [...pending.current].map(id => entriesRef.current.find(entry => entry.id === id)).filter((entry): entry is Entry => entry !== undefined)
      for (const entry of due) {
        pending.current.delete(entry.id)
        if (seen.current.has(entry.id)) continue
        seen.current.add(entry.id)
        if (actionsRef.current.state(entry).seen.value !== 'seen') {
          void actionsRef.current.act(entry, 'seen', 'seen', 'passive').then(() => setError('')).catch(error => {
            seen.current.delete(entry.id)
            pending.current.add(entry.id)
            setError(error instanceof Error ? error.message : 'Could not save reading progress.')
          })
        }
      }
      const last = due.at(-1)
      if (last) void request(`/v1/positions/${encodeURIComponent(key)}`, {method: 'PUT', body: JSON.stringify({seq: last.seq, op_id: crypto.randomUUID()})}).catch(error => setError(error instanceof Error ? error.message : 'Could not save position.'))
    }, seenWriteInterval)
    return () => window.clearInterval(timer)
  }, [key])

  function markScrolledPast() {
    const root = scroller.current
    if (!root) return
    const top = root.scrollTop
    for (const [id, row] of rendered.current) {
      if (row.start + row.size > top) continue
      if (!passed.current.delete(id)) continue
      pending.current.add(id)
    }
  }

  const open = useCallback((entry: Entry) => {
    pending.current.add(entry.id)
    onOpen(entry)
  }, [onOpen])

  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || !shortcutsAllowed(event.target, shortcuts)) return
      const index = entries.findIndex(entry => entry.id === selected?.id)
      const entry = entries[Math.max(0, index)]
      if (!entry) return
      if (event.key === 'j' || event.key === 'k') {
        event.preventDefault()
        // Moving the selection past a row is the keyboard form of scrolling past it.
        if (index >= 0) pending.current.add(entries[index].id)
        const next = index < 0 ? (event.key === 'j' ? 0 : entries.length - 1) : Math.max(0, Math.min(entries.length - 1, index + (event.key === 'j' ? 1 : -1)))
        onSelect(entries[next]); virtual.scrollToIndex(next, {align: 'auto'}); return
      }
      if (event.key === 'o' || event.key === 'Enter') { if (event.target instanceof Element && event.target.closest('button, a')) return; event.preventDefault(); open(entry); return }
      if (event.key === 'v') { event.preventDefault(); window.open(entry.url, '_blank', 'noopener,noreferrer'); return }
      const state = actions.state(entry)
      const action = event.key === 'e' ? ['shelf', 'done'] as const : event.key === 's' ? ['shelf', state.shelf.value === 'later' ? 'none' : 'later'] as const : event.key === 'f' ? ['kept', state.kept.value !== true] as const : event.key === 'm' ? ['seen', 'unseen'] as const : null
      if (action) { event.preventDefault(); if (action[0] === 'seen') pending.current.delete(entry.id); void actions.act(entry, action[0], action[1]).catch(error => setError(error instanceof Error ? error.message : 'Could not save action.')) }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [entries, selected, shortcuts, onSelect, open, virtual, actions])

  const unread = entries.filter(entry => { const state = actions.state(entry); return state.seen.value === 'unseen' && state.shelf.value !== 'done' }).length
  return <main className="river" aria-label="Articles">
    <header><h2 ref={heading} tabIndex={-1}>{view.title}</h2><button disabled={!list.isSuccess || list.isRefetching} onClick={() => {void list.refetch()}}>Refresh articles</button><button disabled={!list.isSuccess || list.isFetchingPreviousPage} onClick={() => { void list.fetchPreviousPage() }}>Earlier articles</button><button aria-pressed={cluster} disabled={!list.isSuccess || list.isFetching} onClick={() => setCluster(value => !value)}>One per story</button>{counts && <p>{unread} unread in loaded {cluster ? 'stories' : 'articles'}</p>}</header>
    {(error || position.error || list.error) && <p role="alert">{error || position.error?.message || list.error?.message} <button onClick={() => { void (position.isError ? position.refetch() : list.refetch()) }}>Try again</button></p>}
    {(position.isPending || list.isPending) && <p role="status">Finding your place…</p>}
    {view.kind === 'edition' && <Edition onOpen={onOpen}/>}
    <div className="river-scroll" ref={scroller} onScroll={markScrolledPast} tabIndex={0} aria-label="Article list">
      {view.kind === 'today' && <TodayAgents since={since} onOpen={onOpen}/>}
      <div ref={listRef} className="virtual-list" style={{height: virtual.getTotalSize()}} role="list">
        {visible.map(row => {
          const entry = entries[row.index]
          const state = actions.state(entry)
          const source = subscriptions.find(sub => sub.id === entry.subscription_id)?.title || 'Feed'
          const group = view.kind === 'edition' && (row.index === 0 || entries[row.index - 1].subscription_id !== entry.subscription_id)
          const alsoIn = clusterInfo.get(entry.id) ?? 0
          return <div className="virtual-row" key={entry.id} role="listitem" data-entry={entry.id} data-index={row.index} ref={virtual.measureElement} style={{transform: `translateY(${row.start - virtual.options.scrollMargin}px)`}}>
            {group && <h3 className="feed-group">{source}</h3>}
            <button className="entry" aria-current={selected?.id === entry.id ? 'true' : undefined} onFocus={() => onSelect(entry)} onClick={() => open(entry)}><strong>{entry.title || 'Untitled'}</strong>{pipeline.status.blurb === 'enabled' && entry.blurb && <span>{entry.blurb}</span>}<span>{source} · {statusLabel(state)}{state.kept.value === true ? ' · Kept' : ''}{cluster && alsoIn > 0 ? ` · also in ${alsoIn}` : ''}</span></button>
          </div>
        })}
      </div>
      {list.hasNextPage && <button disabled={list.isFetchingNextPage} onClick={() => { void list.fetchNextPage() }}>More articles</button>}
      {list.isSuccess && !list.hasNextPage && <section className="session-end" aria-label="Session end"><h3>{entries.length ? 'That is everything in this view.' : emptyCopy()}</h3><p>Your choices this session: {summary.seen} seen, {summary.done} Done, {summary.later} Later, {summary.kept} Keep. Sync status shows any actions still waiting.</p><SessionChanges since={sessionStart}/><p>You can leave the rest here.</p></section>}
    </div>
  </main>
}
