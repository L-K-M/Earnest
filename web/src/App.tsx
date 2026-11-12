import {Component, lazy, Suspense, useCallback, useEffect, useId, useRef, useState, useSyncExternalStore, type ComponentType, type CSSProperties, type FormEvent, type MouseEvent as ReactMouseEvent, type ReactNode} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from './api.generated'
import {request, RequestError, setCSRF} from './request'
import {readUnreadCounts} from './counts'
import {River, type SessionSummary} from './River'
import {FrontPage} from './FrontPage'
import {Reader} from './ArticleReader'
import {ArticleFrame} from './ArticleFrame'
import {defaultSettings, type ReadingSettings} from './Settings'
import {ActivityScreen, Notifications} from './agents/Surfaces'
import {useReaderSync} from './syncBridge'
import {shortcutsAllowed, viewKey, type Entry, type EntryState, type RangeScope, type ReaderActions, type Register, type Value, type View} from './reader'
import {Search, useSavedSearches} from './search/Search'
import {markRestCommand} from './markRest'
import {ContextMenu, menuRequest, SEPARATOR, tidy, useContextMenuKey, type MenuItem, type MenuRequest} from './ContextMenu'
import {listMenu, markUpToHere, newestListed} from './menus'
import type {FeedPrefill} from './FeedSettings'
import {createSubfolder, moveFolder, newOp, patchSubscription, refreshSubscription, renameFolder} from './feeds'
import type {SidebarItem} from './folderMoves'
import {Palette, type Command, type PaletteHandle} from './palette/Palette'
import {OfflineStatus, clearOffline, useOnline} from './offline/Offline'
import {Ask} from './pipeline/Pipeline'
import {Brand} from './Brand'
import {Icon} from './Icon'
import {Login} from './Login'
import {READING_VIEWS, Sidebar} from './Sidebar'
import {ResizeHandle} from './ResizeHandle'
import {usePaneWidths} from './paneWidths'
import {currentHref, initializeHistory, Link, navigate, openedFrom, resetLocation, returnTo, useLocation, useScrollRestoration, whenPresent} from './router'
import {activityHref, frontHref, parseRoute, routeSection, searchHref, targetHref, TODAY, viewHref, type FrontPeriod, type FrontTarget, type Route, type ViewRef} from './routes'
import {clearFrontCache, loadFrontPage} from './front-store'

type Identity = {user_id: number; principal_id: number}
type Toast = {id: number; message: string; undo?: () => Promise<void>}
type OpenMode = 'push' | 'replace'
type FolderDialogState = {kind: 'rename'; id: number; name: string} | {kind: 'subfolder'; id: number; parent: string}

// Sync acknowledges reading actions continuously; coalesce the resource
// refreshes they trigger so a burst of keystrokes costs one refetch burst.
const SYNC_REFRESH_DEBOUNCE_MS = 1500
// An undo toast stays long enough to read and reach; hovering or focusing it
// pauses the timer (WCAG 2.2.1).
const TOAST_MS = 8000
// A "g" waits this long for the second key of a go-to shortcut.
const SEQUENCE_MS = 1500
const GO_TO: Record<string, string> = {t:'/front', e:'/front?period=weekly', l:'/later', k:'/kept', d:'/done', f:'/feeds', s:'/settings', a:'/articles', A:'/activity', n:'/notifications', r:'/rules'}
const DAILY: FrontTarget = {kind: 'front', period: 'daily'}
const THEME_COLORS: Record<string, string> = {light:'#f8f6ef', dark:'#1d1d1b', black:'#000000', eink:'#ffffff'}

// Rarely visited screens load on demand. A failed chunk download is retried
// from the screen's error boundary rather than leaving a blank page.
function retryable<P extends object>(load: () => Promise<ComponentType<P>>) {
  const make = () => lazy(() => load().then(component => ({default: component})))
  let Lazy = make()
  function Screen(props: P) { return <Lazy {...props} /> }
  Screen.reset = () => { Lazy = make() }
  return Screen
}
const loaders = {
  feeds: () => import('./FeedSettings'),
  settings: () => import('./SettingsPage'),
  rules: () => import('./rules/Rules'),
  onboarding: () => import('./Onboarding'),
}
const FeedsScreen = retryable(() => loaders.feeds().then(module => module.Feeds))
const SettingsScreen = retryable(() => loaders.settings().then(module => module.SettingsPage))
const RulesScreen = retryable(() => loaders.rules().then(module => module.Rules))
const OnboardingScreen = retryable(() => loaders.onboarding().then(module => module.Onboarding))
const lazyScreens = [FeedsScreen, SettingsScreen, RulesScreen, OnboardingScreen]
function prefetch(screen: 'feeds' | 'settings' | 'rules') { void loaders[screen]().catch(() => undefined) }

class ScreenBoundary extends Component<{resetKey: string; children: ReactNode}, {error: Error | null}> {
  state: {error: Error | null} = {error: null}
  static getDerivedStateFromError(error: Error) { return {error} }
  componentDidUpdate(previous: {resetKey: string}) {
    if (previous.resetKey !== this.props.resetKey && this.state.error) this.setState({error: null})
  }
  render() {
    if (!this.state.error) return this.props.children
    return <div className="page"><div className="page-inner"><div className="problem" role="alert">
      <h2>This screen did not load</h2>
      <p>{this.state.error.message || 'Something went wrong.'} Check your connection, then try again. Your reading actions are kept.</p>
      <button type="button" className="primary" onClick={() => { for (const screen of lazyScreens) screen.reset(); this.setState({error: null}) }}>Try again</button>
    </div></div></div>
  }
}

function useMedia(query: string): boolean {
  const subscribe = useCallback((notify: () => void) => {
    const list = window.matchMedia?.(query)
    list?.addEventListener('change', notify)
    return () => list?.removeEventListener('change', notify)
  }, [query])
  return useSyncExternalStore(subscribe, () => window.matchMedia?.(query).matches ?? false, () => false)
}

function actionMessage(register: Register, value: Value): string {
  if (register === 'shelf') return value === 'done' ? 'Marked Done.' : value === 'later' ? 'Saved for Later.' : 'Removed from the shelf.'
  if (register === 'kept') return value === true ? 'Kept.' : 'No longer kept.'
  return value === 'unseen' ? 'Marked unread.' : 'Marked read.'
}

function inField(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="menu"], dialog[open]'))
}

export function App() {
  const [session, setSession] = useState(0)
  return <Session key={session} onLogout={() => setSession(value => value + 1)} />
}

