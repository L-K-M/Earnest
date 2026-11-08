import {Component, lazy, Suspense, useCallback, useEffect, useRef, useState, useSyncExternalStore, type ComponentType, type ReactNode} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from './api.generated'
import {request, RequestError, setCSRF} from './request'
import {readUnreadCounts} from './counts'
import {River, type SessionSummary} from './River'
import {Reader} from './ArticleReader'
import {defaultSettings, type ReadingSettings} from './Settings'
import {ActivityScreen, Notifications} from './agents/Surfaces'
import {useReaderSync} from './syncBridge'
import {shortcutsAllowed, viewKey, type Entry, type EntryState, type ReaderActions, type Register, type Value, type View} from './reader'
import {Search, useSavedSearches} from './search/Search'
import {markRestCommand} from './markRest'
import {Palette, type Command, type PaletteHandle} from './palette/Palette'
import {OfflineStatus, clearOffline, useOnline} from './offline/Offline'
import {Ask} from './pipeline/Pipeline'
import {Brand} from './Brand'
import {Icon} from './Icon'
import {Login} from './Login'
import {READING_VIEWS, Sidebar} from './Sidebar'
import {currentHref, initializeHistory, Link, navigate, openedFrom, resetLocation, useLocation, useScrollRestoration, whenPresent} from './router'
import {activityHref, parseRoute, routeSection, searchHref, TODAY, viewHref, type Route, type ViewRef} from './routes'

type Identity = {user_id: number; principal_id: number}
type Toast = {id: number; message: string; undo?: () => Promise<void>}
type OpenMode = 'push' | 'replace'

