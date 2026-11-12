// The web client's URL contract. Every screen a reader might bookmark, share
// or return to with Back has a path here. This module is pure: it parses a
// location into a Route and formats a Route back into a path, nothing else.
//
//   /                          Front page, past 24 hours
//   /front?period=daily|weekly /front/articles/:entry
//   /articles /articles/:entry                  every article, newest first
//   /today /edition                             legacy aliases of the front page
//   /later /kept /done
//   /feeds/:id  /folders/:id  /labels/:id        one subscription, folder, label
//   <any of the above>/articles/:entry          the same list with an article open
//   /search?q=&mode=hybrid&feed=&folder=&limit=  /search/saved/:id
//   /ask  /rules  /rules/new  /rules/:id
//   /activity?agent=&action=&since=
//   /notifications  /notifications/:id
//   /feeds                                       subscription management
//   /settings  /device?user_code=                device approval lives in settings
import type {View} from './reader'

export type ViewKind = View['kind']
export type ViewRef = {kind: ViewKind; id?: number}
export type FrontPeriod = 'daily' | 'weekly'
export type FrontTarget = {kind: 'front'; period: FrontPeriod}
export type SearchRoute = {q: string; mode?: 'hybrid'; feed?: number; folder?: number; limit?: number}
export type ActivityFilters = {agent?: string; action?: string; since?: string}

export type Route =
  | {name: 'river'; view: ViewRef; entry?: number}
  | {name: 'front'; period: FrontPeriod; entry?: number}
  | {name: 'search'; search: SearchRoute; saved?: number}
  | {name: 'ask'}
  | {name: 'rules'; rule?: number | 'new'}
  | {name: 'activity'; filters: ActivityFilters}
  | {name: 'notifications'; id?: number}
  | {name: 'feeds'}
  | {name: 'settings'; device: boolean}
  | {name: 'not-found'}

export const LIST_VIEWS = ['today', 'edition', 'later', 'kept', 'done'] as const
type ListView = typeof LIST_VIEWS[number]
const RESOURCE_SEGMENTS = {feeds: 'feed', folders: 'folder', labels: 'label'} as const
type ResourceSegment = keyof typeof RESOURCE_SEGMENTS
const SEGMENT_FOR_KIND = {feed: 'feeds', folder: 'folders', label: 'labels'} as const

// Legacy names retained for deep links and the handful of callers that still
// describe a destination rather than a screen: Today is the daily front page,
// Edition is the weekly one.
export const TODAY: ViewRef = {kind: 'today'}
export const WEEK: ViewRef = {kind: 'edition'}

// IDs are positive safe integers. Anything else in an ID position makes the
// whole path unknown rather than silently addressing a different item.
function positiveID(segment: string | undefined): number | undefined {
  if (segment === undefined || !/^[1-9]\d{0,15}$/.test(segment)) return undefined
  const value = Number(segment)
  return Number.isSafeInteger(value) ? value : undefined
}

function isListView(value: string): value is ListView {
  return (LIST_VIEWS as readonly string[]).includes(value)
}

function isResourceSegment(value: string): value is ResourceSegment {
  return Object.hasOwn(RESOURCE_SEGMENTS, value)
}

function segments(pathname: string): string[] | null {
  try {
    return pathname.split('/').filter(Boolean).map(decodeURIComponent)
  } catch {
    return null
  }
}

// river parses "<view path>[/articles/:entry]" once the view itself is known.
function river(view: ViewRef, rest: string[]): Route {
  if (rest.length === 0) return {name: 'river', view}
  const entry = positiveID(rest[1])
  if (rest.length === 2 && rest[0] === 'articles' && entry !== undefined) return {name: 'river', view, entry}
  return {name: 'not-found'}
}

// The front page is one screen with two windows. Old Today/Edition links keep
// their old destination meaning, including a deep link to an open article.
function front(params: URLSearchParams, rest: string[], legacy?: FrontPeriod): Route {
  const period: FrontPeriod = legacy ?? (params.get('period') === 'weekly' ? 'weekly' : 'daily')
  if (rest.length === 0) return {name: 'front', period}
  const entry = positiveID(rest[1])
  if (rest.length === 2 && rest[0] === 'articles' && entry !== undefined) return {name: 'front', period, entry}
  return {name: 'not-found'}
}

function searchRoute(params: URLSearchParams): SearchRoute {
  const search: SearchRoute = {q: params.get('q')?.trim() ?? ''}
  if (params.get('mode') === 'hybrid') search.mode = 'hybrid'
  const feed = positiveID(params.get('feed') ?? undefined)
  const folder = positiveID(params.get('folder') ?? undefined)
  const limit = positiveID(params.get('limit') ?? undefined)
  if (feed !== undefined) search.feed = feed
  if (folder !== undefined) search.folder = folder
  if (limit !== undefined) search.limit = limit
  return search
}

// realDay accepts YYYY-MM-DD only when it names a calendar day; 2026-02-31
// would otherwise roll over to March and 2026-13-45 would not parse at all.
function realDay(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const time = Date.parse(`${value}T00:00:00Z`)
  return !Number.isNaN(time) && new Date(time).toISOString().startsWith(value)
}

function activityFilters(params: URLSearchParams): ActivityFilters {
  const filters: ActivityFilters = {}
  const agent = params.get('agent')
  const action = params.get('action')
  const since = params.get('since')
  if (agent && positiveID(agent) !== undefined) filters.agent = agent
  if (action) filters.action = action
  if (since && realDay(since)) filters.since = since
  return filters
}