function Session({onLogout}: {onLogout: () => void}) {
  const queryClient = useQueryClient()
  // A request started by a signed-out session can resolve after the next one
  // signs in; everything that navigates or opens an article checks this.
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])
  const go = useCallback((to: string, options?: {replace?: boolean; from?: string}) => alive.current && navigate(to, options), [])
  const [identity, setIdentity] = useState<Identity | null>(null)
  const sessionRevoked = useRef(false)
  const [checking, setChecking] = useState(() => Boolean(localStorage.getItem('earnest.csrf')))
  const [resettingGeneration, setResettingGeneration] = useState(false)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Entry | null>(null)
  const [onboarded, setOnboarded] = useState(false)
  const [onboardingStarted, setOnboardingStarted] = useState(false)
  const [summary, setSummary] = useState<SessionSummary>({seen:0,done:0,later:0,kept:0})
  const [sessionStart] = useState(() => new Date().toISOString())
  const [since] = useState(() => localStorage.getItem('earnest.lastVisit') ?? new Date(0).toISOString())
  const [toast, setToast] = useState<Toast | null>(null)
  const palette = useRef<PaletteHandle>(null)
  const drawer = useRef<HTMLDialogElement>(null)
  const page = useRef<HTMLDivElement>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const narrow = useMedia('(max-width: 1099px)')
  const compact = useMedia('(max-width: 759px)')
  const paneWidths = usePaneWidths(narrow)
  const location = useLocation()
  const route = parseRoute(location.pathname, location.search)
  const routeRef = useRef(route)
  useEffect(() => { routeRef.current = route })
  useEffect(() => { initializeHistory() }, [])
  useEffect(() => { const save = () => localStorage.setItem('earnest.lastVisit', new Date().toISOString()); window.addEventListener('pagehide', save); return () => window.removeEventListener('pagehide', save) }, [])

  const userID = identity?.user_id ?? 0
  // The list's copy renders the article at once; marking it as old makes the
  // reader refresh it in the background, which also stores it for offline.
  const remember = useCallback((entry: Entry) => { queryClient.setQueryData(['entry', userID, entry.id], entry, {updatedAt: 0}) }, [queryClient, userID])
  function cachedEntry(id: number | undefined): Entry | undefined {
    if (id === undefined) return undefined
    for (const [, data] of queryClient.getQueriesData<{pages?: {entries?: Entry[]}[]}>({queryKey: ['river']})) {
      const found = data?.pages?.flatMap(page => page.entries ?? []).find(entry => entry.id === id)
      if (found) return found
    }
    // The front page response carries complete entries too, so an article can
    // open offline from the page the reader was already looking at.
    for (const [, data] of queryClient.getQueriesData<{sections?: {stories?: {entry?: Entry; coverage?: {entry?: Entry}[]}[]}[]}>({queryKey: ['front']})) {
      const found = data?.sections?.flatMap(section => section.stories ?? []).flatMap(story => [story.entry, ...(story.coverage ?? []).map(item => item.entry)]).find(entry => entry?.id === id)
      if (found) return found
    }
    for (const period of ['daily', 'weekly'] as const) {
      const stored = loadFrontPage(userID, period)
      const found = stored?.page.sections.flatMap(section => section.stories).flatMap(story => [story.entry, ...story.coverage.map(item => item.entry)]).find(entry => entry.id === id)
      if (found) return found
    }
    return undefined
  }
  const online = useOnline()

  const notificationOpened = useRef(false)
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get('notification')
    if (!identity || !id || notificationOpened.current) return
    notificationOpened.current = true
    const url = new URL(window.location.href)
    url.searchParams.delete('notification')
    history.replaceState(history.state, '', url)
    const rest = `${url.search}${url.hash}`
    // If the reader navigates while these requests are in flight, the late
    // response must not replace the history entry they are now on.
    const landed = currentHref()
    let active = true
    if (!/^[1-9]\d*$/.test(id)) return
    void request<components['schemas']['Notification']>(`/v1/notifications/${id}`).then(async notification => {
      if (!active || currentHref() !== landed) return
      const entryID = notification.entry_ids?.[0]
      if (!entryID) { go(`/notifications/${id}${rest}`, {replace: true}); return }
      const entry = await request<Entry>(`/v1/entries/${entryID}`)
      if (!active || currentHref() !== landed) return
      remember(entry); setSelected(entry)
      go(`${frontHref('daily', entry.id)}${rest}`, {replace: true})
    }).catch(e => {if (active && currentHref() === landed) {setError(e instanceof Error ? e.message : 'Could not open notification.'); go(`/notifications${rest}`, {replace: true})}})
    return () => {active = false}
  }, [identity, go, remember])

  const signedIn = identity !== null
  useEffect(() => {
    if (!signedIn || !('serviceWorker' in navigator)) return
    const controlled = () => {
      // First installation may claim this tab after its initial reads finished.
      void request<Identity>('/v1/me').then(() => queryClient.invalidateQueries()).catch(() => undefined)
    }
    navigator.serviceWorker.addEventListener('controllerchange',controlled)
    return () => navigator.serviceWorker.removeEventListener('controllerchange',controlled)
  },[signedIn,queryClient])
  useEffect(() => {
    let active = true
    if (!localStorage.getItem('earnest.csrf')) return
    void request<Identity>('/v1/me').then(value => { if (active) setIdentity(value) }).catch(() => { /* Sign in again when the saved session expires. */ }).finally(() => { if (active) setChecking(false) })
    return () => { active = false }
  }, [])
  const subscriptions = useQuery({queryKey: ['subscriptions'], enabled: signedIn, queryFn: ({signal}) => request<components['schemas']['Subscription'][]>('/v1/subscriptions',{signal})})
  const folders = useQuery({queryKey: ['folders'], enabled: signedIn, queryFn: ({signal}) => request<components['schemas']['Folder'][]>('/v1/folders',{signal})})
  const labels = useQuery({queryKey: ['labels'], enabled: signedIn, queryFn: ({signal}) => request<components['schemas']['Label'][]>('/v1/labels',{signal})})
  const preferences = useQuery({queryKey: ['settings'], enabled: signedIn, queryFn: ({signal}) => request<ReadingSettings>('/v1/settings',{signal})})
  const settings = preferences.data ?? defaultSettings
  // Quick filtering belongs to this tab's session, not the account settings.
  // Until a choice is made, follow the saved default as it loads or changes.
  const [unreadOnlyOverride, setUnreadOnlyOverride] = useState<boolean>()
  const unreadOnly = unreadOnlyOverride ?? settings.show_only_unread ?? false
  const pipeline = useQuery({queryKey:['pipeline'],enabled:signedIn,queryFn:({signal}) => request<components['schemas']['PipelineStatus']>('/v1/pipeline',{signal}),staleTime:30_000})
  const savedSearches = useSavedSearches(signedIn)
  const unreadCounts = useQuery({queryKey:['unread-counts'],enabled:signedIn && settings.reading_mode === 'unread_counts',queryFn:({signal})=>readUnreadCounts(signal),staleTime:30_000})
  const shortcuts = settings.single_key_shortcuts && preferences.isSuccess
  const subs = (subscriptions.data ?? []).filter(sub => !sub.deleted)

  // Resolve the route's view against the loaded resources. A feed, folder or
  // label that no longer exists is reported, not shown as an empty list.
  const riverRoute = route.name === 'river' ? route : null
  const frontRoute = route.name === 'front' ? route : null
  const viewRef: ViewRef = riverRoute?.view ?? TODAY
  const resolved = (() => {
    const standard = READING_VIEWS.find(item => item.kind === viewRef.kind)
    if (standard) return {view: {kind: viewRef.kind, title: standard.title} as View, missing: false}
    if (viewRef.kind === 'feed') {
      const sub = subs.find(item => item.id === viewRef.id)
      return {view: {kind: 'feed', id: viewRef.id, title: sub ? sub.title || sub.url : 'Feed'} as View, missing: subscriptions.isSuccess && !sub}
    }
    const list = viewRef.kind === 'folder' ? folders : labels
    const item = list.data?.find(value => value.id === viewRef.id && !value.deleted)
    return {view: {kind: viewRef.kind, id: viewRef.id, title: item?.name ?? (viewRef.kind === 'folder' ? 'Folder' : 'Label')} as View, missing: list.isSuccess && !item}
  })()
  const view = resolved.view
  const openID = route.name === 'river' || route.name === 'front' ? route.entry : undefined
  // Choosing a parent list from the sidebar/palette is a new visit even when
  // it is already behind the reader. Opening articles and Back/close are not.
  const [listVisit, setListVisit] = useState({locationKey: location.key, number: 0})
  if (listVisit.locationKey !== location.key) {
    setListVisit({locationKey: location.key, number: listVisit.number + Number(unreadOnly && riverRoute !== null && openID === undefined && location.action === 'push')})
  }
  // An article link opened offline cannot fetch the article, but the list it
  // belongs to usually comes from the offline cache a moment later; the retry
  // finds it there.
  const openQuery = useQuery({
    queryKey: ['entry', userID, openID], enabled: signedIn && openID !== undefined, staleTime: 5 * 60_000,
    queryFn: async ({signal}) => {
      try {
        return await request<Entry>(`/v1/entries/${openID}`, {signal})
      } catch (failure) {
        const listed = cachedEntry(openID)
        if (listed && !(failure instanceof RequestError)) return listed
        throw failure
      }
    },
    retry: (count, failure) => !(failure instanceof RequestError && failure.status === 404) && count < 2,
  })
  const openEntry = openID !== undefined ? openQuery.data : undefined
  const current = openEntry ?? selected

  const refreshResources = useCallback((reason?: 'generation') => {
    const invalidateResources = () => {
      for (const resource of ['settings', 'subscriptions', 'folders', 'labels', 'unread-counts', 'saved-searches']) void queryClient.invalidateQueries({queryKey: [resource]})
    }
    if (reason === 'generation') {
      setResettingGeneration(true);setSelected(null)
      const open = routeRef.current
      // Back to the list the article was opened from, as closing it would.
      if (open.name === 'river' && open.entry !== undefined) returnTo(viewHref(open.view))
      // Old revision numbers must not re-enter sync from an offline response.
      clearFrontCache()
      void clearOffline().then(async () => {
        await request<Identity>('/v1/me')
        await queryClient.resetQueries({queryKey:['position']})
        await queryClient.resetQueries({queryKey:['river']})
        invalidateResources()
        setError('')
        setResettingGeneration(false)
      }).catch(error => setError(error instanceof Error ? error.message : 'Could not refresh offline reading copies.'))
      return
    }
    invalidateResources()
  }, [queryClient])
  const syncRefresh = useRef<number | undefined>(undefined)
  const refreshAfterSync = useCallback((reason?: 'generation') => {
    // A generation rotation must clear caches at once and cancel any pending
    // window; ordinary cursor changes share one refresh per window. The timer
    // is scheduled only while idle, so a continuous stream of reading
    // acknowledgements cannot starve the refresh.
    if (reason === 'generation') {
      window.clearTimeout(syncRefresh.current)
      syncRefresh.current = undefined
      refreshResources(reason)
      return
    }
    if (syncRefresh.current === undefined) {
      syncRefresh.current = window.setTimeout(() => {
        syncRefresh.current = undefined
        refreshResources()
      }, SYNC_REFRESH_DEBOUNCE_MS)
    }
  }, [refreshResources])
  useEffect(() => () => window.clearTimeout(syncRefresh.current), [])
  const sync = useReaderSync(identity, refreshAfterSync)
  const retrySync = sync.retry
  useEffect(() => {
    const online = () => retrySync()
    window.addEventListener('online',online)
    return () => window.removeEventListener('online',online)
  },[retrySync])

  const notify = useCallback((message: string, undo?: () => Promise<void>) => { if (alive.current) setToast({id: Date.now(), message, undo}) }, [])
  // Stable, so the toast's timer is not restarted by unrelated renders.
  const dismissToast = useCallback(() => setToast(null), [])
  function count(register: Register, value: Value, delta: number) {
    setSummary(old => ({...old, seen: old.seen + delta * Number(register === 'seen' && value === 'seen'), done: old.done + delta * Number(register === 'shelf' && value === 'done'), later: old.later + delta * Number(register === 'shelf' && value === 'later'), kept: old.kept + delta * Number(register === 'kept' && value === true)}))
  }
  // Deliberate actions apply at once and offer an undo that restores exactly
  // the registers they changed. Marking unread also clears Done on the
  // server, so its undo restores both.
  function undoFor(entry: Entry, register: Register, value: Value, before: EntryState) {
    return async () => {
      setToast(null)
      try {
        if (register === 'seen') {
          await sync.act(entry, 'seen', before.seen.value)
          if (before.shelf.value === 'done') await sync.act(entry, 'shelf', 'done')
        } else {
          await sync.act(entry, register, before[register].value)
        }
      } catch (failure) {
        // Say so instead of dropping the undo silently; the action stands.
        notify(`Could not undo: ${failure instanceof Error ? failure.message : 'the change was not saved.'}`)
        return
      }
      count(register, value, -1)
      notify('Undone.')
    }
  }
  const actions: ReaderActions = {
    deleted: sync.deleted,
    state: entry => sync.state(entry),
    act: async (entry, register, value, kind = 'explicit') => {
      const before = sync.state(entry)
      await sync.act(entry, register, value, kind)
      count(register, value, 1)
      if (kind === 'explicit') notify(actionMessage(register, value), undoFor(entry, register, value, before))
    },
  }

  const openArticle = useCallback((entry: Entry, options: {view?: ViewRef | FrontTarget; mode?: OpenMode} = {}) => {
    if (!alive.current) return
    remember(entry)
    setSelected(entry)
    const replace = options.mode === 'replace'
    const now: Route = routeRef.current
    const target = options.view ?? (now.name === 'river' ? now.view : now.name === 'front' ? {kind: 'front', period: now.period} : DAILY)
    go(targetHref(target, entry.id), {replace, from: replace ? openedFrom() : currentHref()})
  }, [go, remember])
  // From a list: the first article pushes an entry so Back returns to the
  // list; moving between articles replaces it so Back still does.
  const openFromList = useCallback((entry: Entry, mode?: OpenMode) => openArticle(entry, {mode: mode ?? (routeRef.current.name === 'river' && routeRef.current.entry !== undefined ? 'replace' : 'push')}), [openArticle])
  // Opening a front-page story is an explicit read: the representative is
  // marked seen passively once, exactly like opening a river row. Rendering,
  // switching windows and expanding coverage never write.
  const frontScroll = useRef(0)
  const openFront = (entry: Entry, mode?: OpenMode) => {
    if (routeRef.current.name !== 'front' || routeRef.current.entry === undefined) {
      frontScroll.current = document.querySelector('.front')?.scrollTop ?? 0
    }
    const state = actions.state(entry)
    if (!actions.deleted?.(entry) && state.seen.value === 'unseen' && state.shelf.value !== 'done') {
      void actions.act(entry, 'seen', 'seen', 'passive').catch(error => setError(error instanceof Error ? error.message : 'Could not save reading progress.'))
    }
    const period: FrontPeriod = routeRef.current.name === 'front' ? routeRef.current.period : 'daily'
    openArticle(entry, {view: {kind: 'front', period}, mode: mode ?? (routeRef.current.name === 'front' && routeRef.current.entry !== undefined ? 'replace' : 'push')})
  }
  const openElsewhere = useCallback((entry: Entry) => {
    const now = routeRef.current
    const target = now.name === 'river' ? now.view : now.name === 'front' ? {kind: 'front' as const, period: now.period} : DAILY
    return openArticle(entry, {view: target, mode: 'push'})
  }, [openArticle])
  const listHref = frontRoute ? frontHref(frontRoute.period) : viewHref(viewRef)
  const closeReader = useCallback(() => {
    if (!alive.current) return
    if (openedFrom() !== undefined) history.back()
    else go(listHref, {replace: true})
  }, [go, listHref])

  // When the article closes, put focus back on its row, or on the list.
  const previousOpen = useRef<number | undefined>(undefined)
  useEffect(() => {
    const was = previousOpen.current
    previousOpen.current = openID
    if (was === undefined || openID !== undefined) return
    return whenPresent(() => document.querySelector<HTMLElement>(`.river [data-entry="${was}"] .entry`) ?? document.querySelector<HTMLElement>(`.front [data-entry="${was}"] .front-headline a`) ?? document.querySelector<HTMLElement>('.river h2') ?? document.querySelector<HTMLElement>('.front .front-title'), target => {
      const front = document.querySelector('.front')
      if (front) front.scrollTop = frontScroll.current
      target.focus({preventScroll: front !== null})
    }, 10)
  }, [openID])

  // Screens other than the river put focus on their heading when the route
  // changes, unless the screen already moved it somewhere useful.
  const firstRunShown = route.name === 'river' && route.entry === undefined && !onboarded && subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0
  const section = firstRunShown ? 'welcome' : routeSection(route)
  useEffect(() => {
    if ((route.name === 'river' || route.name === 'front') && section !== 'welcome') return
    return whenPresent(() => page.current?.querySelector<HTMLElement>('h2'), heading => {
      if (page.current?.contains(document.activeElement) && document.activeElement !== page.current) return
      if (!heading.hasAttribute('tabindex')) heading.setAttribute('tabindex', '-1')
      heading.focus({preventScroll: true})
    })
    // The section, not every query-string change, decides when focus moves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [section, identity])
  useScrollRestoration(page, location, section)
  // A list that no longer exists has no River to take focus; its explanation does.
  useEffect(() => {
    if (!resolved.missing) return
    return whenPresent(() => document.querySelector<HTMLElement>('.list-pane .empty-state h2'), heading => heading.focus())
  }, [resolved.missing, section])

  useEffect(() => { if (!narrow && drawer.current?.open) drawer.current.close() }, [narrow])
  useEffect(() => { if (drawer.current?.open) drawer.current.close() }, [location.href])

  const pageTitle = (() => {
    switch (route.name) {
    case 'river': return openEntry ? `${openEntry.title || 'Untitled'} · ${view.title}` : openID !== undefined && openQuery.isError ? `Article unavailable · ${view.title}` : view.title
    case 'front': return openEntry ? `${openEntry.title || 'Untitled'} · Front page` : openID !== undefined && openQuery.isError ? 'Article unavailable · Front page' : 'Front page'
    case 'search': return route.saved !== undefined ? savedSearches.data?.find(saved => saved.id === route.saved)?.name ?? 'Saved search' : route.search.q ? `${route.search.q} · Search` : 'Search'
    case 'ask': return 'Ask my feeds'
    case 'rules': return route.rule === 'new' ? 'New rule' : 'Rules'
    case 'activity': return 'Activity'
    case 'notifications': return route.id === undefined ? 'Notifications' : `Notification ${route.id}`
    case 'feeds': return 'Manage feeds'
    case 'settings': return route.device ? 'Approve a device' : 'Settings'
    case 'not-found': return 'Not found'
    }
  })()
  useEffect(() => { document.title = identity ? `${pageTitle} · Earnest` : 'Sign in · Earnest' }, [pageTitle, identity])
  useEffect(() => {
    const root = document.documentElement
    if (settings.theme === 'system') delete root.dataset.theme
    else root.dataset.theme = settings.theme
    for (const meta of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
      if (!meta.dataset.original) meta.dataset.original = meta.content
      meta.content = settings.theme === 'system' ? meta.dataset.original : THEME_COLORS[settings.theme] ?? meta.dataset.original
    }
  }, [settings.theme])

  const closeReaderRef = useRef(closeReader)
  useEffect(() => { closeReaderRef.current = closeReader }, [closeReader])
  const toastRef = useRef(toast)
  useEffect(() => { toastRef.current = toast }, [toast])
  useEffect(() => {
    let sequence = 0
    // Capture phase: the second key of "g e" must not also reach the list's
    // single-key handler as "e" (Done).
    function keyboard(event: KeyboardEvent) {
      if (!alive.current || event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.isComposing) return
      const reading = (routeRef.current.name === 'river' || routeRef.current.name === 'front') && routeRef.current.entry !== undefined
      if (event.key === 'Escape') {
        if (reading && !inField(event.target) && !document.querySelector(':popover-open')) { event.preventDefault(); closeReaderRef.current() }
        return
      }
      if (!shortcutsAllowed(event.target, shortcuts)) return
      if (sequence && performance.now() - sequence < SEQUENCE_MS) {
        sequence = 0
        const to = GO_TO[event.key]
        if (to) { event.preventDefault(); go(to) }
        return
      }
      sequence = 0
      if (event.key === 'g') { sequence = performance.now(); return }
      if (event.key === 'u' && reading) { event.preventDefault(); closeReaderRef.current(); return }
      if (event.key === 'z' && toastRef.current?.undo) { event.preventDefault(); void toastRef.current.undo() }
    }
    window.addEventListener('keydown', keyboard, true)
    return () => window.removeEventListener('keydown', keyboard, true)
  }, [shortcuts, go])

  const [newestLoaded, setNewestLoaded] = useState({key: '', seq: 0})
  const [menu, setMenu] = useState<MenuRequest | null>(null)
  const [folderDialog, setFolderDialog] = useState<FolderDialogState | null>(null)
  const [subscribePrefill, setSubscribePrefill] = useState<FeedPrefill | null>(null)
  const consumeSubscribePrefill = useCallback(() => setSubscribePrefill(null), [])
  const closeMenu = useCallback(() => setMenu(null), [])
  useContextMenuKey()
  function showMenu(event: ReactMouseEvent<HTMLElement>, label: string, items: MenuItem[]) {
    // Shift with a right click keeps the browser's own menu.
    if (event.shiftKey || items.length === 0) return
    event.preventDefault()
    setMenu(menuRequest(event, label, items))
  }
  const markRange = (scope: RangeScope, throughSeq: number) => sync.actRange(scope, throughSeq)
  // Bulk marks run through the newest article this browser knows of: what
  // sync has delivered, or what any list loaded if sync has not caught up.
  const newestKnown = () => Math.max(sync.newestSeq(), newestListed(queryClient.getQueriesData({queryKey: ['river']}).map(([, data]) => data)))
  const confirmAction = (message: string) => window.confirm(message)
  function showListMenu(event: ReactMouseEvent<HTMLElement>, target: View) {
    showMenu(event, `${target.title} actions`, listMenu(target, subs, {
      newest: newestKnown, markRange, confirm: confirmAction, notify,
      go: to => {go(to)}, openTab: href => {window.open(href, '_blank', 'noopener')},
      refreshFeed: async id => {await refreshSubscription(id)},
      // The narrow layout's sidebar drawer is modal; close it so a dialog
      // rendered at the app root is not inert, and so the Feeds screen a
      // prefill navigates to is reachable.
      renameFolder: folder => { drawer.current?.close(); setFolderDialog({kind: 'rename', id: folder.id, name: folder.title}) },
      newSubfolder: parent => { drawer.current?.close(); setFolderDialog({kind: 'subfolder', id: parent.id, parent: parent.title}) },
      addFeed: folder => {
        drawer.current?.close()
        setSubscribePrefill(previous => ({folderID: folder.id, token: (previous?.token ?? 0) + 1}))
        go('/feeds#subscribe')
      },
    }))
  }
  function showArticleMenu(event: ReactMouseEvent<HTMLElement>, entry: Entry, items: MenuItem[]) {
    showMenu(event, `${entry.title || 'Untitled'} actions`, tidy([
      ...items,
      SEPARATOR,
      markUpToHere(entry, view, subs, {markRange, confirm: confirmAction, notify}),
      {id: 'copy-original', label: 'Copy link to the original', icon: 'link', run: async () => {
        if (!navigator.clipboard) throw new Error('Copying needs Earnest on HTTPS or localhost.')
        await navigator.clipboard.writeText(entry.url)
        notify('Link to the original copied.')
      }},
    ]))
  }
  const articleCommand = (id: string, title: string, key: string, run: (entry: Entry) => Promise<void> | void): Command => ({id, title, key, group:'Article', disabled:!current, run:async () => {if (current) await run(current)}})
  const commands: Command[] = [
    ...READING_VIEWS.map(item => ({id:`view-${item.id}`, title:item.title, group:'Go to', key:item.shortcut, run:() => {go(item.to)}})),
    {id:'view-front-week', title:'This week', group:'Go to', key:'g e', run:() => {go('/front?period=weekly')}},
    ...subs.map(sub => ({id:`feed-${sub.id}`, title:`Feed: ${sub.title || sub.url}`, group:'Feed', run:() => {go(viewHref({kind:'feed', id:sub.id}))}})),
    ...(folders.data ?? []).filter(folder => !folder.deleted).map(folder => ({id:`folder-${folder.id}`, title:`Folder: ${folder.name}`, group:'Folder', run:() => {go(viewHref({kind:'folder', id:folder.id}))}})),
    ...(labels.data ?? []).filter(label => !label.deleted).map(label => ({id:`label-${label.id}`, title:`Label: ${label.name}`, group:'Label', run:() => {go(viewHref({kind:'label', id:label.id}))}})),
    ...(savedSearches.data ?? []).map(saved => ({id:`saved-${saved.id}`, title:`Search: ${saved.name}`, group:'Saved search', run:() => {go(`/search/saved/${saved.id}`)}})),
    articleCommand('done', 'Mark Done', 'e', entry => actions.act(entry,'shelf','done')),
    articleCommand('later', 'Toggle Later', 's', entry => actions.act(entry,'shelf',actions.state(entry).shelf.value === 'later' ? 'none' : 'later')),
    articleCommand('keep', 'Toggle Keep', 'f', entry => actions.act(entry,'kept',actions.state(entry).kept.value !== true)),
    articleCommand('unread', 'Mark unread', 'm', entry => actions.act(entry,'seen','unseen')),
    articleCommand('original', 'Open original', 'v', entry => {window.open(entry.url, '_blank', 'noopener,noreferrer')}),
    articleCommand('copy-link', 'Copy Earnest link to the article', '', async entry => {
      // The clipboard API exists only in secure contexts (HTTPS or localhost).
      if (!navigator.clipboard) throw new Error('Copying needs Earnest on HTTPS or localhost. Copy the address bar instead.')
      const now = routeRef.current
      const target = now.name === 'river' ? now.view : now.name === 'front' ? {kind: 'front' as const, period: now.period} : DAILY
      await navigator.clipboard.writeText(new URL(targetHref(target, entry.id), window.location.origin).href)
      notify('Link copied.')
    }),
    {id:'undo', title:'Undo the last action', key:'z', group:'Article', disabled:!toast?.undo, run:async () => {await toast?.undo?.()}},
    ...(route.name === 'river' ? [{...markRestCommand(view, subs, newestLoaded.key === viewKey(view) ? newestLoaded.seq : 0, (scope, throughSeq) => sync.actRange(scope, throughSeq)), group:'Article' as const, key:undefined}] : []),
    {id:'subscribe', title:'Subscribe to a feed', group:'Feeds', run:() => {go('/feeds#subscribe')}},
    {id:'search', title:'Search', key:'/', group:'Go to', run:() => {go('/search')}},
    ...(pipeline.data?.ask === 'enabled' ? [{id:'ask', title:'Ask my feeds', group:'Go to', run:() => {go('/ask')}}] : []),
    {id:'rules', title:'Rules', key:'g r', group:'Go to', run:() => {go('/rules')}},
    {id:'activity', title:'Activity', key:'g A', group:'Go to', run:() => {go('/activity')}},
    {id:'notifications', title:'Notifications', key:'g n', group:'Go to', run:() => {go('/notifications')}},
    {id:'manage-feeds', title:'Manage feeds', key:'g f', group:'Go to', run:() => {go('/feeds')}},
    {id:'settings', title:'Settings', key:'g s', group:'Go to', run:() => {go('/settings')}},
    {id:'shortcuts', title:'Keyboard shortcuts', key:'?', group:'Help', run:() => {window.setTimeout(() => palette.current?.openHelp(), 0)}},
    {id:'sign-out', title:'Sign out', group:'Account', run:() => {void logout()}},
  ]
  function savedSettings(value: ReadingSettings) { queryClient.setQueryData(['settings'], value) }
  async function login(username: string, password: string): Promise<string | null> {
    try {
      const result = await request<{csrf_token: string}>('/v1/session/login', {method:'POST', body:JSON.stringify({username, password})})
      setCSRF(result.csrf_token)
      setIdentity(await request<Identity>('/v1/me')); setError('')
      return null
    } catch (error) {
      // The server's problem titles are terse; say what to do instead.
      if (error instanceof RequestError && error.status === 401) return 'That username and password do not match. Check both and try again.'
      if (error instanceof RequestError && error.status === 429) return 'Too many sign-in attempts. Wait a minute, then try again.'
      if (!(error instanceof RequestError)) return 'Earnest could not be reached. Check your connection and try again.'
      return error.message || 'Sign in failed.'
    }
  }
  async function logout() {
    try {
      // Cache clearing can need a retry after the server has revoked the session.
      if (!sessionRevoked.current) {
        // A 401 means the server already ended this session, so signing out
        // locally is all that is left. Any other failure keeps the session
        // cookie alive, so it is reported rather than hidden behind a sign-in
        // screen that a reload would undo.
        try {
          await request('/v1/session/logout', {method:'POST'})
        } catch (error) {
          if (!(error instanceof RequestError && error.status === 401)) throw error
        }
        sessionRevoked.current = true
      }
      await clearOffline()
      clearFrontCache()
      await queryClient.cancelQueries()
      queryClient.clear()
      localStorage.removeItem('earnest.csrf'); sessionStorage.removeItem('earnest.csrf')
      localStorage.removeItem('earnest.lastVisit')
      // The next account starts on the front page, not on this account's article.
      resetLocation('/')
      // Remount every session-owned hook, including callbacks still awaiting I/O.
      onLogout()
    }
    catch (error) { setError(error instanceof Error ? error.message : 'Could not sign out.') }
  }

  if (checking) return <main className="login" aria-busy="true"><Brand /><p role="status" className="muted center">Checking your session…</p></main>
  if (resettingGeneration) return <main className="login"><Brand /><p role="status" className="muted center">Refreshing reading copies…</p>{error && <p role="alert">{error} <button onClick={() => refreshResources('generation')}>Retry</button></p>}</main>
  if (!identity) return <Login onSignIn={login} />

  const firstRun = (route.name === 'river' || route.name === 'front') && openID === undefined && !onboarded && (onboardingStarted || (subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0))
  const reading = openID !== undefined
  // Offline, failed requests are expected; the offline notice says so once
  // instead of a banner per failed request.
  const syncTone = sync.error && online ? 'problem' as const : !online || /waiting|Restoring|Opening/.test(sync.status) ? 'waiting' as const : 'quiet' as const
  const syncText = !online ? `Offline. ${sync.status.replace(/\.$/, '')}` : sync.error ? 'Sync needs attention' : sync.status.replace(/\.$/, '')
  async function moveSidebarItem(item: SidebarItem, parentID: number) {
    if (item.kind === 'feed') await patchSubscription(item.id, {folder_id: parentID, op_id: newOp()})
    else await moveFolder(item.id, parentID)
    await Promise.all(['subscriptions', 'folders', 'unread-counts', 'river'].map(resource => queryClient.invalidateQueries({queryKey: [resource]})))
  }
  const sidebar = <Sidebar route={route} subscriptions={subs} folders={folders.data ?? []} labels={labels.data ?? []} savedSearches={savedSearches.data ?? []} counts={settings.reading_mode === 'unread_counts' ? unreadCounts.data : undefined} askEnabled={pipeline.data?.ask === 'enabled'} sync={{text: syncText, tone: syncTone}} onNavigate={() => drawer.current?.close()} onPalette={() => {drawer.current?.close(); palette.current?.openPalette()}} onHelp={() => {drawer.current?.close(); palette.current?.openHelp()}} onSignOut={() => {void logout()}} prefetch={prefetch} onMenu={showListMenu} onMove={moveSidebarItem} />

  function readerPane(): ReactNode {
    if (!reading) return <ReaderPlaceholder shortcuts={shortcuts} />
    if (openEntry) return <Reader key={openEntry.id} onOpen={entry => openArticle(entry, {mode: 'push'})} entry={openEntry} subscription={subs.find(sub => sub.id === openEntry.subscription_id)} settings={settings} actions={actions} onSettings={savedSettings} onNotify={notify} renderBody={url => <ArticleFrame src={url} />} onClose={closeReader} />
    if (openQuery.isError) return <ReaderProblem error={openQuery.error} onBack={closeReader} onRetry={() => {void openQuery.refetch()}} />
    return <ReaderSkeleton />
  }

  function screen(): ReactNode {
    if (firstRun) return <div className="page" ref={page}><div className="page-inner onboarding" onChangeCapture={() => setOnboardingStarted(true)} onClickCapture={() => setOnboardingStarted(true)}><Suspense fallback={<PageSkeleton />}><OnboardingScreen onComplete={() => {setOnboarded(true);refreshResources();void queryClient.invalidateQueries({queryKey:['position']});go('/front', {replace: true})}} /></Suspense></div></div>
    switch (route.name) {
    case 'river': return <div className={`panes${reading ? ' reading' : ''}`}>
      <div id="article-list" className="list-pane" inert={compact && reading}>
        {resolved.missing
          ? <section className="river"><div className="empty-state"><h2 tabIndex={-1}>This {viewRef.kind} is gone</h2><p>It may have been deleted or unsubscribed on another device.</p><p><Link to="/front">Go to Front page</Link></p></div></section>
          : <River since={since} sessionStart={sessionStart} key={`${viewKey(view)}:${listVisit.number}`} onNewestLoaded={seq => setNewestLoaded({key: viewKey(view), seq})} view={view} subscriptions={subs} actions={actions} selected={current} onSelect={setSelected} onOpen={openFromList} onMenu={showArticleMenu} reading={reading} shortcuts={shortcuts} counts={settings.reading_mode === 'unread_counts'} unreadOnly={unreadOnly} onUnreadOnlyChange={setUnreadOnlyOverride} summary={summary} />}
      </div>
      {!compact && <ResizeHandle label="Article list width" controls="article-list" {...paneWidths.list} />}
      <div className="reader-pane">{readerPane()}</div>
    </div>
    case 'front': return <div className={`panes${reading ? ' reading' : ''}`}>
      <div id="article-list" className="list-pane" inert={compact && reading}>
        <FrontPage key={`${userID}:${route.period}`} period={route.period} userID={userID} actions={actions} selected={current} onSelect={setSelected} onOpen={openFront} since={since} sessionStart={sessionStart} />
      </div>
      {!compact && <ResizeHandle label="Article list width" controls="article-list" {...paneWidths.list} />}
      <div className="reader-pane">{readerPane()}</div>
    </div>
    case 'search': {
      const saved = route.saved === undefined ? undefined : savedSearches.data?.find(item => item.id === route.saved)
      return <div className="page" ref={page}><div className="page-inner">{route.saved !== undefined && !saved
        ? savedSearches.isPending ? <PageSkeleton />
          : savedSearches.isError ? <div className="problem" role="alert"><h2>Saved searches did not load</h2><p>{savedSearches.error.message} Check your connection and try again.</p><button type="button" className="primary" onClick={() => {void savedSearches.refetch()}}>Try again</button></div>
          : <div className="empty-state"><h2>This saved search no longer exists</h2><p><Link to="/search">Start a new search</Link></p></div>
        : <Search key={route.saved ?? 'search'} saved={saved} route={route.saved === undefined ? route.search : undefined} onRoute={(next, replace) => {go(searchHref(next), {replace})}} onDeleted={() => {notify('Saved search deleted.');go('/search', {replace: true})}} onOpen={openElsewhere} subscriptions={subs} folders={folders.data ?? []}/>}</div></div>
    }
    case 'ask': return <div className="page" ref={page}><div className="page-inner">{pipeline.data?.ask === 'enabled' ? <Ask onOpen={openElsewhere}/> : pipeline.isPending ? <PageSkeleton /> : pipeline.isError ? <div className="problem" role="alert"><h2>Ask my feeds did not load</h2><p>{pipeline.error.message} Check your connection and try again.</p><button type="button" className="primary" onClick={() => {void pipeline.refetch()}}>Try again</button></div> : <div className="empty-state"><h2>Ask my feeds is off</h2><p>Questions need the ask task, which an operator enables in the server configuration.</p><p><Link to="/search">Search your feeds instead</Link></p></div>}</div></div>
    case 'rules': return <div className="page" ref={page}><div className="page-inner"><Suspense fallback={<PageSkeleton />}><RulesScreen feeds={subs} folders={folders.data ?? []} labels={labels.data ?? []} editing={route.rule} onEdit={id => {go(`/rules/${id}`)}} onClose={() => { if (openedFrom() === '/rules') history.back(); else go('/rules', {replace: true}) }} /></Suspense></div></div>
    case 'activity': return <div className="page" ref={page}><div className="page-inner"><ActivityScreen filters={route.filters} onFilters={filters => {go(activityHref(filters), {replace: true})}} /></div></div>
    case 'notifications': return <div className="page" ref={page}><div className="page-inner"><Notifications selectedID={route.id} onSelect={id => {go(`/notifications/${id}`, {replace: route.id !== undefined})}} onOpen={openElsewhere} onOpenSettings={() => {go('/settings#push')}} /></div></div>
    case 'feeds': return <div className="page" ref={page}><div className="page-inner"><Suspense fallback={<PageSkeleton />}><div className="page-header"><h2>Manage feeds</h2></div><FeedsScreen onChanged={refreshResources} prefill={subscribePrefill} onPrefillApplied={consumeSubscribePrefill} /></Suspense></div></div>
    case 'settings': return <div className="page" ref={page}><div className="page-inner wide"><Suspense fallback={<PageSkeleton />}><SettingsScreen device={route.device} settings={settings} onSaved={savedSettings} /></Suspense></div></div>
    case 'not-found': return <div className="page" ref={page}><div className="page-inner"><div className="empty-state"><h2 tabIndex={-1}>There is nothing at this address</h2><p>The link may be mistyped, or the page moved.</p><p><Link to="/front">Go to Front page</Link></p></div></div></div>
    }
  }

  return <div className={`app${reading ? ' is-reading' : ''}`} style={{'--sidebar': `${paneWidths.navigation.value}px`, '--list-width': `${paneWidths.list.value}px`} as CSSProperties}>
    <a className="skip-link" href="#main" onClick={event => {event.preventDefault();document.getElementById('main')?.focus()}}>Skip to content</a>
    {narrow && <header className="topbar">
      <button type="button" className="icon-button" aria-haspopup="dialog" aria-expanded={drawerOpen} onClick={() => drawer.current?.showModal()}><Icon name="menu" /><span className="sr-only">Menu</span></button>
      <p className="topbar-title">{route.name === 'river' ? (reading ? view.title : '') : route.name === 'front' && reading && openEntry ? openEntry.title : pageTitle}</p>
      {syncTone !== 'quiet' && <span className={`sync-dot ${syncTone}`} title={syncText}><Icon name={syncTone === 'problem' ? 'warning' : 'sync'} size={16} /><span className="sr-only">{syncText}</span></span>}
      <Link to="/search" className="icon-button"><Icon name="search" /><span className="sr-only">Search</span></Link>
      <button type="button" className="icon-button" onClick={() => palette.current?.openPalette()}><Icon name="command" /><span className="sr-only">Command palette</span></button>
    </header>}
    {narrow
      ? <dialog ref={drawer} className="drawer" aria-label="Navigation" onClose={() => setDrawerOpen(false)} onToggle={event => setDrawerOpen(event.currentTarget.open)} onClick={event => {if (event.target === drawer.current) drawer.current.close()}}>{sidebar}</dialog>
      : <div id="navigation-pane" className="navigation-pane">{sidebar}<ResizeHandle label="Navigation width" controls="navigation-pane" {...paneWidths.navigation} /></div>}
    <div className="workspace">
      <div className="banners">
        {error && <p role="alert" className="banner problem">{error} <button type="button" className="quiet" onClick={() => setError('')}>Dismiss</button></p>}
        {(subscriptions.error || preferences.error) && <p role="alert" className="banner problem">{subscriptions.error?.message || preferences.error?.message} <button type="button" className="quiet" onClick={() => {void subscriptions.refetch();void preferences.refetch()}}>Try again</button></p>}
        {online && settings.reading_mode === 'unread_counts' && unreadCounts.error && <p role="alert" className="banner problem">Unread counts: {unreadCounts.error.message} <button type="button" className="quiet" onClick={() => {void unreadCounts.refetch()}}>Try again</button></p>}
        {online && sync.error && <p role="alert" className="banner problem">{sync.error} <button type="button" onClick={sync.retry}>Retry sync</button></p>}
        <OfflineStatus/>
      </div>
      <main id="main" tabIndex={-1} className={`main ${(route.name === 'river' || route.name === 'front') && !firstRun ? 'is-river' : 'is-page'}`}>
        <ScreenBoundary resetKey={section}>{screen()}</ScreenBoundary>
      </main>
    </div>
    <Palette ref={palette} commands={commands} shortcuts={shortcuts} triggers={false} />
    <ToastRegion toast={toast} onDismiss={dismissToast} />
    <ContextMenu request={menu} onClose={closeMenu} onError={message => notify(message)} />
    {folderDialog && <FolderDialog state={folderDialog} onClose={() => setFolderDialog(null)} onSaved={note => {refreshResources(); notify(note)}} />}
  </div>
}

// Rename and new-subfolder actions from a folder's context menu. The dialog
// is modal, labelled and keeps its own error so a failed save leaves the
// folder untouched and says why without dismissing the form.
function FolderDialog({state, onClose, onSaved}: {state: FolderDialogState; onClose: () => void; onSaved: (note: string) => void}) {
  const node = useRef<HTMLDialogElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const opener = useRef<HTMLElement | null>(null)
  const ids = useId()
  const [name, setName] = useState(state.kind === 'rename' ? state.name : '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const errorID = `${ids}-folder-error`

  useEffect(() => {
    const dialog = node.current
    if (!dialog) return
    opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!dialog.open) dialog.showModal()
    input.current?.focus()
    input.current?.select()
  }, [])

  function closed() {
    const target = opener.current
    if (target?.isConnected) target.focus({preventScroll: true})
    onClose()
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const next = name.trim()
    if (!next) {
      setError('Give the folder a name.')
      input.current?.focus()
      return
    }
    setBusy(true)
    setError('')
    try {
      if (state.kind === 'rename') await renameFolder(state.id, next)
      else await createSubfolder(next, state.id)
      node.current?.close()
      onSaved(state.kind === 'rename' ? 'Folder renamed.' : 'Subfolder created.')
    } catch (cause) {
      setError(cause instanceof Error && cause.message ? cause.message : 'The change did not go through.')
    } finally {
      setBusy(false)
    }
  }

  const rename = state.kind === 'rename'
  return <dialog ref={node} className="folder-dialog" aria-labelledby={`${ids}-folder-title`} onClose={closed} onClick={event => {if (event.target === node.current) node.current?.close()}}>
    <form onSubmit={submit}>
      <h3 id={`${ids}-folder-title`}>{rename ? 'Rename folder' : 'New subfolder'}</h3>
      {!rename && <p className="muted">Inside {state.parent}.</p>}
      <label htmlFor={`${ids}-folder-name`}>Folder name</label>
      <input id={`${ids}-folder-name`} ref={input} value={name} autoComplete="off" required
        onChange={event => { setName(event.target.value); if (error) setError('') }}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errorID : undefined} />
      {error && <p id={errorID} role="alert">{error}</p>}
      <p className="button-row">
        <button type="button" onClick={() => node.current?.close()}>Cancel</button>
        <button type="submit" className="primary" disabled={busy}>{rename ? 'Save' : 'Create'}</button>
      </p>
    </form>
  </dialog>
}

