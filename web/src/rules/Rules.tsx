import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry, Subscription} from '../reader'
import {request} from '../request'
import {usePipeline} from '../search/pipeline'
import {PredicateEditor} from './PredicateEditor'
import {actionRestriction, predicateError, type Action} from './model'

type Rule = components['schemas']['Rule']
type Body = components['schemas']['RuleBody']
type Resources = {feeds: Subscription[]; folders: components['schemas']['Folder'][]; labels: components['schemas']['Label'][]}
const initial: Body = {name:'', enabled:true, predicate:{type:'feed_in', ids:[]}, actions:[{type:'notify', budget:3}]}
const actionTypes = [['add_label','Add label'], ['set_shelf','Set shelf'], ['notify','Notify'], ['webhook','Webhook']] as const
const message = (error: unknown) => error instanceof Error ? error.message : 'The rule could not be saved.'

export function Rules(resources: Resources) {
  const cache = useQueryClient()
  const pipeline = usePipeline()
  const list = useQuery({queryKey:['rules'], queryFn:({signal}) => request<Rule[] | null>('/v1/rules',{signal})})
  // Watch IDs keep the ordinary editor from changing a watch's notify-only action.
  const watches = useQuery({queryKey:['watch-rules'], queryFn:({signal}) => request<components['schemas']['WatchRule'][] | null>('/v1/watch-rules',{signal})})
  const [editing, setEditing] = useState<Rule | 'new' | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function mutate(path: string, method: string) {
    setBusy(true);setError('')
    try { await request(path,{method});await cache.invalidateQueries({queryKey:['rules']});await cache.invalidateQueries({queryKey:['watch-rules']}) }
    catch (error) { setError(message(error)) }
    finally { setBusy(false) }
  }
  const watchIDs = new Set(watches.data?.map(w => w.id))
  return <main><h2>Rules</h2>
    {(error || list.error || watches.error || pipeline.error) && <p role="alert">{error || list.error?.message || watches.error?.message || pipeline.error?.message}</p>}
    {(list.isPending || watches.isPending) && <p role="status">Loading rules…</p>}
    <button onClick={() => setEditing('new')}>New rule</button>
    <ul>{(watches.isSuccess ? list.data ?? [] : []).filter(rule => !watchIDs.has(rule.id)).map(rule => <li key={rule.id}><strong>{rule.name}</strong><p>{rule.enabled ? 'Enabled' : 'Disabled'} · {rule.match_count} matches · Last match: {rule.last_match_at ? new Date(rule.last_match_at).toLocaleString() : 'Never'}</p>{rule.disable_reason && <p>{rule.disable_reason}</p>}<button disabled={busy} onClick={() => setEditing(rule)}>Edit {rule.name}</button><button disabled={busy} onClick={() => {void mutate(`/v1/rules/${rule.id}:${rule.enabled ? 'disable' : 'enable'}`,'POST')}}>{rule.enabled ? 'Disable' : 'Enable'} {rule.name}</button><button disabled={busy} onClick={() => {void mutate(`/v1/rules/${rule.id}`,'DELETE')}}>Delete {rule.name}</button></li>)}</ul>
    {editing && <RuleEditor key={editing === 'new' ? 'new' : editing.id} {...resources} rule={editing === 'new' ? undefined : editing} pipeline={pipeline.status} onCancel={() => setEditing(null)} onSaved={() => {setEditing(null);void cache.invalidateQueries({queryKey:['rules']})}}/>}
    {pipeline.status.watch === 'enabled' && <Watches watches={watches.data ?? []} onChanged={() => {void cache.invalidateQueries({queryKey:['watch-rules']});void cache.invalidateQueries({queryKey:['rules']})}}/>}
  </main>
}

