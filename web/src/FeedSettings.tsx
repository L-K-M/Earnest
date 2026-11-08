import {useCallback, useEffect, useRef, useState} from 'react'
import {Link} from './router'
import {
  CADENCES, EXTRACT_MODES, groupByFolder, healthPending, isFirehose, newOp, normalizeFeedURLInput, parseCredentialHeaders, summarizeImport,
  createFolder, createMute, deleteFolder, deleteMute, explainHealth, exportOPML, importOPML, readFileText,
  listFolders, listMutes, listSubscriptions, patchSubscription, refreshSubscription, renameFolder, subscribe, unsubscribe,
  type Folder, type Mute, type OPMLResult, type SubscribeBody, type Subscription, type SubscriptionPatch,
} from './feeds'

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

type Act = (work: () => Promise<unknown>, note?: string) => Promise<boolean>

const URL_ERROR = 'That does not look like a feed address. Give the full URL, starting with https://.'
const DRAFT_KEY = 'earnest.subscribe.draft'
type SubscribeDraft = {url: string; title: string; folderID: number; cadence: string; extract: string}

// Only the harmless subscribe fields are restored. Credentials are never
// written here, so a remount cannot leak a password into session storage.
function readDraft(): SubscribeDraft {
  const fallback: SubscribeDraft = {url: '', title: '', folderID: 0, cadence: 'today', extract: ''}
  try {
    const raw = sessionStorage.getItem(DRAFT_KEY)
    if (!raw) return fallback
    const value: unknown = JSON.parse(raw)
    if (!value || typeof value !== 'object') return fallback
    const record = value as Record<string, unknown>
    return {
      url: typeof record.url === 'string' ? record.url : '',
      title: typeof record.title === 'string' ? record.title : '',
      folderID: typeof record.folderID === 'number' ? record.folderID : 0,
      cadence: typeof record.cadence === 'string' ? record.cadence : 'today',
      extract: typeof record.extract === 'string' ? record.extract : '',
    }
  } catch {
    return fallback
  }
}

export function Feeds({onChanged}: {onChanged: () => void}) {
  const [subs, setSubs] = useState<Subscription[] | null>(null)
  const [folders, setFolders] = useState<Folder[]>([])
  const [mutes, setMutes] = useState<Mute[]>([])
  const [loadError, setLoadError] = useState('')
  const [actionError, setActionError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const [nextSubs, nextFolders, nextMutes] = await Promise.all([listSubscriptions(), listFolders(), listMutes()])
    setSubs(nextSubs ?? [])
    setFolders(nextFolders ?? [])
    setMutes(nextMutes ?? [])
  }, [])

  useEffect(() => {
    let active = true
    load().catch(error => { if (active) setLoadError(message(error, 'Could not load feeds.')) })
    return () => { active = false }
  }, [load])

  const act: Act = useCallback(async (work, note = '') => {
    setBusy(true)
    setActionError('')
    setNotice('')
    try {
      await work()
      await load()
      onChanged()
      if (note) setNotice(note)
      return true
    } catch (error) {
      setActionError(message(error, 'The change did not go through.'))
      return false
    } finally {
      setBusy(false)
    }
  }, [load, onChanged])

  function retry() {
    setLoadError('')
    load().catch(error => setLoadError(message(error, 'Could not load feeds.')))
  }

  if (subs === null) {
    return loadError
      ? <section aria-label="Feeds"><p role="alert">{loadError} <button onClick={retry}>Try again</button></p></section>
      : <section aria-label="Feeds"><p role="status">Loading feeds…</p></section>
  }
  return <div className="feeds">
    {actionError && <p role="alert">{actionError}</p>}
    {notice && <p role="status">{notice}</p>}
    <SubscribeForm folders={folders} busy={busy} act={act} />
    <OPMLTools busy={busy} act={act} />
    <FolderSection folders={folders} busy={busy} act={act} />
    <SubscriptionSection subs={subs} folders={folders} busy={busy} act={act} />
    <MuteSection mutes={mutes} busy={busy} act={act} />
  </div>
}

