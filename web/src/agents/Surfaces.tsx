import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry} from '../reader'
import {request, RequestError} from '../request'
import type {ActivityFilters} from '../routes'

type Activity = components['schemas']['Activity']
type Annotation = components['schemas']['Annotation']
type AgentNotificationStatus = components['schemas']['AgentNotificationStatus']
type Notification = components['schemas']['Notification']
const op = () => JSON.stringify({op_id: crypto.randomUUID()})
const message = (error: unknown) => error instanceof Error ? error.message : 'Something went wrong.'
const targetID = (target: unknown): number | undefined => typeof target === 'object' && target !== null && 'id' in target && typeof target.id === 'number' ? target.id : undefined

function useActivities(query = '') {
  return useQuery({queryKey:['activity', query], queryFn: async ({signal}) => {
    const rows: Activity[] = []
    let after = 0
    let more = true
    while (more) {
      const params = new URLSearchParams(query); params.set('limit', '1000'); params.set('after_id', String(after))
      const page = await request<components['schemas']['ActivityPage']>(`/v1/activity?${params}`, {signal})
      const batch = page.activities ?? []; rows.push(...batch)
      more = batch.length === 1000
      if (more) after = batch[batch.length - 1].id
    }
    return rows
  }})
}

// One request per 200 IDs instead of one per activity or notification entry.
function useEntries(ids: number[]) {
  const unique = [...new Set(ids)].filter(id => id > 0).sort((a, b) => a - b)
  return useQuery({
    queryKey: ['entries', unique],
    enabled: unique.length > 0,
    queryFn: async ({signal}) => {
      const map = new Map<number, Entry>()
      for (let start = 0; start < unique.length; start += 200) {
        const page = await request<{entries?: Entry[]}>(`/v1/entries?ids=${unique.slice(start, start + 200).join(',')}`, {signal})
        for (const entry of page.entries ?? []) map.set(entry.id, entry)
      }
      return map
    },
  })
}

function EntryLink({entry, onOpen}: {entry: Entry; onOpen: (entry: Entry) => void}) {
  return <li><button onClick={() => onOpen(entry)}>{entry.title || `Article ${entry.id}`}</button></li>
}

// A missing entry is only "unavailable" after the batch settles; while it is
// still fetching, show a neutral placeholder instead.
function EntryItems({ids, entries, loading, onOpen}: {ids: number[]; entries: Map<number, Entry>; loading: boolean; onOpen: (entry: Entry) => void}) {
  return <>{ids.map(id => {
    const entry = entries.get(id)
    if (entry) return <EntryLink key={id} entry={entry} onOpen={onOpen}/>
    return <li key={id}>Article {id}{loading ? '…' : ' is unavailable.'}</li>
  })}</>
}

function describe(row:Activity): string {
  if (row.action !== 'state.updated') return `${row.action.replaceAll('.', ' ')} · ${JSON.stringify(row.target)}`
  const before = row.before as Partial<Entry['state']> | null
  const after = row.after as Partial<Entry['state']> | null
  if (!before || !after) return 'Changed an article'
  const changes = (['seen','shelf','kept'] as const).filter(key=>before[key]?.value !== after[key]?.value).map(key=>`${key}: ${String(before[key]?.value)} to ${String(after[key]?.value)}`)
  return `Article ${after.entry_id ?? 'unknown'}: ${changes.join(', ') || 'state changed'}`
}

