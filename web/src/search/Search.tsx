import {useEffect,useRef,useState} from 'react'
import {useInfiniteQuery, useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry, Subscription} from '../reader'
import {request} from '../request'
import {usePipeline} from './pipeline'
import {searchHref, type SearchRoute} from '../routes'

export type SavedSearch = components['schemas']['SavedSearch']
type Match = components['schemas']['SearchMatch']
type Page = components['schemas']['SearchPage']
const LIMIT_PRESETS: readonly number[] = [20,50,100,200]

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

// Search takes its query and filters from `route` when one is given, and
// reports every change through onRoute so the address bar, Back and Forward
// and a refresh all show the same search. A saved search keeps its own
// stored query and filters instead.
export function Search({saved, route, onRoute, onDeleted, onOpen, subscriptions = [], folders = []}: {saved?: SavedSearch; route?: SearchRoute; onRoute?: (search: SearchRoute, replace: boolean) => void; onDeleted?: () => void; onOpen: (entry: Entry) => void; subscriptions?: Subscription[]; folders?: components['schemas']['Folder'][]}) {
  const cache = useQueryClient()
  const pipeline = usePipeline()
  const field = useRef<HTMLInputElement>(null)
  const initial: SearchRoute = route ?? {q:saved?.query ?? '', ...(saved?.filters.mode === 'hybrid' ? {mode:'hybrid' as const} : {}), feed:saved?.filters.subscription_id, folder:saved?.filters.folder_id, limit:saved?.filters.limit}
  const [input,setInput] = useState(initial.q)
  const [query,setQuery] = useState(initial.q)
  const [mode,setMode] = useState(initial.mode === 'hybrid' ? 'hybrid' : 'fulltext')
  const [subscriptionID,setSubscriptionID] = useState(initial.feed ?? 0)
  const [folderID,setFolderID] = useState(initial.folder ?? 0)
  const [limit,setLimit] = useState(initial.limit ?? 0)
  // Back and Forward change the route under a mounted search; adopt it.
  const routeKey = route ? searchHref(route) : ''
  const [adopted,setAdopted] = useState(routeKey)
  if (route && routeKey !== adopted) {
    setAdopted(routeKey);setInput(route.q);setQuery(route.q);setMode(route.mode === 'hybrid' ? 'hybrid' : 'fulltext')
    setSubscriptionID(route.feed ?? 0);setFolderID(route.folder ?? 0);setLimit(route.limit ?? 0)
  }
  function publish(change: {q?: string; mode?: string; feed?: number; folder?: number; limit?: number}, replace: boolean) {
    const next = {q:change.q ?? query, mode:change.mode ?? mode, feed:change.feed ?? subscriptionID, folder:change.folder ?? folderID, limit:change.limit ?? limit}
    onRoute?.({q:next.q, ...(next.mode === 'hybrid' ? {mode:'hybrid' as const} : {}), ...(next.feed ? {feed:next.feed} : {}), ...(next.folder ? {folder:next.folder} : {}), ...(next.limit ? {limit:next.limit} : {})}, replace)
  }
  // Arriving with nothing to search for puts the cursor in the field.
  useEffect(() => { if (!initial.q) field.current?.focus() }, [initial.q])
  const [name,setName] = useState('')
  const [renameName,setRenameName] = useState(saved?.name ?? '')
  const [savedTitle,setSavedTitle] = useState(saved?.name ?? '')
  const [removed,setRemoved] = useState(false)
  const [renaming,setRenaming] = useState(false)
  const [renameError,setRenameError] = useState('')
  const [renameMessage,setRenameMessage] = useState('')
  const [error,setError] = useState('')
  const [saving,setSaving] = useState(false)
  const [savedMessage,setSavedMessage] = useState('')
  useEffect(() => {setRenameName(saved?.name ?? '');setSavedTitle(saved?.name ?? '');setRemoved(false);setRenameError('');setRenameMessage('')},[saved?.id,saved?.name])
  const activeSaved = saved && !removed
  const effectiveMode = pipeline.status.embed === 'enabled' ? mode : 'fulltext'
  const filters = {...saved?.filters,mode:effectiveMode}
  if (subscriptionID) filters.subscription_id = subscriptionID
  else delete filters.subscription_id
  if (folderID) filters.folder_id = folderID
  else delete filters.folder_id
  if (limit) filters.limit = limit
  else delete filters.limit
  const limitChoices = limit > 0 && !LIMIT_PRESETS.includes(limit) ? [limit, ...LIMIT_PRESETS] : [...LIMIT_PRESETS]
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
  const unavailable = status === 'disabled' || status === 'paused'
  async function save(event: React.FormEvent) {
    event.preventDefault();setSaving(true);setError('');setSavedMessage('')
    try {await request('/v1/saved-searches',{method:'POST',body:JSON.stringify({name,query,filters})});await cache.invalidateQueries({queryKey:['saved-searches']});setName('');setSavedMessage('Search saved in the sidebar.')}
    catch(error) {setError(error instanceof Error ? error.message : 'Could not save search.')}
    finally {setSaving(false)}
  }
  async function remove() {
    if (!saved || removed) return
    setSaving(true);setError('');setSavedMessage('')
    try {await request(`/v1/saved-searches/${encodeURIComponent(saved.id)}`,{method:'DELETE'});setRemoved(true);setRenameError('');setRenameMessage('');setSavedMessage('Saved search deleted. It is gone from the sidebar.');await cache.invalidateQueries({queryKey:['saved-searches']});onDeleted?.()}
    catch(error) {setError(error instanceof Error ? error.message : 'Could not delete the saved search.')}
    finally {setSaving(false)}
  }
  async function rename(event: React.FormEvent) {
    event.preventDefault()
    if (!saved || removed) return
    const next = renameName.trim()
    if (!next) {setRenameError('Enter a name for the saved search.');setRenameMessage('');return}
    setRenaming(true);setRenameError('');setRenameMessage('')
    try {
      await request(`/v1/saved-searches/${encodeURIComponent(saved.id)}`,{method:'PATCH',body:JSON.stringify({name:next})})
      await cache.invalidateQueries({queryKey:['saved-searches']})
      setSavedTitle(next);setRenameName(next);setRenameMessage('Saved search renamed.')
    } catch(error) {setRenameError(error instanceof Error ? error.message : 'Could not rename the saved search.')}
    finally {setRenaming(false)}
  }
  return <section className="search" aria-label="Search"><div className="page-header"><h2>{activeSaved ? savedTitle : 'Search'}</h2></div>
    {activeSaved && <p><button disabled={saving || renaming} onClick={() => {if (window.confirm(`Delete the saved search “${savedTitle}”?`)) void remove()}}>Delete “{savedTitle}”</button></p>}
    {activeSaved && <form onSubmit={event => {void rename(event)}}><label>Rename saved search<input value={renameName} onChange={event => {setRenameName(event.target.value);setRenameError('');setRenameMessage('')}}/></label><button disabled={renaming || saving}>{renaming ? 'Renaming…' : 'Rename'}</button></form>}
    {activeSaved && renameError && <p role="alert">{renameError}</p>}
    {activeSaved && renameMessage && <p role="status">{renameMessage}</p>}
    <form className="search-form" role="search" onSubmit={event => {event.preventDefault();setQuery(input.trim());setSavedMessage('');publish({q:input.trim()},false)}}>
      <div className="search-row"><label className="grow">Search your feeds<input ref={field} type="search" enterKeyHint="search" autoComplete="off" required value={input} onChange={event => setInput(event.target.value)}/></label><button className="primary">Search</button></div>
      <div className="filter-row">
      {pipeline.status.embed === 'enabled' && <label>Search mode<select value={mode} onChange={event => {setMode(event.target.value);publish({mode:event.target.value},true)}}><option value="fulltext">Full text</option><option value="hybrid">Hybrid: full text and meaning</option></select></label>}
      <label>Feed<select value={subscriptionID} onChange={event => {setSubscriptionID(Number(event.target.value));publish({feed:Number(event.target.value)},true)}}><option value={0}>All feeds</option>{subscriptions.filter(sub => !sub.deleted).map(sub => <option key={sub.id} value={sub.id}>{sub.title || sub.url}</option>)}</select></label>
      <label>Folder<select value={folderID} onChange={event => {setFolderID(Number(event.target.value));publish({folder:Number(event.target.value)},true)}}><option value={0}>All folders</option>{folders.filter(folder => !folder.deleted).map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label>
      <label>Results per page<select value={limit} onChange={event => {setLimit(Number(event.target.value));publish({limit:Number(event.target.value)},true)}}><option value={0}>Server default</option>{limitChoices.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
      </div>
    </form>
    {pipeline.status.embed === 'paused' && <p role="status">Hybrid search is paused. Full-text search is available.</p>}
    {pipeline.error && <p role="alert">Could not check whether AI search features are available.</p>}
    {effectiveMode === 'hybrid' && <p>Hybrid search can make an embedding call and searches a bounded retrieval window. Use Full text to page further.</p>}
    {(error || results.error) && <p role="alert">{error || results.error?.message}</p>}
    {results.isFetching && <p role="status">Searching…</p>}
    {(status === 'disabled' || status === 'paused') && <p role="status">Hybrid search is {status}. <button onClick={() => {setMode('fulltext');publish({mode:'fulltext'},true)}}>Use full-text search</button></p>}
    {results.isSuccess && !unavailable && !matches.length && <p>No matches.</p>}
    {results.isSuccess && <Matches matches={matches} onOpen={onOpen}/>}
    {results.hasNextPage && <button disabled={results.isFetchingNextPage} onClick={() => {void results.fetchNextPage()}}>More search results</button>}
    {query && <form onSubmit={event => {void save(event)}}><label>Saved search name<input required value={name} onChange={event => setName(event.target.value)}/></label><button disabled={saving}>Save search as a view</button></form>}
    {savedMessage && <p role="status">{savedMessage}</p>}
  </section>
}

export function MoreLikeThis({entryID,onOpen}: {entryID: number; onOpen: (entry: Entry) => void}) {
  const pipeline = usePipeline()
  const [requested,setRequested] = useState(false)
  const similar = useQuery({queryKey:['similar',entryID], enabled:requested && pipeline.status.embed !== 'disabled', queryFn:({signal}) => request<Page>(`/v1/entries/${entryID}/similar`,{signal})})
  const unavailable = similar.data?.status === 'disabled' || similar.data?.status === 'paused'
  if (pipeline.status.embed === 'disabled') return null
  return <section aria-label="Similar articles"><button onClick={() => setRequested(true)}>More like this</button>
    {requested && similar.isPending && <p role="status">Finding similar articles…</p>}
    {similar.error && <p role="alert">{similar.error.message}</p>}
    {requested && unavailable && <p role="status">Similar articles are {similar.data?.status}.</p>}
    {requested && similar.isSuccess && !unavailable && <><Matches matches={similar.data.matches ?? []} onOpen={onOpen}/>{!similar.data.matches?.length && <p>No similar articles yet.</p>}</>}
  </section>
}