function RuleEditor({rule, feeds, folders, labels, pipeline, onCancel, onSaved}: Resources & {rule?: Rule; pipeline: components['schemas']['PipelineStatus']; onCancel: () => void; onSaved: () => void}) {
  const [draft, setDraft] = useState<Body>(() => rule ? {name:rule.name,predicate:rule.predicate,actions:rule.actions,enabled:rule.enabled} : initial)
  const [preview, setPreview] = useState<{body: string; entries: Entry[]} | null>(null)
  const [review, setReview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const webhooks = useQuery({queryKey:['webhooks'], queryFn:({signal}) => request<components['schemas']['Webhook'][] | null>('/v1/webhooks',{signal})})
  const actions = draft.actions ?? []
  const invalid = predicateError(draft.predicate,pipeline) || (!actions.length ? 'Choose at least one action.' : '') || actions.map(action => actionRestriction(draft.predicate,action.type) || (action.type === 'add_label' && !action.label_id ? 'Choose a native label.' : action.type === 'webhook' && !action.webhook_id ? 'Choose a webhook destination.' : action.type === 'notify' && (!Number.isSafeInteger(action.budget) || (action.budget ?? 0) <= 0) ? 'Enter a positive daily notification budget.' : '')).find(Boolean)
  const serialized = JSON.stringify(draft)
  const reviewed = preview?.body === serialized
  function changeAction(index: number, action: Action) {setDraft({...draft,actions:actions.map((old,i) => i === index ? action : old)})}
  async function dryRun() {
    setBusy(true);setError('');setPreview(null);setReview(false)
    try { const result = await request<components['schemas']['EntriesPage']>('/v1/rules:dry-run',{method:'POST',body:JSON.stringify({predicate:draft.predicate,actions})});setPreview({body:serialized,entries:result.entries ?? []}) }
    catch (error) {setError(message(error))}
    finally {setBusy(false)}
  }
  async function save() {
    if (!reviewed || invalid) return
    setBusy(true);setError('')
    try {await request(rule ? `/v1/rules/${rule.id}` : '/v1/rules',{method:rule ? 'PATCH' : 'POST',body:serialized});onSaved()}
    catch (error) {setError(message(error))}
    finally {setBusy(false)}
  }
  return <section aria-label="Rule editor"><h3>{rule ? `Edit ${rule.name}` : 'New rule'}</h3><form onSubmit={event => {event.preventDefault();void dryRun()}}><fieldset disabled={busy}><legend>Rule details</legend>
    <label>Name<input required maxLength={255} value={draft.name} onChange={event => setDraft({...draft,name:event.target.value})}/></label>
    <label><input type="checkbox" checked={draft.enabled} onChange={event => setDraft({...draft,enabled:event.target.checked})}/>Enabled</label>
    <PredicateEditor value={draft.predicate} onChange={predicate => setDraft({...draft,predicate})} feeds={feeds} folders={folders} pipeline={pipeline}/>
    {actions.map((action,index) => <fieldset key={index}><legend>Action {index+1}</legend><label>Action<select aria-label={`Action ${index+1} type`} value={action.type} onChange={event => changeAction(index,event.target.value === 'notify' ? {type:'notify',budget:3} : event.target.value === 'set_shelf' ? {type:'set_shelf',shelf:'later'} : {type:event.target.value})}>{actionTypes.map(([type,title]) => <option key={type} value={type}>{title}</option>)}</select></label>
      {actionRestriction(draft.predicate,action.type) && <p role="note">{actionRestriction(draft.predicate,action.type)}</p>}
      {action.type === 'add_label' && <label>Native label<select value={action.label_id ?? ''} required onChange={event => changeAction(index,{type:'add_label',label_id:Number(event.target.value)})}><option value="">Choose a label</option>{labels.filter(label => !label.Deleted).map(label => <option key={label.ID} value={label.ID}>{label.Name}</option>)}</select></label>}
      {action.type === 'set_shelf' && <label>Shelf<select value={action.shelf} onChange={event => changeAction(index,{type:'set_shelf',shelf:event.target.value})}><option value="later">Later</option><option value="done">Done</option></select></label>}
      {action.type === 'notify' && <label>Daily notification budget<input type="number" min={1} required value={action.budget ?? 3} onChange={event => changeAction(index,{type:'notify',budget:Number(event.target.value)})}/></label>}
      {action.type === 'webhook' && <label>Webhook destination<select required value={action.webhook_id ?? ''} onChange={event => changeAction(index,{type:'webhook',webhook_id:Number(event.target.value)})}><option value="">Choose a webhook</option>{webhooks.data?.filter(hook => hook.active).map(hook => <option key={hook.id} value={hook.id}>{hook.url}</option>)}</select></label>}
      <button type="button" onClick={() => setDraft({...draft,actions:actions.filter((_,i) => i !== index)})}>Remove action {index+1}</button>
    </fieldset>)}
    <button type="button" onClick={() => setDraft({...draft,actions:[...actions,{type:'notify',budget:3}]})}>Add action</button>
    <p>Shelf changes require selected feeds. Webhooks can use only selected feeds and your keywords. Notifications have a daily budget.</p>
    {invalid && <p role="note">{invalid}</p>}
    <button disabled={Boolean(invalid) || !draft.name.trim()}>Dry run on the past seven days</button>
    {reviewed && <div role="status"><p>Would have matched {preview.entries.length} items.</p><button type="button" onClick={() => setReview(!review)}>Review them</button><button type="button" disabled={Boolean(invalid)} onClick={() => {void save()}}>Save</button></div>}
    {reviewed && review && <ul aria-label="Dry-run matches">{preview.entries.map(entry => <li key={entry.id}><strong>{entry.title || 'Untitled'}</strong><p>{entry.author && `${entry.author} · `}Feed: {feeds.find(feed => feed.feed_id === entry.feed_id)?.title || entry.feed_id}{entry.first_seen_at && ` · ${new Date(entry.first_seen_at).toLocaleString()}`}</p></li>)}</ul>}
    {preview && !reviewed && <p>Rule changed. Run the dry run again before saving.</p>}
    <button type="button" onClick={onCancel}>Cancel</button>
    </fieldset></form>{(error || webhooks.error) && <p role="alert">{error || webhooks.error?.message}</p>}</section>
}

function Watches({watches, onChanged}: {watches: components['schemas']['WatchRule'][]; onChanged: () => void}) {
  const [name,setName] = useState('')
  const [description,setDescription] = useState('')
  const [budget,setBudget] = useState(3)
  const [confirmation,setConfirmation] = useState(false)
  const [checkBudget,setCheckBudget] = useState(3)
  const [error,setError] = useState('')
  const [busy,setBusy] = useState(false)
  async function submit(path: string, method: string, body?: components['schemas']['WatchBody']) {
    setBusy(true);setError('')
    try {await request(path,{method,body:body && JSON.stringify(body)});onChanged();if(body){setName('');setDescription('')}}
    catch(error) {setError(message(error))}
    finally {setBusy(false)}
  }
  return <section aria-label="Watch rules"><h3>Watch rules</h3><p>A watch matches a semantic description and only notifies you. It cannot change shelves, label entries or call webhooks. A new description costs an embedding call.</p>
    <ul>{watches.map(watch => <li key={watch.id}><strong>{watch.name}</strong><p>{watch.predicate.value} · {watch.status} · {watch.match_count} matches · Last match: {watch.last_match_at ?? 'Never'} · Daily notifications: {watch.actions?.find(a => a.type === 'notify')?.budget ?? 3}</p>{watch.disable_reason && <p>{watch.disable_reason}</p>}<button disabled={busy} onClick={() => {void submit(`/v1/rules/${watch.id}:${watch.enabled ? 'disable' : 'enable'}`,'POST')}}>{watch.enabled ? 'Disable' : 'Enable'} watch {watch.name}</button><button disabled={busy} onClick={() => {void submit(`/v1/watch-rules/${watch.id}`,'DELETE')}}>Delete watch {watch.name}</button></li>)}</ul>
    <form onSubmit={event => {event.preventDefault();void submit('/v1/watch-rules','POST',{name,description,budget,enabled:true,check_llm:confirmation,check_budget:checkBudget})}}><fieldset disabled={busy}><legend>New watch</legend><label>Watch name<input required value={name} onChange={event => setName(event.target.value)}/></label><label>Semantic description<textarea required value={description} onChange={event => setDescription(event.target.value)}/></label><label>Daily watch notification budget<input type="number" required min={1} value={budget} onChange={event => setBudget(Number(event.target.value))}/></label><label><input type="checkbox" checked={confirmation} onChange={event => setConfirmation(event.target.checked)}/>Confirm matches with a model</label>{confirmation && <label>Daily confirmation call budget<input required type="number" min={1} value={checkBudget} onChange={event => setCheckBudget(Number(event.target.value))}/></label>}<button>Create watch</button></fieldset></form>{error && <p role="alert">{error}</p>}
  </section>
}