// Sync acknowledges reading actions continuously; coalesce the resource
// refreshes they trigger so a burst of keystrokes costs one refetch burst.
const SYNC_REFRESH_DEBOUNCE_MS = 1500
// An undo toast stays long enough to read and reach; hovering or focusing it
// pauses the timer (WCAG 2.2.1).
const TOAST_MS = 8000
// A "g" waits this long for the second key of a go-to shortcut.
const SEQUENCE_MS = 1500
const GO_TO: Record<string, string> = {t:'/today', e:'/edition', l:'/later', k:'/kept', d:'/done', f:'/feeds', s:'/settings', a:'/activity', n:'/notifications', r:'/rules'}
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
  return target instanceof Element && Boolean(target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), dialog[open]'))
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
  const scrollMemory = useRef(new Map<string, number>())
  const [drawerOpen, setDrawerOpen] = useState(false)
  const narrow = useMedia('(max-width: 1099px)')
  const compact = useMedia('(max-width: 759px)')
  const location = useLocation()
  const route = parseRoute(location.pathname, location.search)
  const routeRef = useRef(route)
  useEffect(() => { routeRef.current = route })
  useEffect(() => { initializeHistory() }, [])
  useEffect(() => { const save = () => localStorage.setItem('earnest.lastVisit', new Date().toISOString()); window.addEventListener('pagehide', save); return () => window.removeEventListener('pagehide', save) }, [])

  const userID = identity?.user_id ?? 0
  const remember = useCallback((entry: Entry) => { queryClient.setQueryData(['entry', userID, entry.id], entry) }, [queryClient, userID])
  function cachedEntry(id: number | undefined): Entry | undefined {
    if (id === undefined) return undefined
    for (const [, data] of queryClient.getQueriesData<{pages?: {entries?: Entry[]}[]}>({queryKey: ['river']})) {
      const found = data?.pages?.flatMap(page => page.entries ?? []).find(entry => entry.id === id)
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
    let active = true
    if (!/^[1-9]\d*$/.test(id)) return
    void request<components['schemas']['Notification']>(`/v1/notifications/${id}`).then(async notification => {
      if (!active) return
      const entryID = notification.entry_ids?.[0]
      if (!entryID) { go(`/notifications/${id}${rest}`, {replace: true}); return }
      const entry = await request<Entry>(`/v1/entries/${entryID}`)
      if (!active) return
      remember(entry); setSelected(entry)
      go(`${viewHref(TODAY, entry.id)}${rest}`, {replace: true})
    }).catch(e => {if (active) {setError(e instanceof Error ? e.message : 'Could not open notification.'); go(`/notifications${rest}`, {replace: true})}})
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
  const pipeline = useQuery({queryKey:['pipeline'],enabled:signedIn,queryFn:({signal}) => request<components['schemas']['PipelineStatus']>('/v1/pipeline',{signal}),staleTime:30_000})
  const savedSearches = useSavedSearches(signedIn)
  const unreadCounts = useQuery({queryKey:['unread-counts'],enabled:signedIn && settings.reading_mode === 'unread_counts',queryFn:({signal})=>readUnreadCounts(signal),staleTime:30_000})
  const shortcuts = settings.single_key_shortcuts && preferences.isSuccess
  const subs = (subscriptions.data ?? []).filter(sub => !sub.deleted)

  // Resolve the route's view against the loaded resources. A feed, folder or
  // label that no longer exists is reported, not shown as an empty list.
  const riverRoute = route.name === 'river' ? route : null
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
  const openID = riverRoute?.entry
  // An article link opened offline cannot fetch the article, but the list it
  // belongs to usually comes from the offline cache a moment later; the retry
  // finds it there.
  const openQuery = useQuery({
    queryKey: ['entry', userID, openID], enabled: signedIn && openID !== undefined, staleTime: Infinity,
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
      if (open.name === 'river' && open.entry !== undefined) go(viewHref(open.view), {replace: true})
      // Old revision numbers must not re-enter sync from an offline response.
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
  }, [queryClient, go])
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
      if (register === 'seen') {
        await sync.act(entry, 'seen', before.seen.value)
        if (before.shelf.value === 'done') await sync.act(entry, 'shelf', 'done')
      } else {
        await sync.act(entry, register, before[register].value)
      }
      count(register, value, -1)
      notify('Undone.')
    }
  }
  const actions: ReaderActions = {
    state: entry => sync.state(entry),
    act: async (entry, register, value, kind = 'explicit') => {
      const before = sync.state(entry)
      await sync.act(entry, register, value, kind)
      count(register, value, 1)
      if (kind === 'explicit') notify(actionMessage(register, value), undoFor(entry, register, value, before))
    },
  }

  const openArticle = useCallback((entry: Entry, options: {view?: ViewRef; mode?: OpenMode} = {}) => {
    if (!alive.current) return
    remember(entry)
    setSelected(entry)
    const replace = options.mode === 'replace'
    const now: Route = routeRef.current
    const target = options.view ?? (now.name === 'river' ? now.view : TODAY)
    go(viewHref(target, entry.id), {replace, from: replace ? openedFrom() : currentHref()})
  }, [go, remember])
  // From a list: the first article pushes an entry so Back returns to the
  // list; moving between articles replaces it so Back still does.
  const openFromList = useCallback((entry: Entry, mode?: OpenMode) => openArticle(entry, {mode: mode ?? (routeRef.current.name === 'river' && routeRef.current.entry !== undefined ? 'replace' : 'push')}), [openArticle])
  const openElsewhere = useCallback((entry: Entry) => openArticle(entry, {view: routeRef.current.name === 'river' ? routeRef.current.view : TODAY, mode: 'push'}), [openArticle])
  const listHref = viewHref(viewRef)
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
    return whenPresent(() => document.querySelector<HTMLElement>(`.river [data-entry="${was}"] .entry`) ?? document.querySelector<HTMLElement>('.river h2'), target => target.focus({preventScroll: false}), 10)
  }, [openID])

  // Screens other than the river put focus on their heading when the route
  // changes, unless the screen already moved it somewhere useful.
  const firstRunShown = route.name === 'river' && route.entry === undefined && !onboarded && subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0
  const section = firstRunShown ? 'welcome' : routeSection(route)
  useEffect(() => {
    if ((route.name === 'river' && section !== 'welcome') || route.name === 'not-found') return
    return whenPresent(() => page.current?.querySelector<HTMLElement>('h2'), heading => {
      if (page.current?.contains(document.activeElement) && document.activeElement !== page.current) return
      if (!heading.hasAttribute('tabindex')) heading.setAttribute('tabindex', '-1')
      heading.focus({preventScroll: true})
    })
    // The section, not every query-string change, decides when focus moves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [section, identity])
  useScrollRestoration(page, location, section)

  useEffect(() => { if (!narrow && drawer.current?.open) drawer.current.close() }, [narrow])
  useEffect(() => { if (drawer.current?.open) drawer.current.close() }, [location.href])

  const pageTitle = (() => {
    switch (route.name) {
    case 'river': return openEntry ? `${openEntry.title || 'Untitled'} · ${view.title}` : openID !== undefined && openQuery.isError ? `Article unavailable · ${view.title}` : view.title
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
      const reading = routeRef.current.name === 'river' && routeRef.current.entry !== undefined
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
  const articleCommand = (id: string, title: string, key: string, run: (entry: Entry) => Promise<void> | void): Command => ({id, title, key, group:'Article', disabled:!current, run:async () => {if (current) await run(current)}})
  const commands: Command[] = [
    ...READING_VIEWS.map(item => ({id:`view-${item.kind}`, title:item.title, group:'Go to', key:item.shortcut, run:() => {go(viewHref({kind:item.kind}))}})),
    ...subs.map(sub => ({id:`feed-${sub.id}`, title:`Feed: ${sub.title || sub.url}`, group:'Feed', run:() => {go(viewHref({kind:'feed', id:sub.id}))}})),
    ...(folders.data ?? []).filter(folder => !folder.deleted).map(folder => ({id:`folder-${folder.id}`, title:`Folder: ${folder.name}`, group:'Folder', run:() => {go(viewHref({kind:'folder', id:folder.id}))}})),
    ...(labels.data ?? []).filter(label => !label.deleted).map(label => ({id:`label-${label.id}`, title:`Label: ${label.name}`, group:'Label', run:() => {go(viewHref({kind:'label', id:label.id}))}})),
    ...(savedSearches.data ?? []).map(saved => ({id:`saved-${saved.id}`, title:`Search: ${saved.name}`, group:'Saved search', run:() => {go(`/search/saved/${saved.id}`)}})),
    articleCommand('done', 'Mark Done', 'e', entry => actions.act(entry,'shelf','done')),
    articleCommand('later', 'Toggle Later', 's', entry => actions.act(entry,'shelf',actions.state(entry).shelf.value === 'later' ? 'none' : 'later')),
    articleCommand('keep', 'Toggle Keep', 'f', entry => actions.act(entry,'kept',actions.state(entry).kept.value !== true)),
    articleCommand('unread', 'Mark unread', 'm', entry => actions.act(entry,'seen','unseen')),
    articleCommand('original', 'Open original', 'v', entry => {window.open(entry.url, '_blank', 'noopener,noreferrer')}),
    articleCommand('copy-link', 'Copy Earnest link to the article', '', async entry => {await navigator.clipboard.writeText(new URL(viewHref(viewRef, entry.id), window.location.origin).href);notify('Link copied.')}),
    {id:'undo', title:'Undo the last action', key:'z', group:'Article', disabled:!toast?.undo, run:async () => {await toast?.undo?.()}},
    {...markRestCommand(view, subs, newestLoaded.key === viewKey(view) ? newestLoaded.seq : 0, (scope, throughSeq) => sync.actRange(scope, throughSeq)), group:'Article', key:undefined},
    {id:'subscribe', title:'Subscribe to a feed', group:'Feeds', run:() => {go('/feeds#subscribe')}},
    {id:'search', title:'Search', key:'/', group:'Go to', run:() => {go('/search')}},
    ...(pipeline.data?.ask === 'enabled' ? [{id:'ask', title:'Ask my feeds', group:'Go to', run:() => {go('/ask')}}] : []),
    {id:'rules', title:'Rules', key:'g r', group:'Go to', run:() => {go('/rules')}},
    {id:'activity', title:'Activity', key:'g a', group:'Go to', run:() => {go('/activity')}},
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
        await request('/v1/session/logout', {method:'POST'})
        sessionRevoked.current = true
      }
      await clearOffline()
      await queryClient.cancelQueries()
      queryClient.clear()
      localStorage.removeItem('earnest.csrf'); sessionStorage.removeItem('earnest.csrf')
      localStorage.removeItem('earnest.lastVisit')
      // The next account starts on Today, not on this account's feed or article.
      resetLocation('/')
      // Remount every session-owned hook, including callbacks still awaiting I/O.
      onLogout()
    }
    catch (error) { setError(error instanceof Error ? error.message : 'Could not sign out.') }
  }

  if (checking) return <main className="login" aria-busy="true"><Brand /><p role="status" className="muted center">Checking your session…</p></main>
  if (resettingGeneration) return <main className="login"><Brand /><p role="status" className="muted center">Refreshing reading copies…</p>{error && <p role="alert">{error} <button onClick={() => refreshResources('generation')}>Retry</button></p>}</main>
  if (!identity) return <Login onSignIn={login} />

  const firstRun = route.name === 'river' && openID === undefined && !onboarded && (onboardingStarted || (subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0))
  const reading = openID !== undefined
  // Offline, failed requests are expected; the offline notice says so once
  // instead of a banner per failed request.
  const syncTone = sync.error && online ? 'problem' as const : !online || /waiting|Restoring|Opening/.test(sync.status) ? 'waiting' as const : 'quiet' as const
  const syncText = !online ? `Offline. ${sync.status.replace(/\.$/, '')}` : sync.error ? 'Sync needs attention' : sync.status.replace(/\.$/, '')
  const sidebar = <Sidebar route={route} subscriptions={subs} folders={folders.data ?? []} labels={labels.data ?? []} savedSearches={savedSearches.data ?? []} counts={settings.reading_mode === 'unread_counts' ? unreadCounts.data : undefined} askEnabled={pipeline.data?.ask === 'enabled'} sync={{text: syncText, tone: syncTone}} onNavigate={() => drawer.current?.close()} onPalette={() => {drawer.current?.close(); palette.current?.openPalette()}} onHelp={() => {drawer.current?.close(); palette.current?.openHelp()}} onSignOut={() => {void logout()}} prefetch={prefetch} />

  function screen(): ReactNode {
    if (firstRun) return <div className="page" ref={page}><div className="page-inner onboarding" onChangeCapture={() => setOnboardingStarted(true)} onClickCapture={() => setOnboardingStarted(true)}><Suspense fallback={<PageSkeleton />}><OnboardingScreen onComplete={() => {setOnboarded(true);refreshResources();void queryClient.invalidateQueries({queryKey:['position']});go('/today', {replace: true})}} /></Suspense></div></div>
    switch (route.name) {
    case 'river': return <div className={`panes${reading ? ' reading' : ''}`}>
      <div className="list-pane" inert={compact && reading}>
        {resolved.missing
          ? <section className="river"><div className="empty-state"><h2 tabIndex={-1}>This {viewRef.kind} is gone</h2><p>It may have been deleted or unsubscribed on another device.</p><p><Link to="/today">Go to Today</Link></p></div></section>
          : <River since={since} sessionStart={sessionStart} key={viewKey(view)} onNewestLoaded={seq => setNewestLoaded({key: viewKey(view), seq})} view={view} subscriptions={subs} actions={actions} selected={current} onSelect={setSelected} onOpen={openFromList} reading={reading} scrollMemory={scrollMemory.current} shortcuts={shortcuts} counts={settings.reading_mode === 'unread_counts'} summary={summary} />}
      </div>
      <div className="reader-pane">
        {!reading ? <ReaderPlaceholder shortcuts={shortcuts} />
          : openEntry ? <Reader key={openEntry.id} onOpen={entry => openArticle(entry, {mode: 'push'})} entry={openEntry} subscription={subs.find(sub => sub.id === openEntry.subscription_id)} settings={settings} actions={actions} onSettings={savedSettings} onNotify={notify} renderBody={url => <iframe title="Article body" src={url} sandbox="allow-popups allow-popups-to-escape-sandbox" />} onClose={closeReader} />
          : openQuery.isError ? <ReaderProblem error={openQuery.error} onBack={closeReader} onRetry={() => {void openQuery.refetch()}} />
          : <ReaderSkeleton />}
      </div>
    </div>
    case 'search': {
      const saved = route.saved === undefined ? undefined : savedSearches.data?.find(item => item.id === route.saved)
      return <div className="page" ref={page}><div className="page-inner">{route.saved !== undefined && !saved
        ? savedSearches.isPending ? <PageSkeleton /> : <div className="empty-state"><h2>This saved search no longer exists</h2><p><Link to="/search">Start a new search</Link></p></div>
        : <Search key={route.saved ?? 'search'} saved={saved} route={route.saved === undefined ? route.search : undefined} onRoute={(next, replace) => {go(searchHref(next), {replace})}} onDeleted={() => {notify('Saved search deleted.');go('/search', {replace: true})}} onOpen={openElsewhere} subscriptions={subs} folders={folders.data ?? []}/>}</div></div>
    }
    case 'ask': return <div className="page" ref={page}><div className="page-inner">{pipeline.data?.ask === 'enabled' ? <Ask onOpen={openElsewhere}/> : pipeline.isPending ? <PageSkeleton /> : <div className="empty-state"><h2>Ask my feeds is off</h2><p>Questions need the ask task, which an operator enables in the server configuration.</p><p><Link to="/search">Search your feeds instead</Link></p></div>}</div></div>
    case 'rules': return <div className="page" ref={page}><div className="page-inner"><Suspense fallback={<PageSkeleton />}><RulesScreen feeds={subs} folders={folders.data ?? []} labels={labels.data ?? []} editing={route.rule} onEdit={id => {go(`/rules/${id}`)}} onClose={() => { if (openedFrom() === '/rules') history.back(); else go('/rules', {replace: true}) }} /></Suspense></div></div>
    case 'activity': return <div className="page" ref={page}><div className="page-inner"><ActivityScreen filters={route.filters} onFilters={filters => {go(activityHref(filters), {replace: true})}} /></div></div>
    case 'notifications': return <div className="page" ref={page}><div className="page-inner"><Notifications selectedID={route.id} onSelect={id => {go(`/notifications/${id}`, {replace: route.id !== undefined})}} onOpen={openElsewhere} onOpenSettings={() => {go('/settings#push')}} /></div></div>
    case 'feeds': return <div className="page" ref={page}><div className="page-inner"><Suspense fallback={<PageSkeleton />}><div className="page-header"><h2>Manage feeds</h2></div><FeedsScreen onChanged={refreshResources} /></Suspense></div></div>
    case 'settings': return <div className="page" ref={page}><div className="page-inner wide"><Suspense fallback={<PageSkeleton />}><SettingsScreen device={route.device} settings={settings} onSaved={savedSettings} /></Suspense></div></div>
    case 'not-found': return <div className="page" ref={page}><div className="page-inner"><div className="empty-state"><h2 tabIndex={-1}>There is nothing at this address</h2><p>The link may be mistyped, or the page moved.</p><p><Link to="/today">Go to Today</Link></p></div></div></div>
    }
  }

  return <div className={`app${reading ? ' is-reading' : ''}`}>
    <a className="skip-link" href="#main" onClick={event => {event.preventDefault();document.getElementById('main')?.focus()}}>Skip to content</a>
    {narrow && <header className="topbar">
      <button type="button" className="icon-button" aria-haspopup="dialog" aria-expanded={drawerOpen} onClick={() => drawer.current?.showModal()}><Icon name="menu" /><span className="sr-only">Menu</span></button>
      <p className="topbar-title">{route.name === 'river' ? (reading ? view.title : '') : pageTitle}</p>
      {syncTone !== 'quiet' && <span className={`sync-dot ${syncTone}`} title={syncText}><Icon name={syncTone === 'problem' ? 'warning' : 'sync'} size={16} /><span className="sr-only">{syncText}</span></span>}
      <Link to="/search" className="icon-button"><Icon name="search" /><span className="sr-only">Search</span></Link>
      <button type="button" className="icon-button" onClick={() => palette.current?.openPalette()}><Icon name="command" /><span className="sr-only">Command palette</span></button>
    </header>}
    {narrow
      ? <dialog ref={drawer} className="drawer" aria-label="Navigation" onClose={() => setDrawerOpen(false)} onToggle={event => setDrawerOpen(event.currentTarget.open)} onClick={event => {if (event.target === drawer.current) drawer.current.close()}}>{sidebar}</dialog>
      : sidebar}
    <div className="workspace">
      <div className="banners">
        {error && <p role="alert" className="banner problem">{error} <button type="button" className="quiet" onClick={() => setError('')}>Dismiss</button></p>}
        {(subscriptions.error || preferences.error) && <p role="alert" className="banner problem">{subscriptions.error?.message || preferences.error?.message} <button type="button" className="quiet" onClick={() => {void subscriptions.refetch();void preferences.refetch()}}>Try again</button></p>}
        {online && settings.reading_mode === 'unread_counts' && unreadCounts.error && <p role="alert" className="banner problem">Unread counts: {unreadCounts.error.message}</p>}
        {online && sync.error && <p role="alert" className="banner problem">{sync.error} <button type="button" onClick={sync.retry}>Retry sync</button></p>}
        <OfflineStatus/>
      </div>
      <main id="main" tabIndex={-1} className={`main ${route.name === 'river' && !firstRun ? 'is-river' : 'is-page'}`}>
        <ScreenBoundary resetKey={section}>{screen()}</ScreenBoundary>
      </main>
    </div>
    <Palette ref={palette} commands={commands} shortcuts={shortcuts} triggers={false} />
    <ToastRegion toast={toast} onDismiss={dismissToast} />
  </div>
}

function ToastRegion({toast, onDismiss}: {toast: Toast | null; onDismiss: () => void}) {
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (!toast || paused) return
    const timer = window.setTimeout(onDismiss, TOAST_MS)
    return () => window.clearTimeout(timer)
  }, [toast, paused, onDismiss])
  // The region exists before any message so screen readers announce changes.
  return <div className="toast-region" role="status" aria-live="polite" aria-atomic="true">
    {toast && <div key={toast.id} className="toast" onPointerEnter={() => setPaused(true)} onPointerLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}>
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
  return <section className="reader-placeholder" aria-label="Reader">
    <div role="alert" className="problem">
      <h2>{gone ? 'This article is no longer available' : 'The article did not open'}</h2>
      <p>{gone ? 'It may have expired from the river or been removed by its publisher.' : `${error.message} Check your connection and try again.`}</p>
      <p className="button-row">{!gone && <button type="button" className="primary" onClick={onRetry}>Try again</button>}<button type="button" onClick={onBack}>Back to the list</button></p>
    </div>
  </section>
}

function PageSkeleton() {
  return <div aria-busy="true"><p role="status" className="sr-only">Loading…</p><div className="skeleton line title short" aria-hidden="true" /><div className="skeleton block" aria-hidden="true" /><div className="skeleton block" aria-hidden="true" /></div>
}
