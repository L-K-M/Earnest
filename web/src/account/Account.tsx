import {useRef, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import {request} from '../request'
import {useBlocker} from '../router'

type Schema = components['schemas']
const scopes = [
  ['entries:read', 'Read all your subscriptions and articles, including private feeds.'],
  ['annotations:write', 'Add and edit its own notes and annotations.'],
  ['position:own', 'Update its own reading position.'],
  ['state:write-human', 'Mark articles read or Done for you.'],
  ['notify:send', 'Send notifications within its daily budget.'],
  ['notify:urgent', 'Bypass quiet hours while still using the daily budget.'],
] as const
const events = ['feed.failing', 'edition.ready', 'entry.matched'] as const
const op = () => crypto.randomUUID()
const message = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.'
const json = (body: unknown, method = 'POST'): RequestInit => ({method, body: JSON.stringify(body)})

// Every supported browser has Intl.supportedValuesOf; the guard keeps the
// component usable in test environments that lack it.
const timeZones: string[] = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : []
const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

// zoneError names the fix when Intl cannot use the value as a time zone. The
// server applies the same IANA zone rules when it schedules quiet hours.
function zoneError(value: string): string {
  try { new Intl.DateTimeFormat('en', {timeZone: value}) } catch { return 'Use an IANA time zone such as Europe/Zurich.' }
  return ''
}

// formatTimestamp renders an ISO timestamp in the reader's zone, falling
// back to 'never' for an absent or unparseable value.
function formatTimestamp(value: string | null | undefined): string {
  if (!value) return 'never'
  const at = new Date(value)
  return Number.isNaN(at.getTime()) ? 'never' : at.toLocaleString()
}

function Secret({secret, onClose}: {secret:string; onClose:()=>void}) {
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  return <section>
    <p>Save this secret now. It will not be shown again.</p>
    <code className="secret">{secret}</code>
    <p className="button-row">
      <button type="button" onClick={() => {void (async () => {try {await navigator.clipboard.writeText(secret); setCopied(true)} catch(e) {setError(message(e)); setCopied(false)}})()}}>{copied ? 'Copied' : 'Copy secret'}</button>
      {copied && <span role="status">Copied to clipboard.</span>}
      <button type="button" onClick={onClose}>Done</button>
    </p>
    {error && <p role="alert">{error}</p>}
  </section>
}

export function Tokens() {
  const client = useQueryClient()
  const list = useQuery({queryKey:['tokens'], queryFn:({signal}) => request<Schema['Token'][] | null>('/v1/tokens', {signal})})
  const [selected, setSelected] = useState<string[]>([])
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [code, setCode] = useState(() => new URLSearchParams(location.search).get('user_code') ?? '')
  const [approved, setApproved] = useState(false)
  const [pending, setPending] = useState(false)
  const [draftDirty, setDraftDirty] = useState(false)
  const [deviceDirty, setDeviceDirty] = useState(false)
  // A code prefilled by the URL is not an edit. The one-time secret stays
  // guarded until Secret is dismissed, and an in-flight request keeps its
  // result from being lost to an unmount.
  useBlocker(draftDirty || (deviceDirty && !approved) || pending || Boolean(secret), secret ? 'Leave this screen? This token will not be shown again.' : 'Leave without saving your tokens or device approval?')
  async function run(action:()=>Promise<void>) {setPending(true); setError(''); try {await action()} catch(e) {setError(message(e))} finally {setPending(false)}}
  async function create(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = e.currentTarget
    const data = new FormData(form)
    await run(async () => {
      const expiry = String(data.get('expires_at'))
      const parsed = expiry ? new Date(expiry) : null
      if (parsed && Number.isNaN(parsed.getTime())) throw new Error('Enter a valid expiry date.')
      const result = await request<Schema['TokenSecret']>('/v1/tokens', json({kind:'agent', name:String(data.get('name')), audience:String(data.get('audience')), scopes:selected, daily_budget:Number(data.get('daily_budget')), ...(parsed ? {expires_at:parsed.toISOString()} : {})}))
      setSecret(result.secret); await client.invalidateQueries({queryKey:['tokens']})
      // Clear the form so the next token starts from a clean slate.
      form.reset()
      setSelected([])
      setDraftDirty(false)
    })
  }
  return <section>
    <h2>Tokens and device approval</h2>
    {(error || list.error) && <p role="alert">{error || list.error?.message}</p>}
    <ul className="settings-list">
      {list.data?.map(token => <li key={token.id}>
        <div className="settings-row-head"><strong>{token.name}</strong></div>
        <p className="settings-row-detail muted">{token.kind} · {token.audience} · Scopes: {token.scopes?.join(', ') || 'none'} · Daily budget: {token.daily_budget} · Last used: {formatTimestamp(token.last_used_at)} · Expires: {formatTimestamp(token.expires_at)}</p>
        <div className="button-row">
          {token.revoked_at
            ? <span className="chip">Revoked</span>
            : <button disabled={pending} aria-label={`Revoke ${token.name}`} onClick={() => {if (window.confirm(`Revoke ${token.name}?`)) void run(async () => {await request(`/v1/tokens/${token.id}`, {method:'DELETE'}); await client.invalidateQueries({queryKey:['tokens']})})}}>Revoke</button>}
        </div>
      </li>)}
      {list.isSuccess && !list.data?.length && <li className="empty-state">No agent tokens yet.</li>}
    </ul>
    <form onSubmit={e => {void create(e)}} onChange={() => setDraftDirty(true)}>
      <h3>Create an agent token</h3>
      <p>Agents can read everything you subscribe to, including private feeds and full text.</p>
      <label>Name<input name="name" required aria-describedby="token-name-hint" /></label>
      <p id="token-name-hint" className="hint">Shown in Activity next to everything this agent does.</p>
      <label>Audience<select name="audience"><option value="mcp">MCP</option><option value="api">REST</option></select></label>
      <fieldset><legend>Agent scopes</legend>{scopes.map(([scope, description]) => <label key={scope}><input type="checkbox" checked={selected.includes(scope)} onChange={e => setSelected(e.target.checked ? [...selected, scope] : selected.filter(item => item !== scope))} /> {scope}: {description}</label>)}</fieldset>
      <label>Daily notification budget<input name="daily_budget" type="number" min="0" defaultValue="3" required aria-describedby="daily-budget-hint" /></label>
      <p id="daily-budget-hint" className="hint">0 means the agent cannot notify you.</p>
      <label>Expiry (optional)<input name="expires_at" type="datetime-local" aria-describedby="token-expiry-hint" /></label>
      <p id="token-expiry-hint" className="hint">Leave empty for a token that does not expire.</p>
      <button disabled={pending || Boolean(secret)}>Create agent token</button>
    </form>
    {secret && <Secret secret={secret} onClose={() => setSecret('')} />}
    <form id="device" onSubmit={e => {e.preventDefault(); void run(async () => {await request('/v1/devices/approve', json({user_code:code.trim().toUpperCase()})); setApproved(true)})}}>
      <h3>Approve an app or CLI</h3>
      <p>Only approve a code shown by a device you are signing in.</p>
      <label>Device code<input id="device-code" value={code} onChange={e => {setCode(e.target.value); setApproved(false); setDeviceDirty(true)}} required autoComplete="one-time-code" autoCapitalize="characters" spellCheck={false} placeholder="ABCD-EFGH" aria-describedby="device-code-hint" /></label>
      <p id="device-code-hint" className="hint">The code appears in the app or terminal that asked to sign in.</p>
      <button disabled={pending}>Approve device</button>
      {approved && <p role="status">Device approved.</p>}
    </form>
  </section>
}

export function Webhooks() {
  const client = useQueryClient()
  const list = useQuery({queryKey:['webhooks'], queryFn:({signal}) => request<Schema['Webhook'][] | null>('/v1/webhooks', {signal})})
  const [chosen, setChosen] = useState<string[]>([])
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [draftDirty, setDraftDirty] = useState(false)
  useBlocker(draftDirty || pending || Boolean(secret), secret ? 'Leave this screen? This webhook secret will not be shown again.' : 'Leave without saving this webhook?')
  async function run(action:()=>Promise<void>) {setPending(true);setError('');try {await action(); await client.invalidateQueries({queryKey:['webhooks']})} catch(e) {setError(message(e))} finally {setPending(false)}}
  return <section>
    <h2>Webhooks</h2>
    <p>Webhooks carry IDs only, not article content. Only you can create them.</p>
    {(error || list.error) && <p role="alert">{error || list.error?.message}</p>}
    <ul className="settings-list">
      {list.data?.map(hook => <li key={hook.id}>
        <div className="settings-row-head"><strong>{hook.url}</strong></div>
        <p className="settings-row-detail muted">{hook.events?.join(', ')} · {hook.active ? 'Enabled' : 'Disabled'} · Consecutive failures: {hook.consecutive_failures}</p>
        <div className="button-row">
          <button disabled={pending} aria-label={`${hook.active ? 'Disable' : 'Enable'} ${hook.url}`} onClick={() => {void run(async () => {await request(`/v1/webhooks/${hook.id}`, json({active:!hook.active, op_id:op()}, 'PATCH'))})}}>{hook.active ? 'Disable' : 'Enable'}</button>
          <button disabled={pending} aria-label={`Delete ${hook.url}`} onClick={() => {if (window.confirm(`Delete webhook ${hook.url}?`)) void run(async () => {await request(`/v1/webhooks/${hook.id}?op_id=${encodeURIComponent(op())}`, {method:'DELETE'})})}}>Delete</button>
        </div>
      </li>)}
      {list.isSuccess && !list.data?.length && <li className="empty-state">No webhooks yet.</li>}
    </ul>
    <form onSubmit={e => {e.preventDefault(); const form = e.currentTarget; void run(async () => {const data = new FormData(form); const result = await request<Schema['WebhookSecret']>('/v1/webhooks', json({url:String(data.get('url')), events:chosen, op_id:op()}));setSecret(result.secret); form.reset(); setChosen([]); setDraftDirty(false)})}} onChange={() => setDraftDirty(true)}>
      <h3>Create webhook</h3>
      <label>URL<input name="url" type="url" inputMode="url" spellCheck={false} required /></label>
      <fieldset><legend>Events</legend>{events.map(event => <label key={event}><input type="checkbox" checked={chosen.includes(event)} onChange={e => setChosen(e.target.checked ? [...chosen,event] : chosen.filter(item => item !== event))} /> {event}</label>)}</fieldset>
      <button disabled={pending || Boolean(secret) || chosen.length === 0}>Create webhook</button>
    </form>
    {secret && <Secret secret={secret} onClose={() => setSecret('')} />}
  </section>
}

function applicationKey(key:string): Uint8Array<ArrayBuffer> {
  const value = atob(key.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - key.length % 4) % 4))
  return Uint8Array.from(value, char => char.charCodeAt(0))
}
export function Push() {
  const client = useQueryClient()
  const settings = useQuery({queryKey:['push-settings'], queryFn:({signal}) => request<Schema['PushSettings']>('/v1/push-settings', {signal})})
  const devices = useQuery({queryKey:['devices'], queryFn:({signal}) => request<Schema['Device'][] | null>('/v1/devices', {signal})})
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [pending, setPending] = useState(false)
  const [timezoneError, setTimezoneError] = useState('')
  const [dirty, setDirty] = useState(false)
  const [adoptedRev, setAdoptedRev] = useState<number | null>(null)
  const timezoneRef = useRef<HTMLInputElement>(null)
  useBlocker(dirty || pending, 'Leave without saving your notification settings?')
  // Adopt settings saved elsewhere unless this form has unsaved edits, so a
  // refetch cannot silently discard them.
  const rev = settings.data?.rev ?? null
  if (rev !== null && !dirty && adoptedRev !== rev) setAdoptedRev(rev)
  async function run(action:()=>Promise<void>) {setPending(true); setError(''); setStatus(''); try {await action()} catch(e) {setError(message(e))} finally {setPending(false)}}
  return <section>
    <h2>Push notifications</h2>
    {(error || settings.error || devices.error) && <p role="alert">{error || settings.error?.message || devices.error?.message}</p>}
    {status && <p role="status">{status}</p>}
    {settings.data && <form key={adoptedRev} onChange={() => setDirty(true)} onSubmit={e => {
      e.preventDefault()
      const form = new FormData(e.currentTarget)
      const problem = zoneError(String(form.get('timezone')))
      // A blocked submit must not leave an earlier "saved" status on screen.
      setStatus('');setError('')
      setTimezoneError(problem)
      if (problem) return
      void run(async () => {
        const token = String(form.get('ntfy_token'))
        await request('/v1/push-settings', json({ntfy_server:String(form.get('ntfy_server')),ntfy_topic:String(form.get('ntfy_topic')), ...(form.has('clear_token') ? {ntfy_token:''} : token ? {ntfy_token:token} : {}), show_text:form.has('show_text'),quiet_start:String(form.get('quiet_start')),quiet_end:String(form.get('quiet_end')),timezone:String(form.get('timezone')),op_id:op()}, 'PUT'))
        await client.invalidateQueries({queryKey:['push-settings']})
        setStatus('Push settings saved.')
        // A failed save keeps the draft dirty; only success releases the guard.
        setDirty(false)
      })
    }}>
      <h3>ntfy and quiet hours</h3>
      <label>ntfy server URL<input name="ntfy_server" type="url" inputMode="url" autoComplete="url" spellCheck={false} defaultValue={settings.data.ntfy_server ?? ''} /></label>
      <label>ntfy topic<input name="ntfy_topic" autoCapitalize="none" spellCheck={false} defaultValue={settings.data.ntfy_topic ?? ''} /></label>
      <label>ntfy access token<input name="ntfy_token" type="password" autoComplete="new-password" /></label>
      <p>{settings.data.token_set ? 'A token is saved. Leave blank to keep it.' : 'No access token is saved.'}</p>
      {settings.data.token_set && <label><input type="checkbox" name="clear_token" /> Clear saved token (leave the field blank)</label>}
      <label><input type="checkbox" name="show_text" defaultChecked={settings.data.show_text} /> Show the agent's text on the lock screen</label>
      <p>This sends a preview of the agent's words to your self-hosted ntfy server and devices. Public ntfy.sh never receives agent text. Use a custom server only if you operate it.</p>
      <label>Quiet hours start<input name="quiet_start" type="time" defaultValue={settings.data.quiet_start ?? ''} /></label>
      <label>Quiet hours end<input name="quiet_end" type="time" defaultValue={settings.data.quiet_end ?? ''} /></label>
      <label>Timezone<input ref={timezoneRef} name="timezone" list="timezone-options" defaultValue={settings.data.timezone} required aria-invalid={timezoneError ? true : undefined} aria-describedby={timezoneError ? 'timezone-hint timezone-error' : 'timezone-hint'} onBlur={e => setTimezoneError(zoneError(e.target.value))} onChange={e => {if (!zoneError(e.target.value)) setTimezoneError('')}} /></label>
      <datalist id="timezone-options">{timeZones.map(zone => <option key={zone} value={zone} />)}</datalist>
      <p id="timezone-hint" className="hint">This browser reports {browserZone}. <button type="button" className="quiet small" onClick={() => {if (timezoneRef.current) timezoneRef.current.value = browserZone; setTimezoneError(''); setDirty(true)}}>Use {browserZone}</button></p>
      {timezoneError && <p role="alert" id="timezone-error" className="form-error">{timezoneError}</p>}
      <button disabled={pending}>Save push settings</button>
    </form>}
    <h3>Web Push for this browser</h3>
    <p>This browser shows only the server-generated notification title and body.</p>
    <button disabled={pending} onClick={() => {void run(async () => {
      if (!('serviceWorker' in navigator) || !('PushManager' in window)) throw new Error('Web Push is not available in this browser.')
      const key = await request<Schema['VapidKey']>('/v1/push/vapid-public-key')
      const registration = await navigator.serviceWorker.register('/push-worker.js', {scope:'/push/'})
      const subscription = await registration.pushManager.subscribe({userVisibleOnly:true, applicationServerKey:applicationKey(key.public_key)})
      const value = subscription.toJSON()
      if (!value.endpoint || !value.keys?.p256dh || !value.keys.auth) throw new Error('The browser did not provide push subscription keys.')
      await request('/v1/devices', json({kind:'webpush',endpoint:value.endpoint,p256dh:value.keys.p256dh,auth_secret:value.keys.auth,show_text:false,op_id:op()}))
      await client.invalidateQueries({queryKey:['devices']})
      setStatus('This browser is registered.')
    })}}>Subscribe this browser</button>
    <h3>Devices</h3>
    <ul className="settings-list">
      {devices.data?.map(device => <li key={device.id}>
        <div className="settings-row-head"><strong>{device.kind}</strong></div>
        <p className="settings-row-detail muted">{device.endpoint} · Agent text {device.show_text ? 'on' : 'off'}</p>
        <div className="button-row">
          <button disabled={pending} aria-label={`Remove ${device.kind} (${device.endpoint})`} onClick={() => {if (window.confirm('Remove this device?')) void run(async () => {await request(`/v1/devices/${device.id}?op_id=${encodeURIComponent(op())}`, {method:'DELETE'});await client.invalidateQueries({queryKey:['devices']})})}}>Remove</button>
        </div>
      </li>)}
      {devices.isSuccess && !devices.data?.length && <li className="empty-state">No devices registered.</li>}
    </ul>
  </section>
}
