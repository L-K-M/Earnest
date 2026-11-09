import {useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent} from 'react'
import {useInfiniteQuery, useQuery} from '@tanstack/react-query'
import {useVirtualizer} from '@tanstack/react-virtual'
import {emptyCopy} from './copy'
import {entryQuery, orderEntries, shortcutsAllowed, viewKey, type Entry, type EntryState, type ReaderActions, type Subscription, type View} from './reader'
import {request} from './request'
import {TodayAgents, SessionChanges} from './agents/Surfaces'
import {Edition} from './pipeline/Pipeline'
import {usePipeline} from './search/pipeline'
import {Icon} from './Icon'
import {longDate, shortAge} from './format'
import {viewHref} from './routes'
import {Link, whenPresent} from './router'
import {explainHealth} from './feeds'

type ClusterGroup = {entry: Entry; also_in: number}
type Page = {entries: Entry[]; next_cursor?: string; groups?: ClusterGroup[]}
type PageParam = {direction: 'forward' | 'backward'; cursor?: string; after?: number}
type OpenMode = 'push' | 'replace'
export type SessionSummary = {done: number; later: number; kept: number; seen: number}
const seenWriteInterval = 600
const SKELETON_ROWS = 7

// Passive position writes retry after a failure with this backoff, so a long
// outage cannot cost a write per tick. The floor sits above one interval so a
// failure skips at least a tick before the next attempt.
const POSITION_RETRY_MIN_MS = seenWriteInterval * 2
const POSITION_RETRY_MAX_MS = 30_000

// Done and Later are shown on the row; the read state is carried by weight
// and a dot, and spelled out for screen readers.
function shelfLabel(state: EntryState): string {
  if (state.shelf.value === 'done') return 'Done'
  if (state.shelf.value === 'later') return 'Later'
  return ''
}

// Each list explains its own emptiness. Wit is reserved for the river.
function emptyLine(view: View): string {
  switch (view.kind) {
  case 'later': return 'Nothing waiting for later. Press s on an article to put it here.'
  case 'kept': return 'Nothing kept yet. Press f on an article to keep it for good.'
  case 'done': return 'Nothing marked Done yet.'
  case 'edition': return 'This week’s edition is empty. Feeds with the This week cadence collect here.'
  case 'label': return 'No articles carry this label yet.'
  case 'feed': return 'Nothing new from this feed.'
  case 'folder': return 'Nothing new in this folder.'
  default: return emptyCopy()
  }
}

function newTabClick(event: ReactMouseEvent): boolean {
  return event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey
}

