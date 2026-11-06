import {useCallback, useEffect, useRef, useState} from 'react'
import {createRoot} from 'react-dom/client'
import {QueryClient, QueryClientProvider, useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from './api.generated'
import {request, setCSRF} from './request'
import {readUnreadCounts} from './counts'
import {River, type SessionSummary} from './River'
import {Reader} from './ArticleReader'
import {Settings, defaultSettings, type ReadingSettings} from './Settings'
import {Feeds} from './FeedSettings'
import {Onboarding} from './Onboarding'
import {ActivityScreen, Notifications} from './agents/Surfaces'
import {Tokens, Push, Webhooks} from './account/Account'
import {useReaderSync} from './syncBridge'
import {type Entry, type ReaderActions, type Subscription, type View, viewKey, viewScope} from './reader'
import {Rules} from './rules/Rules'
import {Search, useSavedSearches, type SavedSearch} from './search/Search'
import {Palette, type Command} from './palette/Palette'
import {OfflineStatus, clearOffline, registerOffline} from './offline/Offline'
import {Ask} from './pipeline/Pipeline'
import './style.css'

type Identity = {user_id: number; principal_id: number}
const standardViews: View[] = [{kind:'today', title:'Today'}, {kind:'edition', title:'Edition'}, {kind:'later', title:'Later'}, {kind:'kept', title:'Kept'}, {kind:'done', title:'Done'}]

function App() {
  const [session, setSession] = useState(0)
  return <Session key={session} onLogout={() => setSession(value => value + 1)} />
}

function Session({onLogout}: {onLogout: () => void}) {
  const queryClient = useQueryClient()
  const [identity, setIdentity] = useState<Identity | null>(null)
  const sessionRevoked = useRef(false)
  const [checking, setChecking] = useState(() => Boolean(localStorage.getItem('earnest.csrf')))
  const [resettingGeneration,setResettingGeneration] = useState(false)
  const [error, setError] = useState('')
  const [view, setView] = useState<View>(standardViews[0])
  const [panel, setPanel] = useState<'river' | 'feeds' | 'settings' | 'activity' | 'notifications' | 'rules' | 'search' | 'ask'>(() => location.pathname === '/device' ? 'settings' : 'river')
  const [savedSearch,setSavedSearch] = useState<SavedSearch | undefined>()
  const [selected, setSelected] = useState<Entry | null>(null)
  const [opened, setOpened] = useState(false)
  const [onboarded, setOnboarded] = useState(false)
  const [onboardingStarted, setOnboardingStarted] = useState(false)
  const [summary, setSummary] = useState<SessionSummary>({seen:0,done:0,later:0,kept:0})
  const [sessionStart] = useState(() => new Date().toISOString())
  const [since] = useState(() => localStorage.getItem('earnest.lastVisit') ?? new Date(0).toISOString())
  useEffect(() => { const save = () => localStorage.setItem('earnest.lastVisit', new Date().toISOString()); window.addEventListener('pagehide', save); return () => window.removeEventListener('pagehide', save) }, [])
  const notificationOpened = useRef(false)
  useEffect(() => {
    const id = new URLSearchParams(location.search).get('notification')
    if (!identity || !id || notificationOpened.current) return
    notificationOpened.current = true
    const url = new URL(location.href)
    url.searchParams.delete('notification')
    history.replaceState(history.state, '', url)
    let active = true
    if (!/^[1-9]\d*$/.test(id)) return
    void request<components['schemas']['Notification']>(`/v1/notifications/${id}`).then(async notification => {
      if (!active) return
      const entryID = notification.entry_ids?.[0]
      if (!entryID) { setPanel('notifications'); return }
      const entry = await request<Entry>(`/v1/entries/${entryID}`)
      if (!active) return
      setSelected(entry); setPanel('river'); setOpened(true)
    }).catch(e => {if (active) {setError(e instanceof Error ? e.message : 'Could not open notification.'); setPanel('notifications')}})
    return () => {active = false}
  }, [identity])
  const help = useRef<HTMLDialogElement>(null)
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
  const subscriptions = useQuery({queryKey: ['subscriptions'], enabled: signedIn, queryFn: ({signal}) => request<Subscription[]>('/v1/subscriptions',{signal})})
  const folders = useQuery({queryKey: ['folders'], enabled: signedIn, queryFn: ({signal}) => request<components['schemas']['Folder'][]>('/v1/folders',{signal})})
  const labels = useQuery({queryKey: ['labels'], enabled: signedIn, queryFn: ({signal}) => request<components['schemas']['Label'][]>('/v1/labels',{signal})})
  const preferences = useQuery({queryKey: ['settings'], enabled: signedIn, queryFn: ({signal}) => request<ReadingSettings>('/v1/settings',{signal})})
  const settings = preferences.data ?? defaultSettings
  const pipeline = useQuery({queryKey:['pipeline'],enabled:signedIn,queryFn:({signal}) => request<components['schemas']['PipelineStatus']>('/v1/pipeline',{signal}),staleTime:30_000})
  const savedSearches = useSavedSearches(signedIn)
  const unreadCounts = useQuery({queryKey:['unread-counts'],enabled:signedIn && settings.reading_mode === 'unread_counts',queryFn:({signal})=>readUnreadCounts(signal),staleTime:30_000})
  const refreshResources = useCallback((reason?: 'generation') => {
    const invalidateResources = () => {
      for (const resource of ['settings', 'subscriptions', 'folders', 'labels', 'unread-counts', 'saved-searches']) void queryClient.invalidateQueries({queryKey: [resource]})
    }
    if (reason === 'generation') {
      setResettingGeneration(true);setSelected(null);setOpened(false)
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
  }, [queryClient])
  const sync = useReaderSync(identity, refreshResources)
  const retrySync = sync.retry
  const actions: ReaderActions = {
    state: entry => sync.state(entry),
    act: async (entry, register, value, kind = 'explicit') => {
      await sync.act(entry, register, value, kind)
      setSummary(old => ({...old, seen: old.seen + Number(register === 'seen' && value === 'seen'), done: old.done + Number(register === 'shelf' && value === 'done'), later: old.later + Number(register === 'shelf' && value === 'later'), kept: old.kept + Number(register === 'kept' && value === true)}))
    },
  }
  const choose = useCallback((next: View) => { setView(next); setPanel('river'); setSelected(null); setOpened(false) }, [])
  const focusPanel = useCallback((node: HTMLElement | null) => { node?.focus() }, [])
  useEffect(() => {
    const online = () => retrySync()
    window.addEventListener('online',online)
    return () => window.removeEventListener('online',online)
  },[retrySync])
  function showSearch(saved?: SavedSearch) {setSavedSearch(saved);setPanel('search');setOpened(false)}
  function openEntry(entry: Entry) {choose(standardViews[0]);setSelected(entry);setOpened(true)}
  const commands: Command[] = [
    ...standardViews.map(item => ({id:`view-${item.kind}`,title:item.title,key:'Ctrl+K, Enter',run:() => choose(item)})),
    ...(subscriptions.data ?? []).filter(sub => !sub.deleted).map(sub => ({id:`feed-${sub.id}`,title:`Feed: ${sub.title || sub.url}`,key:'Ctrl+K, Enter',run:() => choose({kind:'feed',id:sub.id,title:sub.title || sub.url})})),
    ...(folders.data ?? []).filter(folder => !folder.deleted).map(folder => ({id:`folder-${folder.id}`,title:`Folder: ${folder.name}`,key:'Ctrl+K, Enter',run:() => choose({kind:'folder',id:folder.id,title:folder.name})})),
    ...(labels.data ?? []).filter(label => !label.deleted).map(label => ({id:`label-${label.id}`,title:`Label: ${label.name}`,key:'Ctrl+K, Enter',run:() => choose({kind:'label',id:label.id,title:label.name})})),
    ...(savedSearches.data ?? []).map(saved => ({id:`saved-${saved.id}`,title:`Search: ${saved.name}`,key:'Ctrl+K, Enter',run:() => showSearch(saved)})),
    {id:'done',title:'Mark Done',key:'e',disabled:!selected,run:async () => {if(selected) await actions.act(selected,'shelf','done')}},
    {id:'later',title:'Toggle Later',key:'s',disabled:!selected,run:async () => {if(selected) await actions.act(selected,'shelf',actions.state(selected).shelf.value === 'later' ? 'none' : 'later')}},
    {id:'keep',title:'Toggle Keep',key:'f',disabled:!selected,run:async () => {if(selected) await actions.act(selected,'kept',actions.state(selected).kept.value !== true)}},
    {id:'unread',title:'Mark unread',key:'m',disabled:!selected,run:async () => {if(selected) await actions.act(selected,'seen','unseen')}},
    {id:'subscribe',title:'Subscribe to a feed',key:'Ctrl+K, Enter',run:() => {setPanel('feeds');setOpened(false)}},
    {id:'search',title:'Search',key:'/',run:() => showSearch()},
    ...(pipeline.data?.ask === 'enabled' ? [{id:'ask',title:'Ask my feeds',key:'Ctrl+K, Enter',run:() => {setPanel('ask');setOpened(false)}}] : []),
    {id:'rules',title:'Rules',key:'Ctrl+K, Enter',run:() => {setPanel('rules');setOpened(false)}},
    {id:'settings',title:'Settings',key:'Ctrl+K, Enter',run:() => {setPanel('settings');setOpened(false)}},
  ]
  function savedSettings(value: ReadingSettings) { queryClient.setQueryData(['settings'], value) }
  async function login(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    try {
      const result = await request<{csrf_token: string}>('/v1/session/login', {method:'POST', body:JSON.stringify({username:String(form.get('username')), password:String(form.get('password'))})})
      setCSRF(result.csrf_token)
      setIdentity(await request<Identity>('/v1/me')); setError('')
    } catch (error) { setError(error instanceof Error ? error.message : 'Sign in failed.') }
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
      // Remount every session-owned hook, including callbacks still awaiting I/O.
      onLogout()
    }
    catch (error) { setError(error instanceof Error ? error.message : 'Could not sign out.') }
  }
  if (checking) return <main className="login" role="status">Checking your session…</main>
  if (resettingGeneration) return <main className="login" role="status">Refreshing reading copies…{error && <p role="alert">{error} <button onClick={() => refreshResources('generation')}>Retry</button></p>}</main>
  if (!identity) return <main className="login"><h1>Earnest</h1><form onSubmit={login}><label>Username<input name="username" autoComplete="username" required /></label><label>Password<input name="password" type="password" autoComplete="current-password" required /></label><button>Sign in</button>{error && <p role="alert">{error}</p>}</form></main>
  const firstRun = panel === 'river' && !opened && !onboarded && (onboardingStarted || (subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0))
  return <div className={`layout${opened ? ' reading' : ''}`} data-theme={settings.theme}>
    <a className="skip-link" href="#workspace">Skip to articles</a>
    <nav aria-label="Views"><h1>Earnest</h1>{standardViews.map(item => <button key={item.kind} aria-current={panel === 'river' && viewKey(view) === viewKey(item) ? 'page' : undefined} onClick={() => choose(item)}>{item.title}</button>)}
      {(subscriptions.data ?? []).length > 0 && <details open><summary>Feeds</summary>{subscriptions.data?.map(sub => <button key={sub.id} onClick={() => choose({kind:'feed', id:sub.id, title:sub.title})}>{sub.title || sub.url}{settings.reading_mode === 'unread_counts' && unreadCounts.data && ` (${unreadCounts.data.byFeed[String(sub.id)] ?? 0})`}</button>)}</details>}
      {(folders.data ?? []).length > 0 && <details><summary>Folders</summary>{folders.data?.map(folder => <button key={folder.id} onClick={() => choose({kind:'folder', id:folder.id, title:folder.name})}>{folder.name}</button>)}</details>}
      {(labels.data ?? []).length > 0 && <details><summary>Labels</summary>{labels.data?.filter(label => !label.deleted).map(label => <button key={label.id} onClick={() => choose({kind:'label', id:label.id, title:label.name})}>{label.name}</button>)}</details>}
      {(savedSearches.data ?? []).map(saved => <button key={saved.id} onClick={() => showSearch(saved)}>{saved.name}</button>)}
      <button onClick={() => showSearch()}>Search</button>{pipeline.data?.ask === 'enabled' && <button onClick={() => {setPanel('ask');setOpened(false)}}>Ask my feeds</button>}<button onClick={() => {setPanel('rules');setOpened(false)}}>Rules</button>
      <button onClick={() => {setPanel('activity');setOpened(false)}}>Activity</button><button onClick={() => {setPanel('notifications');setOpened(false)}}>Notifications</button><button onClick={() => {setPanel('feeds');setOpened(false)}}>Manage feeds</button><button onClick={() => {setPanel('settings');setOpened(false)}}>Settings</button><Palette commands={commands} shortcuts={settings.single_key_shortcuts && preferences.isSuccess} helpRef={help}/><button onClick={() => {void logout()}}>Sign out</button>
    </nav>
    <div id="workspace" tabIndex={-1} className={`workspace ${panel === 'river' && !firstRun ? 'with-reader' : ''}`}>
      <div className="sync-messages">
      {error && <p role="alert">{error}</p>}
      {(subscriptions.error || preferences.error) && <p role="alert">{subscriptions.error?.message || preferences.error?.message}</p>}
      {settings.reading_mode === 'unread_counts' && unreadCounts.error && <p role="alert">Unread counts: {unreadCounts.error.message}</p>}
      <OfflineStatus/><p className="sync-status" role="status">{sync.status}</p>
      {sync.error && <p role="alert">{sync.error} <button onClick={sync.retry}>Retry sync</button></p>}
      </div>
      {firstRun ? <main onChangeCapture={() => setOnboardingStarted(true)} onClickCapture={() => setOnboardingStarted(true)}><Onboarding onComplete={() => {setOnboarded(true);refreshResources();void queryClient.invalidateQueries({queryKey:['position']});choose(standardViews[0])}} /></main> : panel === 'ask' && pipeline.data?.ask === 'enabled' ? <Ask onOpen={openEntry}/> : panel === 'rules' ? <Rules feeds={subscriptions.data ?? []} folders={folders.data ?? []} labels={labels.data ?? []}/> : panel === 'search' ? <Search key={savedSearch?.id ?? 'search'} saved={savedSearch} onOpen={openEntry}/> : panel === 'feeds' ? <main key="feeds" tabIndex={-1} ref={focusPanel}><h2>Feeds</h2><Feeds onChanged={refreshResources} /></main> : panel === 'activity' ? <ActivityScreen /> : panel === 'notifications' ? <Notifications onOpen={entry => {choose(standardViews[0]);setSelected(entry);setOpened(true)}} /> : panel === 'settings' ? <main key="settings" tabIndex={-1} ref={focusPanel}><Settings key={settings.rev} settings={settings} onSaved={savedSettings} /><Tokens /><Push /><Webhooks /></main> : <>
        <River since={since} sessionStart={sessionStart} key={viewKey(view)} view={view} subscriptions={subscriptions.data ?? []} actions={actions} selected={selected} onSelect={setSelected} onOpen={entry => {setSelected(entry);setOpened(true)}} shortcuts={settings.single_key_shortcuts && preferences.isSuccess} counts={settings.reading_mode === 'unread_counts'} summary={summary} onHelp={() => help.current?.showModal()} onMarkRestRead={throughSeq => { void sync.actRange(viewScope(view), throughSeq).catch(error => setError(error instanceof Error ? error.message : 'Could not mark the rest read.')) }} />
        {selected && opened ? <Reader key={selected.id} onOpen={openEntry} entry={selected} subscription={subscriptions.data?.find(sub => sub.id === selected.subscription_id)} settings={settings} actions={actions} onSettings={savedSettings} renderBody={url => <iframe title="Article body" src={url} sandbox="allow-popups allow-popups-to-escape-sandbox" />} onClose={() => {setOpened(false);window.requestAnimationFrame(() => document.querySelector<HTMLElement>('.river h2')?.focus())}} /> : <article className="reader-placeholder" aria-label="Reader"><p>Choose an article. It has been waiting patiently.</p></article>}
      </>}
    </div>
  </div>
}
void registerOffline().catch(error => console.error('Offline reading unavailable:', error))
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:1,networkMode:'always'}}})}><App /></QueryClientProvider>)