function ToastRegion({toast, onDismiss}: {toast: Toast | null; onDismiss: () => void}) {
  // Hover and keyboard focus each hold the notice open on their own, so
  // leaving with one does not restart the timer while the other remains.
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const paused = hovered || focused
  useEffect(() => {
    if (!toast || paused) return
    const timer = window.setTimeout(onDismiss, TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [toast, paused, onDismiss])
  // The region exists before any message so screen readers announce changes.
  return <div className="toast-region" role="status" aria-live="polite" aria-atomic="true">
    {toast && <div key={toast.id} className="toast" onPointerEnter={() => setHovered(true)} onPointerLeave={() => setHovered(false)} onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false) }}>
      <span>{toast.message}</span>
      {toast.undo && <button type="button" className="toast-action" aria-keyshortcuts="z" onClick={() => {void toast.undo?.()}}>Undo <kbd aria-hidden="true">z</kbd></button>}
      <button type="button" className="icon-button toast-close" onClick={onDismiss}><Icon name="close" size={16} /><span className="sr-only">Dismiss</span></button>
    </div>}
  </div>
}

function ReaderPlaceholder({shortcuts}: {shortcuts: boolean}) {
  return <section className="reader-placeholder" aria-label="Reader">
    <p className="placeholder-line">Choose an article. It has been waiting patiently.</p>
    {shortcuts && <p className="muted"><kbd>j</kbd> <kbd>k</kbd> to move, <kbd>Enter</kbd> to open, <kbd>?</kbd> for every shortcut.</p>}
  </section>
}

