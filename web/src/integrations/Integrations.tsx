// Article integrations send the open article to Karakeep or Hister. The API
// stores each destination's token server-side and returns only token_set, so
// the form can never display or resend a saved token: a blank token field
// keeps it, typing replaces it, and Hister's optional token has an explicit
// remove action. The saved token is never placed in the DOM or localStorage.
import {useEffect, useRef, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {request} from '../request'
import {useBlocker} from '../router'
import {useOnline} from '../offline/Offline'
import type {components} from '../api.generated'

export type IntegrationProvider = 'karakeep' | 'hister'
export type IntegrationConfig = components['schemas']['IntegrationSettings']
export type IntegrationSettings = components['schemas']['ArticleIntegrations']
export type IntegrationSendResult = components['schemas']['SendArticleResult']

const providers: IntegrationProvider[] = ['karakeep', 'hister']
const providerNames: Record<IntegrationProvider, string> = {karakeep: 'Karakeep', hister: 'Hister'}
const emptyConfig: IntegrationConfig = {url: '', default_list_id: '', default_label: '', token_set: false}
const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback
const newOp = () => crypto.randomUUID()

// 'keep' is the ordinary blank field; 'clear' is the explicit removal that
// sends an empty token. The server never returns the saved value.
type TokenDraft = {kind: 'keep'} | {kind: 'set'; value: string} | {kind: 'clear'}
type Draft = {url: string; default_list_id: string; default_label: string; token: TokenDraft}
const draftFrom = (config: IntegrationConfig): Draft => ({url: config.url, default_list_id: config.default_list_id, default_label: config.default_label, token: {kind: 'keep'}})

// A response from a departed article or signed-out session must not update
// the next reader's notices or shared query cache.
function useAlive() {
  const alive = useRef(true)
  useEffect(() => {alive.current = true; return () => {alive.current = false}}, [])
  return alive
}

export function Integrations() {
  const settings = useQuery({queryKey: ['integrations'], queryFn: ({signal}) => request<IntegrationSettings>('/v1/integrations', {signal})})
  const data = settings.data
  return <section aria-labelledby="integrations-heading">
    <h2 id="integrations-heading">Article integrations</h2>
    <p className="muted">Send an article from its toolbar to Karakeep or Hister. Hister receives Earnest’s stored article text, so it keeps the words even if the original page changes; Karakeep retrieves the original itself.</p>
    <p className="muted">A destination on your LAN or tailnet must be allowed outbound: set EARNEST_OUTBOUND_ALLOW_PRIVATE=host:port on the Earnest host.</p>
    {settings.isPending && <p role="status">Loading integrations…</p>}
    {settings.error && <p role="alert">{message(settings.error, 'Could not load integrations.')} <button type="button" onClick={() => {void settings.refetch()}}>Try again</button></p>}
    {data && providers.map(provider => <ProviderForm key={provider} provider={provider} config={data[provider] ?? emptyConfig} />)}
  </section>
}

function ProviderForm({provider, config}: {provider: IntegrationProvider; config: IntegrationConfig}) {
  const cache = useQueryClient()
  const alive = useAlive()
  const [draft, setDraft] = useState<Draft>(() => draftFrom(config))
  const [base, setBase] = useState(config)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [saving, setSaving] = useState(false)
  const name = providerNames[provider]
  const token = draft.token
  const tokenDirty = token.kind === 'clear' || (token.kind === 'set' && token.value !== '')
  const matches = (value: IntegrationConfig) => draft.url === value.url && draft.default_list_id === value.default_list_id && draft.default_label === value.default_label
  const dirty = !matches(config) || tokenDirty
  // Adopt settings saved elsewhere (another tab, a restore) unless this
  // provider has unsaved edits of its own.
  if (config !== base) {
    setBase(config)
    if (matches(base) && !tokenDirty) setDraft(draftFrom(config))
  }
  const release = useBlocker(dirty, `Leave without saving your ${name} integration?`)
  async function save(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setError('')
    setNotice('')
    try {
      const body: {url: string; default_list_id: string; default_label: string; token?: string; op_id: string} = {
        url: draft.url.trim(),
        default_list_id: draft.default_list_id.trim(),
        default_label: draft.default_label.trim(),
        op_id: newOp(),
      }
      // Omitted while the field is blank and unchanged, so the server keeps
      // the saved token; an explicit clear sends the empty string.
      if (token.kind === 'clear') body.token = ''
      else if (token.kind === 'set' && token.value !== '') body.token = token.value
      const next = await request<IntegrationConfig>(`/v1/integrations/${provider}`, {method: 'PUT', body: JSON.stringify(body)})
      if (!alive.current) return
      setDraft(draftFrom(next))
      cache.setQueryData<IntegrationSettings>(['integrations'], previous => ({karakeep: emptyConfig, hister: emptyConfig, ...previous, [provider]: next}))
      release()
      setNotice(`${name} saved.`)
      await cache.invalidateQueries({queryKey: ['integrations']})
    } catch (cause) {
      if (alive.current) setError(message(cause, `Could not save ${name}.`))
    } finally {
      if (alive.current) setSaving(false)
    }
  }
  const changedURL = draft.url.trim().replace(/\/+$/, '') !== config.url
  const tokenPlaceholder = token.kind === 'clear'
    ? 'The saved token will be removed.'
    : config.token_set && !changedURL
      ? 'Saved. Leave blank to keep it.'
      : provider === 'karakeep' ? 'Required to enable Karakeep.' : 'Optional.'
  return <form aria-label={`${name} integration`} onSubmit={event => {void save(event)}} onChange={() => setNotice('')}>
    <h3>{name}</h3>
    <fieldset disabled={saving}>
      <legend className="sr-only">{name} destination</legend>
      <label>Base URL<input value={draft.url} onChange={event => setDraft({...draft, url: event.target.value})} placeholder="https://example.test" autoComplete="off" spellCheck={false} aria-describedby={`${provider}-url-hint`} /></label>
      <p id={`${provider}-url-hint`} className="hint">The service’s base URL, without {provider === 'karakeep' ? '/api/v1' : '/api/add'}, credentials or a query string. Clear the URL to disable it and remove its token.</p>
      <label>API token<input type="password" value={token.kind === 'set' ? token.value : ''} onChange={event => setDraft({...draft, token: {kind: 'set', value: event.target.value}})} placeholder={tokenPlaceholder} autoComplete="new-password" aria-describedby={`${provider}-token-hint`} /></label>
      <p id={`${provider}-token-hint`} className="hint">{config.token_set && changedURL ? 'Changing the URL removes the saved token. Enter a token again if this destination needs one.' : config.token_set ? 'A token is saved. Leave this blank to keep it, or type a new one to replace it.' : provider === 'karakeep' ? 'Paste the API key from Karakeep’s settings.' : 'Optional. Leave blank unless your Hister server requires one.'}</p>
      {provider === 'karakeep'
        ? <><label>Default list ID<input value={draft.default_list_id} onChange={event => setDraft({...draft, default_list_id: event.target.value})} placeholder="list-1a2b3c" autoComplete="off" spellCheck={false} aria-describedby="karakeep-list-hint" /></label><p id="karakeep-list-hint" className="hint">The ID in a manual list’s URL. Leave blank to save without assigning a list.</p></>
        : <><label>Default label<input value={draft.default_label} onChange={event => setDraft({...draft, default_label: event.target.value})} placeholder="Read later" aria-describedby="hister-label-hint" /></label><p id="hister-label-hint" className="hint">Hister has no folders, so sent articles get this label.</p></>}
      <p className="button-row">
        <button className="primary" disabled={saving || !dirty}>{saving ? 'Saving…' : 'Save'}</button>
        {provider === 'hister' && config.token_set && <button type="button" aria-pressed={token.kind === 'clear'} onClick={() => setDraft({...draft, token: token.kind === 'clear' ? {kind: 'keep'} : {kind: 'clear'}})}>{token.kind === 'clear' ? 'Keep saved token' : 'Remove saved token'}</button>}
        {dirty && <span className="muted">Unsaved changes</span>}
      </p>
    </fieldset>
    {error && <p role="alert" className="form-error">{error}</p>}
    {notice && <p role="status">{notice}</p>}
  </form>
}

export function SendActions({entryID, onNotify}: {entryID: number; onNotify?: (message: string) => void}) {
  const online = useOnline()
  const alive = useAlive()
  const settings = useQuery({queryKey: ['integrations'], queryFn: ({signal}) => request<IntegrationSettings>('/v1/integrations', {signal})})
  const [error, setError] = useState('')
  const [sending, setSending] = useState<IntegrationProvider | null>(null)
  const configured = settings.data ? providers.filter(provider => settings.data?.[provider]?.url) : []
  async function send(provider: IntegrationProvider) {
    setSending(provider)
    setError('')
    try {
      const result = await request<IntegrationSendResult>(`/v1/entries/${entryID}/send/${provider}`, {method: 'POST', body: JSON.stringify({op_id: newOp()})})
      if (alive.current) onNotify?.(result.message)
    } catch (cause) {
      // A problem response, including a bookmark saved but not filed in the
      // default list, is a failure even when part of it worked.
      if (alive.current) setError(message(cause, `Could not send to ${providerNames[provider]}.`))
    } finally {
      if (alive.current) setSending(null)
    }
  }
  if (!configured.length) return null
  return <>
    {configured.map(provider => <button key={provider} type="button" className="tool secondary" disabled={!online || sending !== null} aria-busy={sending === provider} title={`Send to ${providerNames[provider]}`} onClick={() => {void send(provider)}}><span aria-hidden="true" className="integration-mark">{provider === 'karakeep' ? 'K' : 'H'}</span><span className="label">Send to {providerNames[provider]}</span></button>)}
    {error && <span role="alert" style={{color: 'var(--danger)', fontSize: 'var(--step--1)', lineHeight: 1.35, padding: '0 .5rem', flex: '1 1 12rem', minWidth: '8rem'}}>{error}</span>}
  </>
}