export function parseRoute(pathname: string, search = ''): Route {
  const parts = segments(pathname)
  if (parts === null) return {name: 'not-found'}
  const params = new URLSearchParams(search)
  const [head, ...rest] = parts
  if (head === undefined) return {name: 'front', period: 'daily'}
  if (head === 'front') return front(params, rest)
  if (head === 'today') return front(params, rest, 'daily')
  if (head === 'edition') return front(params, rest, 'weekly')
  if (head === 'articles') {
    if (rest.length === 0) return {name: 'river', view: {kind: 'all'}}
    const entry = positiveID(rest[0])
    return rest.length === 1 && entry !== undefined ? {name: 'river', view: {kind: 'all'}, entry} : {name: 'not-found'}
  }
  if (isListView(head)) return river({kind: head}, rest)
  if (head === 'feeds' && rest.length === 0) return {name: 'feeds'}
  if (isResourceSegment(head)) {
    const id = positiveID(rest[0])
    return id === undefined ? {name: 'not-found'} : river({kind: RESOURCE_SEGMENTS[head], id}, rest.slice(1))
  }
  if (head === 'search') {
    if (rest.length === 0) return {name: 'search', search: searchRoute(params)}
    const saved = positiveID(rest[1])
    if (rest.length === 2 && rest[0] === 'saved' && saved !== undefined) return {name: 'search', search: searchRoute(params), saved}
    return {name: 'not-found'}
  }
  if (head === 'ask' && rest.length === 0) return {name: 'ask'}
  if (head === 'rules') {
    if (rest.length === 0) return {name: 'rules'}
    if (rest.length === 1 && rest[0] === 'new') return {name: 'rules', rule: 'new'}
    const rule = positiveID(rest[0])
    return rest.length === 1 && rule !== undefined ? {name: 'rules', rule} : {name: 'not-found'}
  }
  if (head === 'activity' && rest.length === 0) return {name: 'activity', filters: activityFilters(params)}
  if (head === 'notifications') {
    if (rest.length === 0) return {name: 'notifications'}
    const id = positiveID(rest[0])
    return rest.length === 1 && id !== undefined ? {name: 'notifications', id} : {name: 'not-found'}
  }
  if (head === 'settings' && rest.length === 0) return {name: 'settings', device: false}
  if (head === 'device' && rest.length === 0) return {name: 'settings', device: true}
  return {name: 'not-found'}
}

export function frontHref(period: FrontPeriod, entry?: number): string {
  const query = period === 'weekly' ? '?period=weekly' : ''
  return entry === undefined ? `/front${query}` : `/front/articles/${entry}${query}`
}

// A destination is either a river view or the front page. Keeping both here
// means opening an article never has to know which screen asked for it.
export function targetHref(target: ViewRef | FrontTarget, entry?: number): string {
  if (target.kind === 'front') return frontHref(target.period, entry)
  return viewHref(target, entry)
}

export function viewHref(view: ViewRef, entry?: number): string {
  // Today and Edition remain valid targets for old saved links and for
  // callers that only know the old destination names.
  if (view.kind === 'today') return frontHref('daily', entry)
  if (view.kind === 'edition') return frontHref('weekly', entry)
  if (view.kind === 'all') return entry === undefined ? '/articles' : `/articles/${entry}`
  const base = view.kind === 'feed' || view.kind === 'folder' || view.kind === 'label'
    ? `/${SEGMENT_FOR_KIND[view.kind]}/${view.id}`
    : `/${view.kind}`
  return entry === undefined ? base : `${base}/articles/${entry}`
}

export function searchHref(search: SearchRoute, saved?: number): string {
  const params = new URLSearchParams()
  if (search.q) params.set('q', search.q)
  if (search.mode === 'hybrid') params.set('mode', 'hybrid')
  if (search.feed) params.set('feed', String(search.feed))
  if (search.folder) params.set('folder', String(search.folder))
  if (search.limit) params.set('limit', String(search.limit))
  const query = params.toString()
  return `${saved === undefined ? '/search' : `/search/saved/${saved}`}${query ? `?${query}` : ''}`
}

export function activityHref(filters: ActivityFilters): string {
  const params = new URLSearchParams()
  if (filters.agent) params.set('agent', filters.agent)
  if (filters.action) params.set('action', filters.action)
  if (filters.since) params.set('since', filters.since)
  const query = params.toString()
  return `/activity${query ? `?${query}` : ''}`
}

export function routeHref(route: Route): string {
  switch (route.name) {
  case 'river': return viewHref(route.view, route.entry)
  case 'front': return frontHref(route.period, route.entry)
  case 'search': return searchHref(route.search, route.saved)
  case 'ask': return '/ask'
  case 'rules': return route.rule === undefined ? '/rules' : `/rules/${route.rule}`
  case 'activity': return activityHref(route.filters)
  case 'notifications': return route.id === undefined ? '/notifications' : `/notifications/${route.id}`
  case 'feeds': return '/feeds'
  case 'settings': return route.device ? '/device' : '/settings'
  case 'not-found': return '/'
  }
}

export function sameView(a: ViewRef, b: ViewRef): boolean {
  return a.kind === b.kind && (a.id ?? 0) === (b.id ?? 0)
}

// The sidebar section a route belongs to, so its link can carry
// aria-current="page" while a sub-route such as an open article is showing.
export function routeSection(route: Route): string {
  switch (route.name) {
  case 'river': return `river:${route.view.kind}:${route.view.id ?? ''}`
  case 'front': return 'river:front'
  case 'search': return route.saved === undefined ? 'search' : `saved:${route.saved}`
  default: return route.name
  }
}