function ReaderSkeleton() {
  return <section className="reader" aria-busy="true" aria-label="Reader">
    <p role="status" className="sr-only">Opening article…</p>
    <div className="reader-toolbar skeleton-bar" aria-hidden="true" />
    <div className="article-header" aria-hidden="true"><div className="skeleton line short" /><div className="skeleton line title" /><div className="skeleton line title short" /><div className="skeleton line" /></div>
  </section>
}

function ReaderProblem({error, onBack, onRetry}: {error: Error; onBack: () => void; onRetry: () => void}) {
  const gone = error instanceof RequestError && error.status === 404
  // The article this route names did not open; take focus to why, next to
  // the way back, instead of leaving it on the page body.
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => { heading.current?.focus() }, [])
  return <section className="reader-placeholder" aria-label="Reader">
    <div role="alert" className="problem">
      <h2 ref={heading} tabIndex={-1}>{gone ? 'This article is no longer available' : 'The article did not open'}</h2>
      <p>{gone ? 'It may have expired from the river or been removed by its publisher.' : `${error.message} Check your connection and try again.`}</p>
      <p className="button-row">{!gone && <button type="button" className="primary" onClick={onRetry}>Try again</button>}<button type="button" onClick={onBack}>Back to the list</button></p>
    </div>
  </section>
}

function PageSkeleton() {
  return <div aria-busy="true"><p role="status" className="sr-only">Loading…</p><div className="skeleton line title short" aria-hidden="true" /><div className="skeleton block" aria-hidden="true" /><div className="skeleton block" aria-hidden="true" /></div>
}