export function River({view, subscriptions, actions, selected, onSelect, onOpen, shortcuts, counts, summary, onNewestLoaded, reading = false, since = '1970-01-01T00:00:00Z', sessionStart = '1970-01-01T00:00:00Z'}: {since?:string; sessionStart?:string; onNewestLoaded?: (seq: number) => void; view: View; subscriptions: Subscription[]; actions: ReaderActions; selected: Entry | null; onSelect: (entry: Entry) => void; onOpen: (entry: Entry, mode?: OpenMode) => void; shortcuts: boolean; counts: boolean; summary: SessionSummary; reading?: boolean}) {
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
  const focusNext = useRef<number | null>(null)
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
  const virtual = useVirtualizer({count: entries.length, getScrollElement: () => scroller.current, estimateSize: () => 76, overscan: 6, scrollMargin, getItemKey: index => entries[index].id})
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
  // Report the newest loaded entry so the palette's "Mark the rest read" never
  // marks past what this view has shown. A ref keeps the parent's inline
  // callback from re-running the effect on every render.
  const newestLoaded = entries.reduce((max, entry) => Math.max(max, entry.seq), 0)
  const onNewestLoadedRef = useRef(onNewestLoaded)
  useEffect(() => { onNewestLoadedRef.current = onNewestLoaded }, [onNewestLoaded])
  useEffect(() => { onNewestLoadedRef.current?.(newestLoaded) }, [newestLoaded])
  // Passive progress only ever advances the resume position: a row that
  // becomes due after a further row was already saved must not drag "where
  // you left off" backwards, and a failed write retries with backoff instead
  // of being lost or hammered out. The high-water state is per view: it
  // resets when the view key changes, then the fetched position reseeds it.
  const targetSeq = useRef(0)
  const resumeSeq = useRef(0)
  const pendingSeq = useRef<number | null>(null)
  const retryDelay = useRef(0)
  const retryAfter = useRef(0)
  useEffect(() => {
    targetSeq.current = 0
    resumeSeq.current = 0
    pendingSeq.current = null
    retryDelay.current = 0
    retryAfter.current = 0
  }, [key])
  useEffect(() => { if (position.data) resumeSeq.current = Math.max(resumeSeq.current, position.data.seq) }, [position.data])
  // A newly chosen list takes focus at its heading, unless an article is
  // opening alongside it (the reader takes focus then).
  const readingRef = useRef(reading)
  useEffect(() => { readingRef.current = reading }, [reading])
  // A frame later, so a closing dialog (the palette) restores its own focus
  // first and the new list still ends up with it.
  useEffect(() => {
    if (readingRef.current) return
    const frame = window.requestAnimationFrame(() => heading.current?.focus({preventScroll: true}))
    return () => window.cancelAnimationFrame(frame)
  }, [key])

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

  // Keyboard movement inside the list carries focus with the selection; the
  // target row may need a frame to render after scrollToIndex.
  useEffect(() => {
    const id = focusNext.current
    if (id === null) return
    return whenPresent(() => listRef.current?.querySelector<HTMLElement>(`[data-entry="${id}"] .entry`), row => {
      focusNext.current = null
      row.focus({preventScroll: true})
    }, 10)
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
      const reached = due.reduce((max, entry) => Math.max(max, entry.seq), targetSeq.current)
      targetSeq.current = reached
      if (reached > resumeSeq.current && pendingSeq.current === null && Date.now() >= retryAfter.current) {
        pendingSeq.current = reached
        void request(`/v1/positions/${encodeURIComponent(key)}`, {method: 'PUT', body: JSON.stringify({seq: reached, op_id: crypto.randomUUID()})})
          .then(() => { resumeSeq.current = Math.max(resumeSeq.current, reached); retryDelay.current = 0 })
          .catch(error => {
            // Back off instead of retrying every tick through a long outage.
            retryDelay.current = Math.min(Math.max(retryDelay.current * 2, POSITION_RETRY_MIN_MS), POSITION_RETRY_MAX_MS)
            retryAfter.current = Date.now() + retryDelay.current
            setError(error instanceof Error ? error.message : 'Could not save position.')
          })
          .finally(() => { pendingSeq.current = null })
      }
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

  const open = useCallback((entry: Entry, mode?: OpenMode) => {
    pending.current.add(entry.id)
    onOpen(entry, mode)
  }, [onOpen])

  // move selects the row at `index`; in reading mode it opens it in place.
  const move = useCallback((index: number, from: number, focus: boolean) => {
    const target = entries[index]
    if (!target) return
    // Moving the selection past a row is the keyboard form of scrolling past it.
    if (from >= 0) pending.current.add(entries[from].id)
    onSelect(target)
    virtual.scrollToIndex(index, {align: 'auto'})
    if (focus) focusNext.current = target.id
    if (reading && from !== index) open(target, 'replace')
  }, [entries, onSelect, virtual, reading, open])

  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || !shortcutsAllowed(event.target, shortcuts)) return
      const index = entries.findIndex(entry => entry.id === selected?.id)
      const entry = entries[Math.max(0, index)]
      if (!entry) return
      if (event.key === 'j' || event.key === 'k') {
        event.preventDefault()
        const next = index < 0 ? (event.key === 'j' ? 0 : entries.length - 1) : Math.max(0, Math.min(entries.length - 1, index + (event.key === 'j' ? 1 : -1)))
        const inList = event.target instanceof Node && Boolean(scroller.current?.contains(event.target))
        move(next, index, inList)
        return
      }
      // Enter belongs to whatever control has focus (a link, a button, the
      // summary of a brief); it opens the selection only from plain content.
      if (event.key === 'o' || event.key === 'Enter') { if (event.key === 'Enter' && event.target instanceof Element && event.target.closest('button, a, summary, label, [role="option"]')) return; event.preventDefault(); open(entry); return }
      if (event.key === 'v') { event.preventDefault(); window.open(entry.url, '_blank', 'noopener,noreferrer'); return }
      const state = actions.state(entry)
      const action = event.key === 'e' ? ['shelf', 'done'] as const : event.key === 's' ? ['shelf', state.shelf.value === 'later' ? 'none' : 'later'] as const : event.key === 'f' ? ['kept', state.kept.value !== true] as const : event.key === 'm' ? ['seen', 'unseen'] as const : null
      if (action) { event.preventDefault(); if (action[0] === 'seen') pending.current.delete(entry.id); void actions.act(entry, action[0], action[1]).catch(error => setError(error instanceof Error ? error.message : 'Could not save action.')) }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [entries, selected, shortcuts, open, move, actions])

  // Arrow keys work in the list whether or not single-key shortcuts are on.
  function listKeys(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.ctrlKey || event.metaKey || event.altKey || !entries.length) return
    const index = entries.findIndex(entry => entry.id === selected?.id)
    const next = event.key === 'ArrowDown' ? Math.min(entries.length - 1, index + 1)
      : event.key === 'ArrowUp' ? Math.max(0, index < 0 ? 0 : index - 1)
      : event.key === 'Home' ? 0
      : event.key === 'End' ? entries.length - 1
      : null
    if (next === null) return
    event.preventDefault()
    move(next, index, true)
  }

  const unread = entries.filter(entry => { const state = actions.state(entry); return state.seen.value === 'unseen' && state.shelf.value !== 'done' }).length
  const loading = position.isPending || list.isPending
  const failure = error || position.error?.message || list.error?.message
  const feed = view.kind === 'feed' ? subscriptions.find(sub => sub.id === view.id) : undefined
  const feedProblem = feed ? explainHealth(feed.health) : null
  const rowsShown = new Set(visible.map(row => entries[row.index]?.id))
  const tabStop = selected && rowsShown.has(selected.id) ? selected.id : entries[visible[0]?.index ?? 0]?.id
  return <section className="river" aria-label="Articles">
    <header className="pane-header">
      <div className="pane-heading">
        <h2 ref={heading} tabIndex={-1}>{view.title}</h2>
        <button type="button" className="icon-button" title="Refresh articles" disabled={!list.isSuccess || list.isRefetching} onClick={() => {void list.refetch()}}><Icon name="refresh" /><span className="sr-only">Refresh articles</span></button>
      </div>
      <div className="pane-tools">
        <button type="button" className="quiet small" disabled={!list.isSuccess || list.isFetchingPreviousPage} onClick={() => { void list.fetchPreviousPage() }}><Icon name="earlier" size={16} />Earlier articles</button>
        <button type="button" className="quiet small toggle" aria-pressed={cluster} disabled={!list.isSuccess || list.isFetching} onClick={() => setCluster(value => !value)}><Icon name="stack" size={16} />One per story</button>
        {view.kind === 'feed' && <Link className="pane-link" to={`/feeds#subscription-${view.id}`}>Feed settings</Link>}
        {counts && list.isSuccess && <p className="pane-count">{unread} unread in loaded {cluster ? 'stories' : 'articles'}</p>}
      </div>
    </header>
    {feedProblem && <p className="banner inline" role="status"><strong>Failing:</strong> {feedProblem} <Link to={`/feeds#subscription-${view.id}`}>Fix it in feed settings</Link></p>}
    {failure && <p role="alert" className="banner problem inline">{failure} <button type="button" onClick={() => { setError(''); void (position.isError ? position.refetch() : list.refetch()) }}>Try again</button></p>}
    {view.kind === 'edition' && <Edition onOpen={onOpen}/>}
    <div className="river-scroll" ref={scroller} onScroll={markScrolledPast} onKeyDown={listKeys} tabIndex={-1} aria-label="Article list" aria-busy={loading || undefined}>
      {view.kind === 'today' && <TodayAgents since={since} onOpen={onOpen}/>}
      {loading && <div className="skeleton-rows" aria-hidden="true">{Array.from({length: SKELETON_ROWS}, (_, index) => <div key={index} className="skeleton-row"><div className="skeleton line" /><div className="skeleton line short" /></div>)}</div>}
      {loading && <p role="status" className="sr-only">Finding your place…</p>}
      <div ref={listRef} className="virtual-list" style={{height: virtual.getTotalSize()}} role="list">
        {visible.map(row => {
          const entry = entries[row.index]
          const state = actions.state(entry)
          const source = subscriptions.find(sub => sub.id === entry.subscription_id)?.title || 'Feed'
          const group = view.kind === 'edition' && (row.index === 0 || entries[row.index - 1].subscription_id !== entry.subscription_id)
          const alsoIn = clusterInfo.get(entry.id) ?? 0
          const isUnread = state.seen.value !== 'seen' && state.shelf.value !== 'done'
          const shelf = shelfLabel(state)
          const date = entry.published_at || entry.first_seen_at
          return <div className="virtual-row" key={entry.id} role="listitem" data-entry={entry.id} data-index={row.index} ref={virtual.measureElement} style={{transform: `translateY(${row.start - virtual.options.scrollMargin}px)`}}>
            {group && <h3 className="feed-group">{source}</h3>}
            <a className="entry" href={viewHref({kind: view.kind, id: view.id}, entry.id)} tabIndex={entry.id === tabStop ? 0 : -1} data-unread={isUnread || undefined} data-selected={selected?.id === entry.id || undefined} aria-current={reading && selected?.id === entry.id ? 'page' : undefined} onFocus={() => onSelect(entry)} onClick={event => { if (newTabClick(event)) return; event.preventDefault(); open(entry) }}>
              <span className="entry-title">{isUnread && <span className="unread-dot" aria-hidden="true" />}{entry.title || 'Untitled'}</span>
              {pipeline.status.blurb === 'enabled' && entry.blurb && <span className="entry-blurb">{entry.blurb}</span>}
              <span className="entry-meta"><span className="entry-source">{source}{shelf && ` · ${shelf}`}{state.kept.value === true ? ' · Kept' : ''}{cluster && alsoIn > 0 ? ` · also in ${alsoIn}` : ''}{isUnread && <span className="sr-only"> · Unread</span>}</span>{date && <time dateTime={date} title={longDate(date)}>{shortAge(date)}</time>}</span>
            </a>
          </div>
        })}
      </div>
      {list.hasNextPage && <p className="more-row"><button type="button" disabled={list.isFetchingNextPage} onClick={() => { void list.fetchNextPage() }}>{list.isFetchingNextPage ? 'Loading more…' : 'More articles'}</button></p>}
      {list.isSuccess && !list.hasNextPage && <section className="session-end" aria-label="Session end">
        <h3>{entries.length ? 'That is everything in this view.' : emptyLine(view)}</h3>
        {(summary.seen + summary.done + summary.later + summary.kept > 0) && <p>Your choices this session: {summary.seen} seen, {summary.done} Done, {summary.later} Later, {summary.kept} Keep. Sync status shows any actions still waiting.</p>}
        <SessionChanges since={sessionStart}/>
        {entries.length > 0 && <p className="muted">You can leave the rest here.</p>}
      </section>}
    </div>
  </section>
}
