import {useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent} from 'react'
import {useQuery} from '@tanstack/react-query'
import type {Entry, ReaderActions} from './reader'
import {request} from './request'
import {frontHref, type FrontPeriod} from './routes'
import {Link} from './router'
import {Icon} from './Icon'
import {longDate, shortAge} from './format'
import {FRONT_WINDOWS, storyRead, type FrontPage as FrontPageData, type FrontStory} from './front'
import {loadFrontPage, saveFrontPage, type StoredFrontPage} from './front-store'
import {useOnline} from './offline/Offline'
import {TodayAgents, SessionChanges} from './agents/Surfaces'

// The server owns ranking and selection. The client keeps the visit's order
// until the reader asks for the newer revision the background fetch found.
const BACKGROUND_REFRESH_MS = 30_000

function FrontImage({src}: {src?: string}) {
  const [broken, setBroken] = useState(false)
  if (!src || broken) return null
  return <img className="front-image" src={src} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />
}

function StorySummary({story, onOpen}: {story: FrontStory; onOpen: (entry: Entry) => void}) {
  const [citations, setCitations] = useState(false)
  const summary = story.summary
  if (!summary) return null
  const claims = summary.claims ?? []
  const passages = claims.flatMap(claim => claim.citations ?? [])
  return <div className="front-summary">
    {claims.map((claim, index) => <p className="front-claim" key={index}>{claim.text}</p>)}
    <p className="front-provenance"><span>Summary of linked coverage</span>{summary.model ? <> · <span>{summary.model}</span></> : null}{summary.prompt_version ? <> · prompt <span>{summary.prompt_version}</span></> : null} · <time dateTime={summary.generated_at}>{longDate(summary.generated_at)}</time></p>
    {summary.excerpt_notice && <p className="front-notice">{summary.excerpt_notice}</p>}
    {passages.length > 0 && <div className="front-citations">
      <button type="button" className="quiet small" aria-expanded={citations} onClick={() => setCitations(value => !value)}>Citations</button>
      {citations && <ul className="front-passages">
        {passages.map((citation, index) => <li key={index}>
          <span className="muted">{citation.verified ? 'Verified citation' : 'Citation not found in the article'}</span>
          {(citation.passages ?? []).map((passage, passageIndex) => {
            const target = (story.coverage ?? []).find(item => item.entry.id === citation.entry_id)
            return target
              ? <button type="button" className="front-passage" key={passageIndex} onClick={() => onOpen(target.entry)}>{passage}</button>
              : <p className="front-passage" key={passageIndex}>{passage}</p>
          })}
        </li>)}
      </ul>}
    </div>}
  </div>
}

