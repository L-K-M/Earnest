import {useCallback, useEffect, useRef, useState} from 'react'
import {createRoot} from 'react-dom/client'
import {QueryClient, QueryClientProvider, useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from './api.generated'
import {request, setCSRF} from './request'
import {readUnreadCounts} from './counts'
import {River, type SessionSummary} from './River'
import {Reader} from './Reader'
import {Settings, defaultSettings, type ReadingSettings} from './Settings'
import {Feeds} from './Feeds'
import {Onboarding} from './Onboarding'
import {useReaderSync} from './syncBridge'
import {type Entry, type ReaderActions, type Subscription, type View, viewKey} from './reader'
import './style.css'

type Identity = {user_id: number; principal_id: number}
const standardViews: View[] = [{kind:'today', title:'Today'}, {kind:'edition', title:'Edition'}, {kind:'later', title:'Later'}, {kind:'kept', title:'Kept'}, {kind:'done', title:'Done'}]

function App() {
  const queryClient = useQueryClient()
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [checking, setChecking] = useState(() => Boolean(localStorage.getItem('earnest.csrf')))
  const [error, setError] = useState('')
  const [view, setView] = useState<View>(standardViews[0])
  const [panel, setPanel] = useState<'river' | 'feeds' | 'settings'>('river')
  const [selected, setSelected] = useState<Entry | null>(null)
  const [opened, setOpened] = useState(false)
  const [onboarded, setOnboarded] = useState(false)
  const [onboardingStarted, setOnboardingStarted] = useState(false)
  const [summary, setSummary] = useState<SessionSummary>({seen:0,done:0,later:0,kept:0})
  const help = useRef<HTMLDialogElement>(null)
  const palette = useRef<HTMLDialogElement>(null)
  const signedIn = identity !== null
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
  const unreadCounts = useQuery({queryKey:['unread-counts'],enabled:signedIn && settings.reading_mode === 'unread_counts',queryFn:({signal})=>readUnreadCounts(signal),staleTime:30_000})
  const refreshResources = useCallback((reason?: 'generation') => {
    for (const resource of ['settings', 'subscriptions', 'folders', 'labels', 'unread-counts']) void queryClient.invalidateQueries({queryKey: [resource]})
    if (reason === 'generation') {
      setSelected(null);setOpened(false)
      void queryClient.resetQueries({queryKey:['position']})
      void queryClient.resetQueries({queryKey:['river']})
    }
  }, [queryClient])
  const sync = useReaderSync(identity, refreshResources)
  const actions: ReaderActions = {
    state: entry => sync.state(entry),
    act: async (entry, register, value, kind = 'explicit') => {
      await sync.act(entry, register, value, kind)
      setSummary(old => ({...old, seen: old.seen + Number(register === 'seen' && value === 'seen'), done: old.done + Number(register === 'shelf' && value === 'done'), later: old.later + Number(register === 'shelf' && value === 'later'), kept: old.kept + Number(register === 'kept' && value === true)}))
    },
  }
  const choose = useCallback((next: View) => { setView(next); setPanel('river'); setSelected(null); setOpened(false); palette.current?.close() }, [])
  const focusPanel = useCallback((node: HTMLElement | null) => { node?.focus() }, [])
  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k' && signedIn) { event.preventDefault(); palette.current?.showModal() }
    }
    window.addEventListener('keydown', keyboard)
    return () => window.removeEventListener('keydown', keyboard)
  }, [signedIn])
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
    try { await request('/v1/session/logout', {method:'POST'}); setIdentity(null); queryClient.clear(); localStorage.removeItem('earnest.csrf'); sessionStorage.removeItem('earnest.csrf') }
    catch (error) { setError(error instanceof Error ? error.message : 'Could not sign out.') }
  }
  if (checking) return <main className="login" role="status">Checking your session…</main>
  if (!identity) return <main className="login"><h1>Earnest</h1><form onSubmit={login}><label>Username<input name="username" autoComplete="username" required /></label><label>Password<input name="password" type="password" autoComplete="current-password" required /></label><button>Sign in</button>{error && <p role="alert">{error}</p>}</form></main>
  const firstRun = !onboarded && (onboardingStarted || (subscriptions.isSuccess && subscriptions.data.length === 0 && preferences.isSuccess && preferences.data.rev === 0))
  return <div className={`layout${opened ? ' reading' : ''}`} data-theme={settings.theme}>
    <a className="skip-link" href="#workspace">Skip to articles</a>
    <nav aria-label="Views"><h1>Earnest</h1>{standardViews.map(item => <button key={item.kind} aria-current={panel === 'river' && viewKey(view) === viewKey(item) ? 'page' : undefined} onClick={() => choose(item)}>{item.title}</button>)}
      {(subscriptions.data ?? []).length > 0 && <details open><summary>Feeds</summary>{subscriptions.data?.map(sub => <button key={sub.id} onClick={() => choose({kind:'feed', id:sub.id, title:sub.title})}>{sub.title || sub.url}{settings.reading_mode === 'unread_counts' && unreadCounts.data && ` (${unreadCounts.data.byFeed[String(sub.id)] ?? 0})`}</button>)}</details>}
      {(folders.data ?? []).length > 0 && <details><summary>Folders</summary>{folders.data?.map(folder => <button key={folder.id} onClick={() => choose({kind:'folder', id:folder.id, title:folder.name})}>{folder.name}</button>)}</details>}
      {(labels.data ?? []).length > 0 && <details><summary>Labels</summary>{labels.data?.filter(label => !label.Deleted).map(label => <button key={label.ID} onClick={() => choose({kind:'label', id:label.ID, title:label.Name})}>{label.Name}</button>)}</details>}
      <button onClick={() => {setPanel('feeds');setOpened(false)}}>Manage feeds</button><button onClick={() => {setPanel('settings');setOpened(false)}}>Settings</button><button onClick={() => help.current?.showModal()}>Keyboard help (?)</button><button onClick={() => palette.current?.showModal()}>Go to… (Ctrl+K)</button><button onClick={() => {void logout()}}>Sign out</button>
    </nav>
    <div id="workspace" tabIndex={-1} className={`workspace ${panel === 'river' && !firstRun ? 'with-reader' : ''}`}>
      <div className="sync-messages">
      {error && <p role="alert">{error}</p>}
      {(subscriptions.error || preferences.error) && <p role="alert">{subscriptions.error?.message || preferences.error?.message}</p>}
      {settings.reading_mode === 'unread_counts' && unreadCounts.error && <p role="alert">Unread counts: {unreadCounts.error.message}</p>}
      <p className="sync-status" role="status">{sync.status}</p>
      {sync.error && <p role="alert">{sync.error} <button onClick={sync.retry}>Retry sync</button></p>}
      </div>
      {firstRun ? <main onChangeCapture={() => setOnboardingStarted(true)} onClickCapture={() => setOnboardingStarted(true)}><Onboarding onComplete={() => {setOnboarded(true);refreshResources();void queryClient.invalidateQueries({queryKey:['position']});choose(standardViews[0])}} /></main> : panel === 'feeds' ? <main key="feeds" tabIndex={-1} ref={focusPanel}><h2>Feeds</h2><Feeds onChanged={refreshResources} /></main> : panel === 'settings' ? <main key="settings" tabIndex={-1} ref={focusPanel}><Settings key={settings.rev} settings={settings} onSaved={savedSettings} /></main> : <>
        <River key={viewKey(view)} view={view} subscriptions={subscriptions.data ?? []} actions={actions} selected={selected} onSelect={setSelected} onOpen={entry => {setSelected(entry);setOpened(true)}} shortcuts={settings.single_key_shortcuts && preferences.isSuccess} counts={settings.reading_mode === 'unread_counts'} summary={summary} onHelp={() => help.current?.showModal()} />
        {selected && opened ? <Reader key={selected.id} entry={selected} subscription={subscriptions.data?.find(sub => sub.id === selected.subscription_id)} settings={settings} actions={actions} onSettings={savedSettings} renderBody={url => <iframe title="Article body" src={url} sandbox="allow-popups allow-popups-to-escape-sandbox" />} onClose={() => {setOpened(false);window.requestAnimationFrame(() => document.querySelector<HTMLElement>('.river h2')?.focus())}} /> : <article className="reader-placeholder" aria-label="Reader"><p>Choose an article. It has been waiting patiently.</p></article>}
      </>}
    </div>
    <dialog ref={help} aria-labelledby="help-title"><h2 id="help-title">Keyboard shortcuts</h2><p>Single-key shortcuts work outside text fields. You can switch them off in Settings.</p><dl>{[['j / k','Next / previous article'],['o / Enter','Open article'],['v','Open original'],['e','Done'],['s','Toggle Later'],['f','Toggle Keep'],['m','Mark unread, clearing Done'],['?','This help'],['Ctrl+K / ⌘K','Go to a view']].map(([key, label]) => <div key={key}><dt><kbd>{key}</kbd></dt><dd>{label}</dd></div>)}</dl><button onClick={() => help.current?.close()}>Close</button></dialog>
    <dialog ref={palette} aria-labelledby="palette-title"><h2 id="palette-title">Go to</h2>{standardViews.map(item => <button key={item.kind} onClick={() => choose(item)}>{item.title}</button>)}<button onClick={() => palette.current?.close()}>Close</button></dialog>
  </div>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:1}}})}><App /></QueryClientProvider>)
