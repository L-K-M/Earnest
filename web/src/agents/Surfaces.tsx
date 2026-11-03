import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry} from '../reader'
import {request, RequestError} from '../request'

type Activity = components['schemas']['Activity']
type Annotation = components['schemas']['Annotation']
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

function EntryLink({id, onOpen}: {id:number; onOpen:(entry:Entry)=>void}) {
  const entry = useQuery({queryKey:['entry', id], queryFn:({signal})=>request<Entry>(`/v1/entries/${id}`, {signal})})
  if (entry.error) return <li>Article {id} could not be loaded: {entry.error.message}</li>
  return <li><button disabled={!entry.data} onClick={()=>{if(entry.data) onOpen(entry.data)}}>{entry.data?.title || `Article ${id}`}</button></li>
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

export function TodayAgents({since, onOpen}: {since:string; onOpen:(entry:Entry)=>void}) {
  const briefs = useQuery({queryKey:['briefs'],queryFn:({signal})=>request<Annotation[] | null>('/v1/briefs/today',{signal})})
  const activity = useActivities('action=annotation.updated')
  const changes = useQuery({queryKey:['agent-changes', since], queryFn:({signal})=>request<Activity[] | null>(`/v1/activity/agent-changes?since=${encodeURIComponent(since)}`,{signal})})
  return <><div className="agent-today">{(briefs.error || activity.error || changes.error) && <p role="alert">{(briefs.error || activity.error || changes.error)?.message}</p>}{(Array.isArray(briefs.data) ? briefs.data : []).map(brief=>{
    const row = activity.data?.slice().reverse().find(item=>targetID(item.target) === brief.id && typeof item.after === 'object' && item.after !== null && 'rev' in item.after && item.after.rev === brief.rev)
    return <Brief key={brief.id} brief={brief} row={row} onOpen={onOpen} />
  })}{!!changes.data?.length && <details><summary>While you were out</summary><ActivityRows rows={changes.data}/></details>}</div></>
}
function Brief({brief,row,onOpen}: {brief:Annotation;row?:Activity;onOpen:(entry:Entry)=>void}) {
  const key = `earnest.brief.${brief.id}`
  const [open,setOpen] = useState(()=>localStorage.getItem(key) !== 'closed')
  return <details open={open} onToggle={event=>{const value=event.currentTarget.open;setOpen(value);localStorage.setItem(key,value ? 'open' : 'closed')}}><summary>{row?.principal_name ?? `Agent ${brief.principal_id}`} {row ? `· ${new Date(row.created_at).toLocaleString()}` : '· Time unavailable'}</summary><p style={{whiteSpace:'pre-wrap'}}>{brief.value}</p><ul>{brief.entry_ids?.map(id=><EntryLink key={id} id={id} onOpen={onOpen}/>)}</ul></details>
}
export function SessionChanges({since}: {since:string}) {
  const changes = useQuery({queryKey:['agent-changes', since], queryFn:({signal})=>request<Activity[] | null>(`/v1/activity/agent-changes?since=${encodeURIComponent(since)}`,{signal}), refetchInterval:30000})
  return <>{changes.error && <p role="alert">Agent changes: {changes.error.message}</p>}{!!changes.data?.length && <><p>Agent changes this session:</p><ActivityRows rows={changes.data}/></>}</>
}

export function ActivityScreen() {
  const [principal,setPrincipal] = useState(''); const [action,setAction] = useState(''); const [since,setSince] = useState('')
  const params = new URLSearchParams(); if(principal) params.set('principal_id',principal); if(action) params.set('action',action); if(since) params.set('since',new Date(since).toISOString())
  const activity = useActivities(params.toString())
  return <main><h2>Activity</h2><label>Principal ID <input type="number" min="1" value={principal} onChange={e=>setPrincipal(e.target.value)}/></label><label>Action <input value={action} onChange={e=>setAction(e.target.value)}/></label><label>Since <input type="date" value={since} onChange={e=>setSince(e.target.value)}/></label>{activity.error && <p role="alert">{activity.error.message}</p>}{activity.isPending ? <p>Loading activity…</p> : <ActivityRows rows={activity.data ?? []}/>}</main>
}

export function Notifications({onOpen}: {onOpen:(entry:Entry)=>void}) {
  const client = useQueryClient(); const [selected,setSelected] = useState<number | null>(null); const [error,setError] = useState(''); const [pending,setPending] = useState(false)
  const list = useQuery({queryKey:['notifications'], queryFn:async ({signal})=>{const rows:Notification[]=[];let before=0;let more=true;while(more) {const page=await request<Notification[] | null>(`/v1/notifications?limit=1000${before ? `&before_id=${before}` : ''}`,{signal});const batch=page ?? [];rows.push(...batch);more=batch.length===1000;if(more) before=Math.min(...batch.map(n=>n.id))}return rows}})
  const detail = useQuery({queryKey:['notification',selected],enabled:selected !== null,queryFn:({signal})=>request<Notification>(`/v1/notifications/${selected}`,{signal})})
  const n = detail.data
  async function run(path:string, options:RequestInit) {setPending(true);setError('');try {await request(path,options);await Promise.all(['notifications','notification','mutes','activity'].map(key=>client.invalidateQueries({queryKey:[key]})))} catch(e){setError(message(e))} finally{setPending(false)}}
  async function release() {setPending(true);setError('');try {const cursor=await request<{generation:string}>('/v1/bootstrap');await request('/v1/notifications:release',{method:'POST',body:JSON.stringify({generation:cursor.generation,op_id:crypto.randomUUID()})});await client.invalidateQueries({queryKey:['notifications']})} catch(e){setError(message(e))} finally {setPending(false)}}
  return <main><h2>Notifications</h2>{(error || list.error || detail.error) && <p role="alert">{error || list.error?.message || detail.error?.message}</p>}<p>After a restore, delivery is withheld until you release the current database generation. Notifications restored from an older generation remain withheld.</p><button disabled={pending} onClick={()=>{void release()}}>Release current notifications</button><ul>{list.data?.map(item=><li key={item.id}><button onClick={()=>setSelected(item.id)}>{item.why.sender} · {item.category} · {item.status} · {new Date(item.created_at).toLocaleString()}</button></li>)}</ul>{n && <section><h3>Notification {n.id}</h3><h4>Why you got this</h4><p>Sender: {n.why.sender}. Budget: {n.why.budget.used_today} of {n.why.budget.limit_today} today. {n.why.matching_rule_id ? `Matching rule: ${n.why.matching_rule_id}.` : 'No matching rule.'}</p><h4>The agent says:</h4><p style={{whiteSpace:'pre-wrap'}}>{n.note}</p><h4>Entries</h4><ul>{n.entry_ids?.map(id=><EntryLink key={id} id={id} onOpen={onOpen}/>)}</ul>{n.status === 'delivered' && <p>Delivered notifications can't be recalled.</p>}{n.status === 'held' && <button disabled={pending} onClick={()=>{void run(`/v1/notifications/${n.id}:cancel`,{method:'POST',body:op()})}}>Cancel notification</button>}<button disabled={pending} onClick={()=>{void run(`/v1/agents/${n.principal_id}/mute`,{method:'PUT',body:op()})}}>Mute {n.why.sender}</button><button disabled={pending} onClick={()=>{void run(`/v1/agents/${n.principal_id}/mute?op_id=${encodeURIComponent(crypto.randomUUID())}`,{method:'DELETE'})}}>Unmute {n.why.sender}</button></section>}</main>
}