function Story({story: original, period, lead, actions, expanded, onToggleCoverage, onOpen, onSelect}: {
  story: FrontStory
  period: FrontPeriod
  lead: boolean
  actions: ReaderActions
  expanded: boolean
  onToggleCoverage: (storyID: string) => void
  onOpen: (entry: Entry) => void
  onSelect: (entry: Entry) => void
}) {
  const deleted = (entry: Entry) => entry.deleted || actions.deleted?.(entry) === true
  const originalCoverage = original.coverage ?? []
  const coverage = originalCoverage.filter(item => !deleted(item.entry))
  // A deleted representative must not take the story down while other coverage
  // survives; the newest visible member becomes the headline link.
  const representative = coverage.find(item => item.entry.id === original.entry.id) ?? coverage[0]
  if (!representative) return null
  const sameOriginal = representative.entry.id === original.entry.id
  const live = new Set(coverage.map(item => item.entry.id))
  const story: FrontStory = {...original, coverage,
    source_title: sameOriginal ? original.source_title : representative.source_title,
    published_at: sameOriginal ? original.published_at : representative.entry.published_at || representative.entry.first_seen_at,
    feed_count: coverage.length === originalCoverage.length ? original.feed_count : new Set(coverage.map(item => item.entry.feed_id)).size,
    image_url: sameOriginal ? original.image_url : undefined,
    excerpt: sameOriginal ? original.excerpt : '',
    summary: original.summary?.claims?.every(claim => (claim.citations ?? []).every(citation => live.has(citation.entry_id))) ? original.summary : undefined,
  }
  const others = coverage.filter(item => item.entry.id !== representative.entry.id)
  const entry = representative.entry
  const title = entry.title || 'Untitled'
  const read = storyRead({...story, coverage}, actions.state)
  function open(event: ReactMouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    onOpen(entry)
  }
  return <article className={`front-story${lead ? ' lead' : ''}`} data-story={story.id} data-entry={entry.id}>
    {story.image_url && <FrontImage key={story.image_url} src={story.image_url} />}
    <div className="front-story-body">
      <h3 className="front-headline"><a href={frontHref(period, entry.id)} onClick={open} onFocus={() => onSelect(entry)}>{title}</a></h3>
      <p className="front-meta">
        <span className="front-source">{story.source_title}</span>
        <time dateTime={story.published_at} title={longDate(story.published_at)}>{shortAge(story.published_at)}</time>
        {story.feed_count > 1 && <span>Covered by {story.feed_count} feeds</span>}
        {read && <span className="front-read"><Icon name="check" size={14} /> Read</span>}
      </p>
      {story.summary && (story.summary.claims?.length ?? 0) > 0 ? <StorySummary story={story} onOpen={onOpen} />
        : story.excerpt ? <div className="front-excerpt-block">
          <p className="front-excerpt">{story.excerpt}</p>
          <p className="front-attribution">Excerpt from {story.source_title}</p>
        </div>
        : <p className="muted front-no-text">No summary or excerpt is available.</p>}
      {others.length > 0 && <div className="front-coverage">
        <button type="button" className="quiet small" aria-expanded={expanded} aria-label={`Other sources for ${title}`} onClick={() => onToggleCoverage(story.id)}>
          <Icon name="stack" size={16} />{expanded ? 'Hide other sources' : `Other sources (${others.length})`}
        </button>
        {expanded && <ul className="front-coverage-list">
          {others.map(item => <li key={item.entry.id}><a href={frontHref(period, item.entry.id)} onClick={event => {
            if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
            event.preventDefault()
            onOpen(item.entry)
          }}>{item.source_title} · {item.entry.title || 'Untitled'}</a></li>)}
        </ul>}
      </div>}
    </div>
  </article>
}

