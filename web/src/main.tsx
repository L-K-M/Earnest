import React, {useState} from 'react'
import {createRoot} from 'react-dom/client'
import {QueryClient, QueryClientProvider, useQuery} from '@tanstack/react-query'
import createClient from 'openapi-fetch'
import type {paths, components} from './api.generated'
import './style.css'
import {emptyCopy} from './copy'

const api = createClient<paths>()
type Entry = components['schemas']['Entry']
type View = 'river' | 'later' | 'kept' | 'done'

function App() {
  const [signedIn, setSignedIn] = useState(false)
  const [error, setError] = useState('')
  const [view, setView] = useState<View>('river')
  const [selected, setSelected] = useState<Entry | null>(null)
  const entries = useQuery({queryKey: ['entries', view], enabled: signedIn, queryFn: async () => {
    const {data, error} = await api.GET('/v1/entries', {params: {query: {view, limit: 200}}})
    if (error || !data) throw new Error('Could not load entries.')
    if (data.entries?.some(entry => !Number.isSafeInteger(entry.id) || !Number.isSafeInteger(entry.seq))) throw new Error('Entry identifiers exceed the supported range.')
    return data
  }})
  const content = useQuery({queryKey: ['content', selected?.id], enabled: selected !== null, queryFn: async () => {
    const response = await fetch(`/web/content-url/${selected!.id}`)
    if (!response.ok) throw new Error('Could not load article.')
    return await response.json() as {url: string}
  }})
  async function login(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const {data, error} = await api.POST('/v1/session/login', {body: {username: String(form.get('username')), password: String(form.get('password'))}})
    if (error || !data) { setError('Sign in failed. Check your username and password.'); return }
    setError('')
    setSignedIn(true)
  }
  if (!signedIn) return <main className="login"><h1>Earnest</h1><form onSubmit={login}><label>Username<input name="username" autoComplete="username" required /></label><label>Password<input name="password" type="password" autoComplete="current-password" required /></label><button>Sign in</button>{error && <p role="alert">{error}</p>}</form></main>
  return <div className="layout"><nav aria-label="Views"><h1>Earnest</h1>{([['Today','river'],['Later','later'],['Kept','kept'],['Done','done']] as const).map(([label, value]) => <button key={value} aria-current={view === value ? 'page' : undefined} onClick={() => {setView(value); setSelected(null)}}>{label}</button>)}</nav><main aria-label="Articles"><h2>{view === 'river' ? 'Today' : view}</h2>{entries.error && <p role="alert">{entries.error.message}</p>}{entries.data?.entries?.map(entry => <button className="entry" key={entry.id} onClick={() => setSelected(entry)}>{entry.title || 'Untitled'}</button>)}{entries.data?.next_cursor && <p>More articles are available in the API.</p>}{entries.data?.entries?.length === 0 && <p>{emptyCopy()}</p>}</main><article aria-label="Reader">{selected && <><h2>{selected.title}</h2><p>{selected.author} {selected.published_at && new Date(selected.published_at).toLocaleDateString()}</p><a href={selected.url} target="_blank" rel="noopener noreferrer">Open original</a>{content.isError && <p role="alert">Could not load article.</p>}{content.data && <iframe title="Article body" src={content.data.url} sandbox="allow-popups allow-popups-to-escape-sandbox" />}</>}</article></div>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={new QueryClient()}><App /></QueryClientProvider>)