function SubscribeForm({folders, busy, act}: {folders: Folder[]; busy: boolean; act: Act}) {
  const [draft] = useState(readDraft)
  const [url, setURL] = useState(draft.url)
  const [title, setTitle] = useState(draft.title)
  const [folderID, setFolderID] = useState(draft.folderID)
  const [cadence, setCadence] = useState(draft.cadence)
  const [extract, setExtract] = useState(draft.extract)
  const [isPrivate, setPrivate] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [headerText, setHeaderText] = useState('')
  const [urlError, setUrlError] = useState('')
  const [error, setError] = useState('')
  const skipSave = useRef(false)

  useEffect(() => {
    if (skipSave.current) { skipSave.current = false; return }
    try {
      sessionStorage.setItem(DRAFT_KEY, JSON.stringify({url, title, folderID, cadence, extract}))
    } catch { /* Storage can be full or disabled; the form still works. */ }
  }, [url, title, folderID, cadence, extract])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalized = normalizeFeedURLInput(url)
    if (!normalized) {
      setUrlError(URL_ERROR)
      return
    }
    const headers = parseCredentialHeaders(headerText)
    if (headers === null) {
      setError('Write extra headers as one “Name: value” pair per line.')
      return
    }
    let credentials: SubscribeBody['credentials']
    if (isPrivate) {
      if (!username && !password && Object.keys(headers).length === 0) {
        setError('A private feed needs a username and password, or at least one header.')
        return
      }
      credentials = {username, password, ...(Object.keys(headers).length ? {headers} : {})}
    }
    setUrlError('')
    setError('')
    const body: SubscribeBody = {
      url: normalized, cadence, op_id: newOp(),
      ...(title.trim() ? {title: title.trim()} : {}),
      ...(folderID ? {folder_id: folderID} : {}),
      ...(extract ? {extract} : {}),
      ...(credentials ? {credentials} : {}),
    }
    if (await act(() => subscribe(body), 'Subscribed. The first fetch is on its way.')) {
      // Clear the draft before resetting the fields so the save effect does
      // not write the emptied form back to storage.
      skipSave.current = true
      try { sessionStorage.removeItem(DRAFT_KEY) } catch { /* Storage can be disabled; the form still works. */ }
      setURL('')
      setTitle('')
      setFolderID(0)
      setCadence('today')
      setExtract('')
      setUrlError('')
      setError('')
      setUsername('')
      setPassword('')
      setHeaderText('')
    }
  }

  return <section id="subscribe" className="card" aria-labelledby="subscribe-heading"><h3 id="subscribe-heading">Subscribe</h3>
    <form onSubmit={submit}>
      <label>Feed or site URL<input id="subscribe-url" type="url" value={url}
        onChange={e => {
          const next = e.target.value
          setURL(next)
          if (urlError && (next.trim() === '' || normalizeFeedURLInput(next) !== null)) setUrlError('')
        }}
        onBlur={() => { if (url.trim() !== '' && normalizeFeedURLInput(url) === null) setUrlError(URL_ERROR) }}
        placeholder="https://example.com/feed.xml" required inputMode="url" autoComplete="url" spellCheck={false}
        aria-invalid={urlError ? true : undefined}
        aria-describedby={urlError ? 'subscribe-url-hint subscribe-url-error' : 'subscribe-url-hint'} /></label>
      <p id="subscribe-url-hint" className="hint">Paste a feed or a site address; Earnest finds the feed.</p>
      <label>Title (optional)<input value={title} onChange={e => setTitle(e.target.value)} placeholder="Leave empty to use the feed’s own title" /></label>
      <label>Folder<select value={folderID} onChange={e => setFolderID(Number(e.target.value))}><option value={0}>No folder</option>{folders.map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label>
      <fieldset><legend>Cadence</legend>{CADENCES.map(option => <label key={option.value}><input type="radio" name="cadence" checked={cadence === option.value} onChange={() => setCadence(option.value)} /> {option.label}: {option.hint}</label>)}</fieldset>
      <label>Full text<select value={extract} onChange={e => setExtract(e.target.value)}>{EXTRACT_MODES.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
      <details><summary>Private feed credentials</summary>
        <label><input type="checkbox" checked={isPrivate} onChange={e => setPrivate(e.target.checked)} /> This feed needs credentials</label>
        {isPrivate && <>
          <label>Username<input value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" /></label>
          <label>Password<input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="off" /></label>
          <label>Extra headers (optional)<textarea value={headerText} onChange={e => setHeaderText(e.target.value)} placeholder={'Authorization: Bearer …'} rows={2} /></label>
          <p>Credentials are stored encrypted and are never shown again.</p>
        </>}
      </details>
      <button disabled={busy}>Subscribe</button>
      {urlError && <p id="subscribe-url-error" role="alert">{urlError}</p>}
      {error && <p role="alert">{error}</p>}
    </form>
  </section>
}

function OPMLTools({busy, act}: {busy: boolean; act: Act}) {
  const [file, setFile] = useState<File | null>(null)
  const [report, setReport] = useState<OPMLResult[] | null>(null)
  const [error, setError] = useState('')
  const [working, setWorking] = useState(false)

  async function upload() {
    if (!file) return
    setWorking(true)
    setError('')
    try {
      const results = await importOPML(await readFileText(file))
      setReport(results ?? [])
      await act(async () => undefined, 'OPML imported.')
    } catch (cause) {
      setError(message(cause, 'Could not import that OPML file.'))
    } finally {
      setWorking(false)
    }
  }

  async function download() {
    setWorking(true)
    setError('')
    try {
      const text = await exportOPML()
      const url = URL.createObjectURL(new window.Blob([text], {type: 'text/x-opml'}))
      const link = document.createElement('a')
      link.href = url
      link.download = 'earnest-subscriptions.opml'
      link.click()
      // Revoke after the download starts; revoking synchronously can abort it in Safari.
      window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (cause) {
      setError(message(cause, 'Could not export OPML.'))
    } finally {
      setWorking(false)
    }
  }

  const summary = report && summarizeImport(report)
  return <section className="card" aria-labelledby="opml-heading"><h3 id="opml-heading">OPML</h3>
    <label>Import a subscription file<input type="file" accept=".opml,.xml,text/x-opml,application/xml,text/xml" onChange={e => setFile(e.target.files?.[0] ?? null)} /></label>
    <button disabled={busy || working || !file} onClick={() => { void upload() }}>Import</button>
    <button disabled={busy || working} onClick={() => { void download() }}>Export subscriptions</button>
    {error && <p role="alert">{error}</p>}
    {summary && <div role="status">
      <p>{summary.feeds} {summary.feeds === 1 ? 'feed' : 'feeds'} subscribed{summary.folders > 0 && `, ${summary.folders} ${summary.folders === 1 ? 'folder' : 'folders'} created`}{summary.deferred > 0 && '. Their first fetches run in the background.'}</p>
      {summary.failed.length > 0 && <p role="alert">{summary.failed.length} could not be imported: {summary.failed.join('; ')}</p>}
    </div>}
  </section>
}

function FolderSection({folders, busy, act}: {folders: Folder[]; busy: boolean; act: Act}) {
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<number | null>(null)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')

  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!name.trim()) {
      setError('Give the folder a name.')
      return
    }
    setError('')
    if (await act(() => createFolder(name.trim()), 'Folder created.')) setName('')
  }

  return <section className="card" aria-labelledby="folders-heading"><h3 id="folders-heading">Folders</h3>
    <form onSubmit={add}>
      <label>New folder<input id="folder-name" value={name} onChange={e => setName(e.target.value)} required
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? 'folder-name-error' : undefined} /></label>
      <button disabled={busy}>Add folder</button>
      {error && <p id="folder-name-error" role="alert">{error}</p>}
    </form>
    {folders.length === 0 && <p>No folders yet.</p>}
    <ul>{folders.map(folder => <li key={folder.id}>
      {editing === folder.id
        ? <form onSubmit={event => { event.preventDefault(); const renamed = draft.trim(); if (renamed) void act(() => renameFolder(folder.id, renamed), 'Folder renamed.').then(ok => { if (ok) setEditing(null) }) }}>
            <label>Name<input value={draft} onChange={e => setDraft(e.target.value)} required /></label>
            <button disabled={busy}>Save</button><button type="button" onClick={() => setEditing(null)}>Cancel</button>
          </form>
        : <>{folder.name} <button disabled={busy} onClick={() => { setEditing(folder.id); setDraft(folder.name) }}>Rename</button>
          <button disabled={busy} onClick={() => { if (window.confirm(`Delete folder “${folder.name}”? Its feeds move to No folder.`)) void act(() => deleteFolder(folder.id), 'Folder deleted.') }}>Delete</button></>}
    </li>)}</ul>
  </section>
}

