// The web client's URL contract. Every screen a reader might bookmark, share
// or return to with Back has a path here. This module is pure: it parses a
// location into a Route and formats a Route back into a path, nothing else.
//
//   /                          Today (alias of /today)
//   /today /edition /later /kept /done
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
export type SearchRoute = {q: string; mode?: 'hybrid'; feed?: number; folder?: number; limit?: number}
export type ActivityFilters = {agent?: string; action?: string; since?: string}

export type Route =
  | {name: 'river'; view: ViewRef; entry?: number}
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

export const TODAY: ViewRef = {kind: 'today'}

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

function activityFilters(params: URLSearchParams): ActivityFilters {
  const filters: ActivityFilters = {}
  const agent = params.get('agent')
  const action = params.get('action')
  const since = params.get('since')
  if (agent && positiveID(agent) !== undefined) filters.agent = agent
  if (action) filters.action = action
  if (since && /^\d{4}-\d{2}-\d{2}$/.test(since)) filters.since = since
  return filters
}

export function parseRoute(pathname: string, search = ''): Route {
  const parts = segments(pathname)
  if (parts === null) return {name: 'not-found'}
  const params = new URLSearchParams(search)
  const [head, ...rest] = parts
  if (head === undefined) return {name: 'river', view: TODAY}
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

export function viewHref(view: ViewRef, entry?: number): string {
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
  case 'search': return route.saved === undefined ? 'search' : `saved:${route.saved}`
  default: return route.name
  }
}
