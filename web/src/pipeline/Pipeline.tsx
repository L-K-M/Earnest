import {useEffect, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import type {Entry} from '../reader'
import {request, RequestError} from '../request'
import {usePipeline} from '../search/pipeline'

type GroundedClaim = components['schemas']['GroundedClaim']
const message = (error: unknown) => error instanceof Error ? error.message : 'The request failed.'

function GroundedClaims({claims, onOpen}: {claims: GroundedClaim[] | null; onOpen: (id: number) => void}) {
  return <>{claims?.map((claim, index) => <div key={index}><p>{claim.text}</p>{!claim.verified && <p>Citation not found in the article</p>}{claim.citations?.map((citation, i) => <div key={i}>{!citation.verified && claim.verified && <p>Citation not found in the article</p>}{citation.passages?.map((passage, j) => <p key={j}>{citation.verified ? <button onClick={() => onOpen(citation.entry_id)}>{passage}</button> : passage}</p>)}</div>)}</div>)}</>
}

export function Summary({entryID, shortcuts}: {entryID: number; shortcuts: boolean}) {
  const {status} = usePipeline()
  const [visible, setVisible] = useState(false)
  const [result, setResult] = useState<components['schemas']['SummaryResult']>()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [passage, setPassage] = useState('')
  async function toggle() {
    if (visible) {setVisible(false);return}
    setVisible(true)
    if (result || busy) return
    setBusy(true);setError('')
    try {setResult(await request(`/v1/entries/${entryID}/summary`, {method:'POST'}))}
    catch (error) {setError(message(error))}
    finally {setBusy(false)}
  }
  useEffect(() => {
    if (status.summary !== 'enabled' || !shortcuts) return
    function key(event: KeyboardEvent) {
      if (event.key !== 'Tab' || event.shiftKey || event.ctrlKey || event.altKey || event.metaKey || event.defaultPrevented) return
      if (!(event.target instanceof Element) || event.target.closest('input, textarea, select, button, a, [contenteditable], dialog')) return
      event.preventDefault();void toggle()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  })
  if (status.summary !== 'enabled') return null
  return <section aria-label="Summary"><button onClick={() => {void toggle()}} aria-expanded={visible}>Summary</button>{visible && <>{busy && <p role="status">Generating summary…</p>}{error && <p role="alert">{error} <button onClick={() => {setVisible(false)}}>Close</button></p>}{result && <><p>Summary · {result.model} · <time dateTime={result.generated_at}>{new Date(result.generated_at).toLocaleString()}</time></p>{result.excerpt_notice && <p>{result.excerpt_notice}</p>}{result.claims?.map((claim, index) => <div key={index}><p>{claim.text}</p>{!claim.verified && <p>Citation not found in the article</p>}{claim.passages?.map((text, i) => <button key={i} onClick={() => setPassage(text)}>{text}</button>)}</div>)}{passage && <p>Highlighting is unavailable in the article frame. Passage: {passage}</p>}</>}</>}</section>
}

export function Ask({onOpen}: {onOpen: (entry: Entry) => void}) {
  const [question, setQuestion] = useState('')
  const [result, setResult] = useState<components['schemas']['AskResult']>()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit() {
    setBusy(true);setError('')
    try {setResult(await request('/v1/ask',{method:'POST',body:JSON.stringify({question})}))}
    catch (error) {setError(message(error))}
    finally {setBusy(false)}
  }
  async function open(id: number) {
    try {onOpen(await request<Entry>(`/v1/entries/${id}`))} catch (error) {setError(message(error))}
  }
  return <main><h2>Ask my feeds</h2><p>Answers come from your feeds only.</p><form onSubmit={event => {event.preventDefault();void submit()}}><label>Question<textarea value={question} required onChange={event => setQuestion(event.target.value)}/></label><button disabled={busy}>Ask</button></form>{error && <p role="alert">{error}</p>}{result && <><p>{result.remaining_today} of {result.daily_limit} questions remaining today.</p>{result.excerpt_notice && <p>{result.excerpt_notice}</p>}<GroundedClaims claims={result.claims} onOpen={id => {void open(id)}}/></>}</main>
}

export function Edition({onOpen}: {onOpen: (entry: Entry) => void}) {
  const {status} = usePipeline()
  const cache = useQueryClient()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const latest = useQuery({queryKey:['edition-latest'],enabled:status.edition === 'enabled',queryFn:({signal}) => request<components['schemas']['Edition']>('/v1/editions/latest',{signal}),retry:false,refetchInterval: query => query.state.data && ['pending','paused'].includes(query.state.data.status) ? 5000 : false})
  async function trigger() {
    setBusy(true);setError('')
    try {await request('/v1/editions',{method:'POST',body:JSON.stringify({period:'weekly',op_id:crypto.randomUUID()})});await cache.invalidateQueries({queryKey:['edition-latest']})}
    catch (error) {setError(message(error))}
    finally {setBusy(false)}
  }
  async function open(id: number) {
    try {onOpen(await request<Entry>(`/v1/entries/${id}`))} catch (error) {setError(message(error))}
  }
  if (status.edition !== 'enabled') return null
  return <section aria-label="Edition prose"><button disabled={busy} onClick={() => {void trigger()}}>Make this week's edition</button>{error && <p role="alert">{error}</p>}{latest.error && !(latest.error instanceof RequestError && latest.error.status === 404) && <p role="alert">{latest.error.message}</p>}{latest.data && latest.data.status !== 'ready' && <p>Edition status: {latest.data.status}</p>}{latest.data?.error && <p role="alert">{latest.data.error}</p>}{latest.data?.sections?.map((section, i) => section.claims?.length ? <section key={i}><h3>{section.name}</h3><GroundedClaims claims={section.claims} onOpen={id => {void open(id)}}/></section> : null)}</section>
}
