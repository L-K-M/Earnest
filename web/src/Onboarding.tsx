import {useEffect, useRef, useState} from 'react'
import {Typography, type ReadingSettings} from './Settings'
import {
  defaultReadingSettings, healthPending, importOPML, isFirehose, latestSeq, listSubscriptions, newOp, readFileText,
  patchSubscription, putSettings, startRiverAt, summarizeImport,
  type OPMLResult, type Subscription,
} from './feeds'

// Volume detection is bounded: the fetcher leases due feeds within a minute,
// so a short poll covers fast feeds without holding onboarding open-ended.
const HEALTH_POLLS = 8
const HEALTH_POLL_MS = 2500

function sleep(ms: number): Promise<void> { return new Promise(resolve => window.setTimeout(resolve, ms)) }
function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

export function Onboarding({onComplete}: {onComplete: () => void}) {
  const alive = useRef(true)
  useEffect(() => () => { alive.current = false }, [])
  const [step, setStep] = useState<'import' | 'report' | 'mode'>('import')
  const [file, setFile] = useState<File | null>(null)
  const [results, setResults] = useState<OPMLResult[] | null>(null)
  const [subs, setSubs] = useState<Subscription[]>([])
  const [checking, setChecking] = useState(false)
  const [settings, setSettings] = useState<ReadingSettings>(defaultReadingSettings())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // Each step replaces the buttons that led to it; take focus to its heading.
  const heading = useRef<HTMLHeadingElement>(null)
  const firstStep = useRef(true)
  useEffect(() => {
    if (firstStep.current) { firstStep.current = false; return }
    heading.current?.focus()
  }, [step])

  async function measure() {
    setChecking(true)
    try {
      for (let poll = 0; poll < HEALTH_POLLS; poll++) {
        const fresh = await listSubscriptions()
        if (!alive.current) return
        setSubs(fresh ?? [])
        if (!(fresh ?? []).some(healthPending)) break
        await sleep(HEALTH_POLL_MS)
      }
    } catch (cause) {
      if (alive.current) setError(message(cause, 'Could not check posting volume. You can review cadence in Feeds.'))
    } finally {
      if (alive.current) setChecking(false)
    }
  }

  async function upload(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!file) return
    setBusy(true)
    setError('')
    try {
      setResults(await importOPML(await readFileText(file)) ?? [])
      setStep('report')
      void measure()
    } catch (cause) {
      setError(message(cause, 'Could not import that OPML file.'))
    } finally {
      setBusy(false)
    }
  }

  async function moveFirehoses() {
    setBusy(true)
    setError('')
    try {
      for (const sub of subs.filter(isFirehose)) await patchSubscription(sub.id, {cadence: 'week', op_id: newOp()})
      setStep('mode')
    } catch (cause) {
      setError(message(cause, 'Could not move every firehose. Feeds can move the rest.'))
    } finally {
      setBusy(false)
    }
  }

  async function start(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (settings.reading_mode === 'river') {
        const seq = await latestSeq()
        if (seq > 0) await startRiverAt(seq)
      }
      await putSettings(settings)
      onComplete()
    } catch (cause) {
      setError(message(cause, 'Could not save your settings.'))
    } finally {
      setBusy(false)
    }
  }

  if (step === 'import') {
    return <section className="card" aria-labelledby="onboarding-title"><p className="step muted">Step 1 of 3</p><h2 id="onboarding-title" ref={heading} tabIndex={-1}>Welcome to Earnest</h2>
      <p>Bring your subscriptions from another reader, or start empty.</p>
      <form onSubmit={upload}>
        <label>OPML subscription file<input type="file" accept=".opml,.xml,text/x-opml,application/xml,text/xml" onChange={e => setFile(e.target.files?.[0] ?? null)} /></label>
        <button disabled={busy || !file}>Import and continue</button>
        <button type="button" disabled={busy} onClick={() => setStep('mode')}>Skip: I will add feeds later</button>
        {error && <p role="alert">{error}</p>}
      </form>
    </section>
  }

  if (step === 'report' && results) {
    const summary = summarizeImport(results)
    const firehoses = subs.filter(isFirehose)
    const pending = subs.filter(healthPending).length
    return <section className="card" aria-labelledby="report-title"><p className="step muted">Step 2 of 3</p><h2 id="report-title" ref={heading} tabIndex={-1}>Import complete</h2>
      <p>Found {summary.feeds} {summary.feeds === 1 ? 'feed' : 'feeds'}{summary.folders > 0 && ` in ${summary.folders} ${summary.folders === 1 ? 'folder' : 'folders'}`}. Nothing is marked read.</p>
      {summary.failed.length > 0 && <p role="alert">{summary.failed.length} could not be imported: {summary.failed.join('; ')}</p>}
      {checking && <p role="status">Measuring how busy your feeds are…</p>}
      {!checking && pending > 0 && <p>Volume is still being measured for {pending} {pending === 1 ? 'feed' : 'feeds'}; Feeds shows it once the first fetches finish.</p>}
      {!checking && firehoses.length > 0 && <>
        <p>{firehoses.length} {firehoses.length === 1 ? 'feed posts' : 'feeds post'} more than 20 a day. Put {firehoses.length === 1 ? 'it' : 'them'} in This week?</p>
        <button disabled={busy} onClick={() => { void moveFirehoses() }}>Move to This week</button>
      </>}
      <button disabled={busy || checking} onClick={() => setStep('mode')}>{firehoses.length > 0 && !checking ? 'Keep in the river' : 'Continue'}</button>
      {error && <p role="alert">{error}</p>}
    </section>
  }

  return <section className="card" aria-labelledby="mode-title"><p className="step muted">Step 3 of 3</p><h2 id="mode-title" ref={heading} tabIndex={-1}>How do you like to read?</h2>
    <p>Front page brings together today's and this week's stories across your feeds. All articles lets you browse chronologically. These preferences apply to river lists.</p>
    <form onSubmit={start}>
      <fieldset><legend>Reading mode</legend>
        <label><input type="radio" name="reading_mode" checked={settings.reading_mode === 'river'} onChange={() => setSettings({...settings, reading_mode: 'river'})} /> A river that ends: resume where you left off; what you skip stays searchable.</label>
        <label><input type="radio" name="reading_mode" checked={settings.reading_mode === 'unread_counts'} onChange={() => setSettings({...settings, reading_mode: 'unread_counts'})} /> Unread counts: every feed keeps a count until you clear it.</label>
      </fieldset>
      {settings.reading_mode === 'river' && <label>River retention in days<input type="number" min={1} max={365} value={settings.today_decay_days} onChange={e => {
        const today_decay_days = e.target.valueAsNumber
        setSettings({...settings, today_decay_days: Number.isFinite(today_decay_days) ? today_decay_days : defaultReadingSettings().today_decay_days})
      }} required /></label>}
      <Typography value={settings} onChange={value => setSettings({...settings, ...value})} />
      <label><input type="checkbox" checked={settings.single_key_shortcuts} onChange={e => setSettings({...settings, single_key_shortcuts: e.target.checked})} /> Single-key shortcuts</label>
      <button disabled={busy}>Start reading</button>
      {error && <p role="alert">{error}</p>}
    </form>
  </section>
}
