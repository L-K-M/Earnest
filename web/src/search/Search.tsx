import {useState} from 'react'
import {useInfiniteQuery, useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry} from '../reader'
import {request} from '../request'
import {usePipeline} from './pipeline'

export type SavedSearch = components['schemas']['SavedSearch']
type Match = components['schemas']['SearchMatch']
type Page = components['schemas']['SearchPage']

export function useSavedSearches(enabled=true) {
  return useQuery({queryKey:['saved-searches'], enabled, queryFn:({signal}) => request<SavedSearch[] | null>('/v1/saved-searches',{signal})})
}

function Matches({matches, onOpen}: {matches: Match[]; onOpen: (entry: Entry) => void}) {
  const [error,setError] = useState('')
  const [opening,setOpening] = useState(false)
  async function open(id: number) {
    setOpening(true);setError('')
    try {
      const result = await request<components['schemas']['EntriesPage']>(`/v1/entries?ids=${id}`)
      const entry = result.entries?.find(entry => entry.id === id)
      if (!entry) throw new Error('This article is no longer available.')
      onOpen(entry)
    } catch(error) {setError(error instanceof Error ? error.message : 'Could not open article.')}
    finally {setOpening(false)}
  }
  return <>{(['entry','agent_note'] as const).map(kind => <section key={kind} aria-label={kind === 'entry' ? 'Article matches' : 'Agent note matches'}><h3>{kind === 'entry' ? 'Articles' : 'Agent notes'}</h3><ul>{matches.filter(match => match.kind === kind).map(match => <li key={`${kind}:${match.entry_id}:${match.annotation_id ?? 0}`}>
    <button disabled={opening} onClick={() => {void open(match.entry_id)}}>{match.title || 'Untitled'}</button>
    {kind === 'agent_note' && <p>Agent note · {match.agent_name || 'Unknown agent'}</p>}
    <p className="search-snippet">{match.snippet}</p>
  </li>)}</ul></section>)}{error && <p role="alert">{error}</p>}</>
}

export function Search({saved, onOpen}: {saved?: SavedSearch; onOpen: (entry: Entry) => void}) {
  const cache = useQueryClient()
  const pipeline = usePipeline()
  const [input,setInput] = useState(saved?.query ?? '')
  const [query,setQuery] = useState(saved?.query ?? '')
  const [mode,setMode] = useState(saved?.filters.mode === 'hybrid' ? 'hybrid' : 'fulltext')
  const [name,setName] = useState('')
  const [error,setError] = useState('')
  const [saving,setSaving] = useState(false)
  const [savedMessage,setSavedMessage] = useState('')
  const effectiveMode = pipeline.status.embed === 'enabled' ? mode : 'fulltext'
  const filters = {...saved?.filters,mode:effectiveMode}
  const results = useInfiniteQuery({queryKey:['search',query,filters], enabled:Boolean(query.trim()), initialPageParam:'', queryFn:({pageParam,signal}) => {
    const params = new URLSearchParams({q:query,mode:effectiveMode})
    if (filters.subscription_id) params.set('subscription_id',String(filters.subscription_id))
    if (filters.folder_id) params.set('folder_id',String(filters.folder_id))
    if (filters.limit) params.set('limit',String(filters.limit))
    if (pageParam) params.set('cursor',pageParam)
    return request<Page>(`/v1/search?${params}`,{signal})
  }, getNextPageParam:page => page.matches?.length ? page.next_cursor : undefined})
  const matches = [...new Map((results.data?.pages.flatMap(page => page.matches ?? []) ?? []).map(match => [`${match.kind}:${match.entry_id}:${match.annotation_id ?? 0}`,match])).values()]
  const status = results.data?.pages.at(-1)?.status
  async function save(event: React.FormEvent) {
    event.preventDefault();setSaving(true);setError('');setSavedMessage('')
    try {await request('/v1/saved-searches',{method:'POST',body:JSON.stringify({name,query,filters})});await cache.invalidateQueries({queryKey:['saved-searches']});setName('');setSavedMessage('Search saved in the sidebar.')}
    catch(error) {setError(error instanceof Error ? error.message : 'Could not save search.')}
    finally {setSaving(false)}
  }
  return <main aria-label="Search"><h2>{saved?.name ?? 'Search'}</h2>
    <form onSubmit={event => {event.preventDefault();setQuery(input.trim());setSavedMessage('')}}><label>Search your feeds<input type="search" required value={input} onChange={event => setInput(event.target.value)}/></label>
      {pipeline.status.embed === 'enabled' && <label>Search mode<select value={mode} onChange={event => setMode(event.target.value)}><option value="fulltext">Full text</option><option value="hybrid">Hybrid: full text and meaning</option></select></label>}
      <button>Search</button>
    </form>
    {effectiveMode === 'hybrid' && <p>Hybrid search can make an embedding call and searches a bounded retrieval window. Use Full text to page further.</p>}
    {(error || results.error) && <p role="alert">{error || results.error?.message}</p>}
    {results.isFetching && <p role="status">Searching…</p>}
    {(status === 'disabled' || status === 'paused') && <p role="status">Hybrid search is {status}. <button onClick={() => setMode('fulltext')}>Use full-text search</button></p>}
    {results.isSuccess && !status && !matches.length && <p>No matches.</p>}
    {results.isSuccess && <Matches matches={matches} onOpen={onOpen}/>}
    {results.hasNextPage && <button disabled={results.isFetchingNextPage} onClick={() => {void results.fetchNextPage()}}>More search results</button>}
    {query && <form onSubmit={event => {void save(event)}}><label>Saved search name<input required value={name} onChange={event => setName(event.target.value)}/></label><button disabled={saving}>Save search as a view</button></form>}
    {savedMessage && <p role="status">{savedMessage}</p>}
  </main>
}

export function MoreLikeThis({entryID,onOpen}: {entryID: number; onOpen: (entry: Entry) => void}) {
  const pipeline = usePipeline()
  const [requested,setRequested] = useState(false)
  const similar = useQuery({queryKey:['similar',entryID], enabled:requested && pipeline.status.embed === 'enabled', queryFn:({signal}) => request<Page>(`/v1/entries/${entryID}/similar`,{signal})})
  if (pipeline.status.embed !== 'enabled') return null
  return <section aria-label="Similar articles"><button onClick={() => setRequested(true)}>More like this</button>
    {requested && similar.isPending && <p role="status">Finding similar articles…</p>}
    {similar.error && <p role="alert">{similar.error.message}</p>}
    {requested && similar.data?.status && <p role="status">Similar articles are {similar.data.status}.</p>}
    {requested && similar.isSuccess && !similar.data.status && <><Matches matches={similar.data.matches ?? []} onOpen={onOpen}/>{!similar.data.matches?.length && <p>No similar articles yet.</p>}</>}
  </section>
}