export function FrontPage({period, userID, actions, selected, onSelect, onOpen, since, sessionStart}: {
  period: FrontPeriod
  userID: number
  actions: ReaderActions
  selected: Entry | null
  onSelect: (entry: Entry) => void
  onOpen: (entry: Entry, mode?: 'push' | 'replace') => void
  since: string
  sessionStart: string
}) {
  const online = useOnline()
  const [stored] = useState<StoredFrontPage | undefined>(() => loadFrontPage(userID, period))
  const [page, setPage] = useState<StoredFrontPage | undefined>(stored)
  const [available, setAvailable] = useState<FrontPageData | undefined>()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const heading = useRef<HTMLHeadingElement>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])
  const pageRevision = page?.page.revision
  const query = useQuery({
    queryKey: ['front', userID, period],
    queryFn: ({signal}) => request<FrontPageData>(`/v1/front-page?period=${period}`, {signal}),
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: BACKGROUND_REFRESH_MS,
  })

  // A successful read replaces the visit's page only when it is the same
  // revision. A newer revision waits behind the explicit refresh offer.
  useEffect(() => {
    const data = query.data
    if (!data) return
    const updatedAt = new Date().toISOString()
    if (!pageRevision || pageRevision === data.revision) {
      const next = {page: data, updated_at: updatedAt}
      // The visit's page changes only here or through the explicit refresh.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setPage(next)
      setAvailable(undefined)
      saveFrontPage(userID, period, data, updatedAt)
      return
    }
    setAvailable(data)
    saveFrontPage(userID, period, data, updatedAt)
  }, [query.data, pageRevision, userID, period])

  const sections = useMemo(() => {
    const visible = (story: FrontStory) => (story.coverage ?? []).some(item => !item.entry.deleted && !actions.deleted?.(item.entry))
    return (page?.page.sections ?? []).map(section => ({
      name: section.name,
      stories: (section.stories ?? []).filter(visible),
    })).filter(section => section.stories.length > 0)
  }, [page, actions])
  const leadID = sections[0]?.stories[0]?.id
  const loading = query.isPending && !page
  const error = query.error as Error | null
  const updatedAt = page?.updated_at

  function applyAvailable() {
    if (!available) return
    const next = {page: available, updated_at: new Date().toISOString()}
    setPage(next)
    setAvailable(undefined)
    saveFrontPage(userID, period, available, next.updated_at)
  }

  async function refresh() {
    if (available) { applyAvailable(); return }
    const result = await query.refetch()
    if (!mounted.current || !result.isSuccess || !result.data) return
    const next = {page: result.data, updated_at: new Date().toISOString()}
    setPage(next)
    setAvailable(undefined)
    saveFrontPage(userID, period, result.data, next.updated_at)
  }

  function toggleCoverage(storyID: string) {
    setExpanded(previous => {
      const next = new Set(previous)
      if (next.has(storyID)) next.delete(storyID)
      else next.add(storyID)
      return next
    })
  }

  // Focus the masthead when the page first appears without an article open.
  useEffect(() => {
    if (selected) return
    const frame = window.requestAnimationFrame(() => heading.current?.focus({preventScroll: true}))
    return () => window.cancelAnimationFrame(frame)
  }, [selected])

  if (loading) return <section className="front" aria-label="Front page" aria-busy="true">
    <p role="status" className="sr-only">Loading the front page…</p>
    <div className="skeleton line title short" aria-hidden="true" />
    <div className="skeleton block" aria-hidden="true" />
    <div className="skeleton block" aria-hidden="true" />
  </section>

  if (!page) return <section className="front" aria-label="Front page">
    {error
      ? <div className="problem" role="alert"><h2>Front page did not load</h2><p>{error?.message ?? 'Something went wrong.'} Check your connection and try again.</p><p className="button-row"><button type="button" className="primary" onClick={() => {void query.refetch()}}>Try again</button></p></div>
      : <div className="empty-state"><h2>No stories in this window</h2><p>New coverage appears as your feeds publish.</p></div>}
  </section>

  const data = page.page
  const dated = data.selected_at || data.window_end
  return <section className="front" aria-label="Front page">
    <header className="front-masthead">
      <div>
        <h2 ref={heading} tabIndex={-1} className="front-title">Front page</h2>
        <p className="front-date"><time dateTime={dated} title={longDate(dated)}>{longDate(dated)}</time></p>
      </div>
      <div className="front-tools">
        <p className="front-updated">Updated <time dateTime={updatedAt}>{longDate(updatedAt)}</time>{!online && <span className="front-offline"> · Offline</span>}</p>
        <button type="button" className="quiet small" disabled={query.isFetching} onClick={refresh}><Icon name="refresh" size={16} />Refresh</button>
      </div>
      <nav className="front-tabs" aria-label="Front page window">
        {FRONT_WINDOWS.map(window => {
          const current = window.period === period
          return <Link key={window.period} aria-current={current ? 'page' : undefined} aria-label={`${window.name}, ${window.label}`} className={`front-tab${current ? ' current' : ''}`} to={frontHref(window.period)}>
            <span className="front-tab-name">{window.name}</span>
            <span className="front-tab-window">{window.label}</span>
          </Link>
        })}
      </nav>
      {query.isFetching && <p role="status" className="front-updating">Updating…</p>}
      {available && <p role="status" className="front-available"><span>New coverage available</span> <button type="button" className="quiet small" onClick={applyAvailable}>Refresh</button></p>}
      {query.isError && page && <p role="alert" className="banner inline">Could not refresh the front page. Showing the last update from <time dateTime={updatedAt}>{longDate(updatedAt)}</time>. <button type="button" className="quiet small" onClick={() => {void query.refetch()}}>Try again</button></p>}
    </header>
    <div className="front-agents">
      <TodayAgents since={since} onOpen={entry => onOpen(entry)} />
    </div>
    {sections.length === 0
      ? <div className="empty-state"><h2>No stories in this window</h2><p>Nothing matched the past {period === 'weekly' ? '7 days' : '24 hours'}.</p></div>
      : <div className="front-sections">
        {sections.map(section => <section className="front-section" key={section.name} aria-labelledby={`front-section-${section.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}`}>
          <h3 className="front-section-name" id={`front-section-${section.name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}`}>{section.name}</h3>
          <div className="front-stories">
            {section.stories.map(story => <Story key={story.id} story={story} period={period} lead={story.id === leadID} actions={actions} expanded={expanded.has(story.id)} onToggleCoverage={toggleCoverage} onOpen={entry => onOpen(entry)} onSelect={onSelect} />)}
          </div>
        </section>)}
      </div>}
    <div className="front-session">
      <SessionChanges since={sessionStart} />
    </div>
  </section>
}