function SubscriptionSection({subs, folders, busy, act}: {subs: Subscription[]; folders: Folder[]; busy: boolean; act: Act}) {
  const groups = groupByFolder(subs, folders)
  if (subs.length === 0) return <section className="card" aria-labelledby="subscriptions-heading"><h3 id="subscriptions-heading">Subscriptions</h3><p>No subscriptions yet.</p></section>
  return <section className="card" aria-labelledby="subscriptions-heading"><h3 id="subscriptions-heading">Subscriptions</h3>
    {groups.map(group => group.subscriptions.length === 0 ? null : <div key={group.folder?.id ?? 0}>
      {group.folder && <h4>{group.folder.name}</h4>}
      <ul>{group.subscriptions.map(sub => <SubscriptionRow key={sub.id} sub={sub} folders={folders} busy={busy} act={act} />)}</ul>
    </div>)}
  </section>
}

function when(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString()
}

function SubscriptionRow({sub, folders, busy, act}: {sub: Subscription; folders: Folder[]; busy: boolean; act: Act}) {
  const [title, setTitle] = useState(sub.title)
  // Adopt an external title change unless the user has an unsaved draft.
  // Tracking a dirty flag rather than focus keeps the draft when a click
  // elsewhere (such as Refresh now) moves focus away from the field.
  const [dirty, setDirty] = useState(false)
  const [seenTitle, setSeenTitle] = useState(sub.title)
  if (sub.title !== seenTitle) {
    setSeenTitle(sub.title)
    if (!dirty) setTitle(sub.title)
  }
  const explanation = explainHealth(sub.health)
  const folder = folders.find(candidate => candidate.id === sub.folder_id)
  const cadence = CADENCES.find(option => option.value === sub.cadence)
  const name = sub.title || sub.url
  const patch = (body: Omit<SubscriptionPatch, 'op_id'>, note: string) => act(() => patchSubscription(sub.id, {...body, op_id: newOp()}), note)
  return <li id={`subscription-${sub.id}`}>
    <div>
      <Link className="feed-title" to={`/feeds/${sub.id}`}>{name}</Link>
      <p className="muted feed-meta">
        {cadence?.label ?? sub.cadence} · {folder?.name ?? 'No folder'}
        {sub.private && ' · private'}
        {isFirehose(sub) && ' · firehose'}
        {' · '}
        {healthSummary(sub)}
      </p>
    </div>
    <details className="feed-edit" open={!!explanation}>
      {/* The visible label stays short; the accessible name names the feed. */}
      <summary aria-label={`Edit ${name}`}>Edit</summary>
      <div className="feed-edit-body">
        <p className="feed-site"><a href={sub.site_url || sub.url} target="_blank" rel="noopener noreferrer">Site</a></p>
        <form onSubmit={event => { event.preventDefault(); void patch({title: title.trim()}, 'Title saved.').then(saved => { if (saved) setDirty(false) }) }}>
          <label>Title<input value={title} onChange={e => { setTitle(e.target.value); setDirty(true) }} /></label>
          <button disabled={busy}>Save title</button>
        </form>
        <label>Cadence<select value={sub.cadence} disabled={busy} onChange={e => { void patch({cadence: e.target.value}, 'Cadence updated.') }}>{CADENCES.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
        <label>Folder<select value={sub.folder_id} disabled={busy} onChange={e => { void patch({folder_id: Number(e.target.value)}, 'Folder updated.') }}><option value={0}>No folder</option>{folders.map(folder => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select></label>
        <label>Full text<select value={sub.extract} disabled={busy} onChange={e => { void patch({extract: e.target.value}, 'Full-text mode updated.') }}>{EXTRACT_MODES.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
        {sub.health.last_attempt_at && <p>
          {`Tried ${when(sub.health.last_attempt_at)}.`}
          {sub.health.last_success_at && ` Succeeded ${when(sub.health.last_success_at)}.`}
          {sub.health.next_fetch_at && ` Next fetch ${when(sub.health.next_fetch_at)}.`}
          {sub.health.http_status > 0 && ` HTTP ${sub.health.http_status}.`}
          {sub.health.posts_per_day > 0 && ` About ${Math.round(sub.health.posts_per_day * 10) / 10} posts a day.`}
        </p>}
        <Credentials sub={sub} busy={busy} patch={patch} />
        <button disabled={busy} onClick={() => { void act(() => refreshSubscription(sub.id), 'Refresh requested.') }}>Refresh now</button>
        <button disabled={busy} onClick={() => { if (window.confirm(`Unsubscribe from “${name}”?`)) void act(() => unsubscribe(sub.id), 'Unsubscribed.') }}>Unsubscribe</button>
      </div>
    </details>
  </li>
}

// The summary line stays calm: one muted line for cadence, folder and health.
// The word "Failing:" keeps a problem from being signalled by colour alone.
function healthSummary(sub: Subscription) {
  if (healthPending(sub)) return <>Waiting for the first fetch.</>
  const explanation = explainHealth(sub.health)
  if (explanation) {
    return <span className="feed-health-problem"><strong>Failing:</strong> {explanation}{sub.health.consecutive_failures > 1 && ` It has failed ${sub.health.consecutive_failures} times in a row.`}</span>
  }
  const posts = sub.health.posts_per_day > 0 ? ` · about ${Math.round(sub.health.posts_per_day * 10) / 10} posts a day` : ''
  return <>Healthy{posts}.</>
}

function Credentials({sub, busy, patch}: {sub: Subscription; busy: boolean; patch: (body: Omit<SubscriptionPatch, 'op_id'>, note: string) => Promise<boolean>}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [headerText, setHeaderText] = useState('')
  const [error, setError] = useState('')

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const headers = parseCredentialHeaders(headerText)
    if (headers === null) {
      setError('Write extra headers as one “Name: value” pair per line.')
      return
    }
    if (!username && !password && Object.keys(headers).length === 0) {
      setError('Enter a username and password, or at least one header.')
      return
    }
    setError('')
    if (await patch({credentials: {username, password, ...(Object.keys(headers).length ? {headers} : {})}}, 'Credentials saved.')) {
      setUsername('')
      setPassword('')
      setHeaderText('')
    }
  }

  return <details><summary>Credentials{sub.private ? ' (set)' : ''}</summary>
    <p>{sub.private ? 'This feed fetches with stored credentials.' : 'This feed fetches without credentials.'} Credentials are stored encrypted and are never shown again.</p>
    <form onSubmit={save}>
      <label>Username<input value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" /></label>
      <label>Password<input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="off" /></label>
      <label>Extra headers (optional)<textarea value={headerText} onChange={e => setHeaderText(e.target.value)} rows={2} /></label>
      <button disabled={busy}>{sub.private ? 'Replace credentials' : 'Set credentials'}</button>
      {error && <p role="alert">{error}</p>}
    </form>
    {sub.private && <button disabled={busy} onClick={() => { if (window.confirm('Clear the stored credentials? The feed becomes public.')) void patch({credentials_set: true}, 'Credentials cleared.') }}>Clear credentials</button>}
  </details>
}

function MuteSection({mutes, busy, act}: {mutes: Mute[]; busy: boolean; act: Act}) {
  const [kind, setKind] = useState<'keyword' | 'author'>('keyword')
  const [value, setValue] = useState('')
  const [error, setError] = useState('')

  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!value.trim()) {
      setError('Give the mute a value.')
      return
    }
    setError('')
    if (await act(() => createMute(kind, value.trim()), 'Mute added.')) setValue('')
  }

  const active = mutes.filter(mute => !mute.deleted)
  return <section className="card" aria-labelledby="mutes-heading"><h3 id="mutes-heading">Mutes</h3>
    <form onSubmit={add}>
      <label>Kind<select value={kind} onChange={e => setKind(e.target.value as 'keyword' | 'author')}><option value="keyword">Keyword in the title</option><option value="author">Author</option></select></label>
      <label>Value<input id="mute-value" value={value} onChange={e => setValue(e.target.value)} required
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? 'mute-value-error' : undefined} /></label>
      <button disabled={busy}>Add mute</button>
      {error && <p id="mute-value-error" role="alert">{error}</p>}
    </form>
    {active.length === 0 && <p>No mutes. Everything reaches the river.</p>}
    <ul>{active.map(mute => <li key={mute.id}>{mute.kind === 'keyword' ? 'Title contains' : 'Author'} “{mute.value}” <button disabled={busy} onClick={() => { void act(() => deleteMute(mute.id), 'Mute removed.') }}>Remove</button></li>)}</ul>
  </section>
}