function ActivityRows({rows}: {rows:Activity[]}) {
  const client = useQueryClient()
  const [error,setError] = useState('')
  const [pending,setPending] = useState<number | null>(null)
  async function act(row:Activity) {
    setPending(row.id); setError('')
    try {
      const id = row.next_action === 'cancel' ? targetID(row.target) : row.id
      if (id === undefined) throw new Error('Notification ID is unavailable.')
      await request(row.next_action === 'cancel' ? `/v1/notifications/${id}:cancel` : `/v1/activity/${id}:revert`, {method:'POST',body:op()})
      await Promise.all(['activity','agent-changes','notifications'].map(key=>client.invalidateQueries({queryKey:[key]})))
    } catch (error) { setError(error instanceof RequestError && error.status === 409 ? 'You changed it since this action. It could not be reverted or cancelled.' : message(error)) }
    finally {setPending(null)}
  }
  const groups = new Map<string, Activity[]>()
  for (const row of rows) {const key = `${row.principal_id}:${row.principal_name}`; groups.set(key, [...(groups.get(key) ?? []), row])}
  return <>{error && <p role="alert">{error}</p>}{[...groups].map(([key, items])=><section key={key}><h3>{items[0].principal_name}</h3><ul>{items.map(row=><li key={row.id}><span>{describe(row)} · {new Date(row.created_at).toLocaleString()}</span>{row.next_action === 'revert' || row.next_action === 'cancel' ? <button disabled={pending !== null} onClick={()=>{void act(row)}}>{row.next_action === 'revert' ? 'Revert' : 'Cancel'}</button> : <span> · Nothing to do{row.reason ? `: ${row.reason}` : ''}</span>}{row.action === 'notification.created' && row.reason === 'notification is delivered' && <p>Delivered notifications can't be recalled.</p>}</li>)}</ul></section>)}</>
}

async function agentChanges(since:string, signal:AbortSignal): Promise<Activity[]> {
  const all: Activity[] = []
  for (;;) {
    const page = await request<Activity[] | null>(`/v1/activity/agent-changes?since=${encodeURIComponent(since)}&after_id=${all.at(-1)?.id ?? 0}&limit=1000`, {signal}) ?? []
    all.push(...page)
    if (page.length < 1000) return all
  }
}

export function TodayAgents({since, onOpen}: {since:string; onOpen:(entry:Entry)=>void}) {
  const briefs = useQuery({queryKey:['briefs'],queryFn:({signal})=>request<Annotation[] | null>('/v1/briefs/today',{signal})})
  const changes = useQuery({queryKey:['agent-changes', since], queryFn:({signal})=>agentChanges(since,signal)})
  const briefList = Array.isArray(briefs.data) ? briefs.data : []
  const entries = useEntries(briefList.flatMap(brief => brief.entry_ids ?? []))
  return <><div className="agent-today">{(briefs.error || changes.error || entries.error) && <p role="alert">{(briefs.error || changes.error || entries.error)?.message}</p>}{briefList.map(brief=><Brief key={brief.id} brief={brief} entries={entries.data ?? new Map()} entriesLoading={entries.isLoading} onOpen={onOpen}/>)}{!!changes.data?.length && <details><summary>While you were out</summary><ActivityRows rows={changes.data}/></details>}</div></>
}
function Brief({brief,entries,entriesLoading,onOpen}: {brief:Annotation; entries: Map<number, Entry>; entriesLoading: boolean; onOpen:(entry:Entry)=>void}) {
  const key = `earnest.brief.${brief.id}`
  const [open,setOpen] = useState(()=>localStorage.getItem(key) !== 'closed')
  return <details open={open} onToggle={event=>{const value=event.currentTarget.open;setOpen(value);localStorage.setItem(key,value ? 'open' : 'closed')}}><summary>{brief.author_name} · {new Date(brief.created_at).toLocaleString()}</summary><p style={{whiteSpace:'pre-wrap'}}>{brief.value}</p><ul><EntryItems ids={brief.entry_ids ?? []} entries={entries} loading={entriesLoading} onOpen={onOpen}/></ul></details>
}
export function SessionChanges({since}: {since:string}) {
  const changes = useQuery({queryKey:['agent-changes', since], queryFn:({signal})=>agentChanges(since,signal), refetchInterval:30000})
  return <>{changes.error && <p role="alert">Agent changes: {changes.error.message}</p>}{!!changes.data?.length && <><p>Agent changes this session:</p><ActivityRows rows={changes.data}/></>}</>
}

// Filters live in the URL when the shell passes them, so a filtered view can
// be bookmarked and survives a refresh; standalone, they are local state.
export function ActivityScreen({filters, onFilters}: {filters?: ActivityFilters; onFilters?: (filters: ActivityFilters) => void} = {}) {
  const [local,setLocal] = useState<ActivityFilters>({})
  const current = filters ?? local
  const update = (change: ActivityFilters) => { const next = {...current, ...change}; for (const key of ['agent','action','since'] as const) if (!next[key]) delete next[key]; if (onFilters) onFilters(next); else setLocal(next) }
  const principal = current.agent ?? ''; const action = current.action ?? ''; const since = current.since ?? ''
  const setPrincipal = (agent: string) => update({agent}); const setAction = (value: string) => update({action:value}); const setSince = (value: string) => update({since:value})
  const params = new URLSearchParams(); if(principal) params.set('principal_id',principal); if(action) params.set('action',action); if(since && !Number.isNaN(Date.parse(since))) params.set('since',new Date(since).toISOString())
  const activity = useActivities(params.toString())
  const agents = useQuery({queryKey:['agents'], queryFn:({signal})=>request<AgentNotificationStatus[] | null>('/v1/agents',{signal})})
  const filtered = Boolean(principal || action || since)
  return <section className="activity" aria-labelledby="activity-title"><div className="page-header"><h2 id="activity-title">Activity</h2><p className="muted">Every change an agent or rule made, and what you can still undo.</p></div><section><h3>Agents</h3>{agents.error && <p role="alert">{agents.error.message}</p>}{agents.isPending ? <p>Loading agents…</p> : <ul>{agents.data?.map(agent=><li key={agent.principal_id}>{agent.name} · {agent.muted ? 'Muted' : 'Unmuted'} · {agent.used_today} notifications today</li>)}</ul>}</section><label>Agent <select value={principal} onChange={e=>setPrincipal(e.target.value)}><option value="">All agents</option>{(agents.data ?? []).map(agent=><option key={agent.principal_id} value={String(agent.principal_id)}>{agent.name}</option>)}</select></label><label>Action <input value={action} onChange={e=>setAction(e.target.value)}/></label><label>Since <input type="date" value={since} onChange={e=>setSince(e.target.value)}/></label>{activity.error && <p role="alert">{activity.error.message}</p>}{activity.isPending ? <p>Loading activity…</p> : <>{!activity.data?.length ? <p>{filtered ? 'No activity matches these filters.' : 'No activity yet.'}</p> : <ActivityRows rows={activity.data ?? []}/>}</>}</section>
}

export function Notifications({onOpen, onOpenSettings, selectedID, onSelect}: {onOpen:(entry:Entry)=>void; onOpenSettings:()=>void; selectedID?: number; onSelect?: (id: number) => void}) {
  const client = useQueryClient(); const [localSelected,setLocalSelected] = useState<number | null>(null)
  // The open notification is part of the URL when the shell routes it.
  const selected = onSelect ? selectedID ?? null : localSelected
  const setSelected = (id: number) => { if (onSelect) onSelect(id); else setLocalSelected(id) }; const [error,setError] = useState(''); const [pending,setPending] = useState(false)
  const list = useQuery({queryKey:['notifications'], queryFn:async ({signal})=>{const rows:Notification[]=[];let before=0;let more=true;while(more) {const page=await request<Notification[] | null>(`/v1/notifications?limit=1000${before ? `&before_id=${before}` : ''}`,{signal});const batch=page ?? [];rows.push(...batch);more=batch.length===1000;if(more) before=Math.min(...batch.map(n=>n.id))}return rows}})
  const detail = useQuery({queryKey:['notification',selected],enabled:selected !== null,queryFn:({signal})=>request<Notification>(`/v1/notifications/${selected}`,{signal})})
  const n = detail.data
  const entries = useEntries(n?.entry_ids ?? [])
  async function run(path:string, options:RequestInit) {setPending(true);setError('');try {await request(path,options);await Promise.all(['notifications','notification','agents','activity'].map(key=>client.invalidateQueries({queryKey:[key]})))} catch(e){setError(message(e))} finally{setPending(false)}}
  async function release() {setPending(true);setError('');try {const cursor=await request<{generation:string}>('/v1/bootstrap');await request('/v1/notifications:release',{method:'POST',body:JSON.stringify({generation:cursor.generation,op_id:crypto.randomUUID()})});await client.invalidateQueries({queryKey:['notifications']})} catch(e){setError(message(e))} finally {setPending(false)}}
  return <section className="notifications" aria-labelledby="notifications-title"><div className="page-header"><h2 id="notifications-title">Notifications</h2></div>{(error || list.error || detail.error || entries.error) && <p role="alert">{error || list.error?.message || detail.error?.message || entries.error?.message}</p>}<p>After a restore, delivery is withheld until you release the current database generation. Notifications restored from an older generation remain withheld.</p><button disabled={pending} onClick={()=>{void release()}}>Release current notifications</button>{list.isSuccess && !list.data.length && <p className="empty-state">No notifications. Agents and rules that notify you will appear here, each with the reason it was sent.</p>}<ul className="notification-list">{list.data?.map(item=><li key={item.id}><button aria-current={selected === item.id ? 'true' : undefined} onClick={()=>setSelected(item.id)}>{item.why.sender} · {item.category} · {item.status} · {new Date(item.created_at).toLocaleString()}</button></li>)}</ul>{n && <section><h3>Notification {n.id}</h3><h4>Why you got this</h4><p>Sender: {n.why.sender}. Budget: {n.why.budget.used_today} of {n.why.budget.limit_today} today. {n.why.matching_rule_id ? `Matching rule: ${n.why.matching_rule_id}.` : 'No matching rule.'}</p><h4>The agent says:</h4><p style={{whiteSpace:'pre-wrap'}}>{n.note}</p><h4>Entries</h4><ul><EntryItems ids={n.entry_ids ?? []} entries={entries.data ?? new Map()} loading={entries.isLoading} onOpen={onOpen}/></ul>{n.status === 'failed' && <p>No push destination accepted this notification. <button onClick={onOpenSettings}>Check push settings</button></p>}{n.status === 'delivered' && <p>Delivered notifications can't be recalled.</p>}{n.status === 'held' && <button disabled={pending} onClick={()=>{void run(`/v1/notifications/${n.id}:cancel`,{method:'POST',body:op()})}}>Cancel notification</button>}{n.muted ? <button disabled={pending} onClick={()=>{void run(`/v1/agents/${n.principal_id}/mute?op_id=${encodeURIComponent(crypto.randomUUID())}`,{method:'DELETE'})}}>Unmute {n.why.sender}</button> : <button disabled={pending} onClick={()=>{void run(`/v1/agents/${n.principal_id}/mute`,{method:'PUT',body:op()})}}>Mute {n.why.sender}</button>}</section>}</section>
}
