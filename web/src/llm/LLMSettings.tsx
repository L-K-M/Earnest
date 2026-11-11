import {useCallback, useEffect, useRef, useState} from 'react'
import {request, RequestError} from '../request'
import {useBlocker} from '../router'
import {
  LLM_DEFAULT_BASE_URLS,
  LLM_PROVIDER_KINDS,
  LLM_TOKEN_FIELDS,
  draftFromSettings,
  inputFromDraft,
  validateDraft,
  validateModel,
  validateProvider,
  type LLMProviderKind,
  type LLMSettings as LLMSettingsData,
  type LLMSettingsDraft,
  type LLMTokenField,
  type LLMTask,
  type ModelDraft,
  type ProviderDraft,
} from './model'

const newOp = () => crypto.randomUUID()
const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback

type ProviderFormValues = {
  name: string
  kind: LLMProviderKind
  base_url: string
  api_key: string
  clear_api_key: boolean
  monthly_limit_usd: string
  timeout_seconds: string
  token_field: LLMTokenField
}

type ModelFormValues = {
  name: string
  input_per_1m: string
  output_per_1m: string
  embedding_per_1m: string
  dimensions: string
  truncatable: boolean
}

function emptyProvider(): ProviderDraft {
  return {
    name: '',
    kind: 'openai',
    base_url: LLM_DEFAULT_BASE_URLS.openai,
    has_api_key: false,
    monthly_limit_usd: '0',
    timeout_seconds: '60',
    token_field: 'max_completion_tokens',
    api_key: '',
    clear_api_key: false,
    original_name: null,
  }
}

// LLMSettings is the server-wide service configuration section. The server
// owns the only copy; this form edits a draft and Save replaces the whole
// provider, task and model configuration in one request.
export function LLMSettings() {
  const [loaded, setLoaded] = useState<LLMSettingsData | null>(null)
  const [draft, setDraft] = useState<LLMSettingsDraft | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [providerForm, setProviderForm] = useState<{mode: 'add'} | {mode: 'edit'; index: number} | null>(null)
  const [modelForm, setModelForm] = useState(false)
  // The op_id of the request whose outcome is uncertain, so a retry of the
  // identical body reuses it instead of risking a second apply.
  const attempt = useRef<{body: string; opID: string} | null>(null)

  const load = useCallback(async (notice: string) => {
    setBusy(true)
    setProblems([])
    setNotice('')
    try {
      const next = await request<LLMSettingsData>('/v1/settings/llm')
      attempt.current = null
      setLoaded(next)
      setDraft(draftFromSettings(next))
      setProviderForm(null)
      setModelForm(false)
      setNotice(notice)
    } catch (error) {
      setProblems([message(error, 'Could not load the LLM services.')])
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => { void load('') }, [load])

  const loadedDraft = loaded ? draftFromSettings(loaded) : null
  const dirty = draft !== null && loadedDraft !== null
    && JSON.stringify(inputFromDraft(draft)) !== JSON.stringify(inputFromDraft(loadedDraft))
  const hasOpenEditor = providerForm !== null || modelForm
  useBlocker(dirty || hasOpenEditor, 'Leave without saving your LLM service settings?')

  function setTask(index: number, patch: Partial<LLMTask>) {
    if (!draft) return
    setDraft({...draft, tasks: draft.tasks.map((task, i) => i === index ? {...task, ...patch} : task)})
    setNotice('')
  }

  function setModel(index: number, patch: Partial<ModelDraft>) {
    if (!draft) return
    setDraft({...draft, models: draft.models.map((model, i) => i === index ? {...model, ...patch} : model)})
    setNotice('')
  }

  function addProvider(values: ProviderFormValues) {
    if (!draft) return
    const provider: ProviderDraft = {...values, has_api_key: false, original_name: null}
    const found = validateProvider(provider, undefined, new Set(draft.providers.map(item => item.name)))
    if (found.length) { setProblems(found); return }
    setDraft({...draft, providers: [...draft.providers, provider]})
    setProviderForm(null)
    setProblems([])
    setNotice('')
  }

  function updateProvider(index: number, values: ProviderFormValues) {
    if (!draft) return
    const providers = draft.providers.map((provider, i) => i === index
      ? {...values, has_api_key: provider.has_api_key, original_name: provider.original_name}
      : provider)
    const provider = providers[index]
    const original = loaded?.providers?.find(item => item.name === provider.original_name)
    const others = new Set(providers.filter((_, i) => i !== index).map(item => item.name))
    const found = validateProvider(provider, original, others)
    if (found.length) { setProblems(found); return }
    setDraft({...draft, providers})
    setProviderForm(null)
    setProblems([])
    setNotice('')
  }

  function removeProvider(index: number) {
    if (!draft) return
    const provider = draft.providers[index]
    const off = draft.tasks.filter(task => task.provider === provider.name).map(task => task.task)
    const note = off.length ? ` Removing it turns off ${off.join(', ')}.` : ''
    if (!window.confirm(`Remove provider ${provider.name}?${note}`)) return
    setDraft({
      ...draft,
      providers: draft.providers.filter((_, i) => i !== index),
      tasks: draft.tasks.map(task => task.provider === provider.name ? {task: task.task, provider: '', model: ''} : task),
    })
    if (providerForm?.mode === 'edit' && providerForm.index === index) setProviderForm(null)
    setProblems([])
    setNotice('')
  }

  function addModel(values: ModelFormValues) {
    if (!draft) return
    const model: ModelDraft = {...values}
    const found = validateModel(model, new Set(draft.models.map(item => item.name)))
    if (found.length) { setProblems(found); return }
    setDraft({...draft, models: [...draft.models, model]})
    setModelForm(false)
    setProblems([])
    setNotice('')
  }

  function removeModel(index: number) {
    if (!draft) return
    const model = draft.models[index]
    const off = draft.tasks.filter(task => task.model === model.name).map(task => task.task)
    const note = off.length ? ` Tasks using it are turned off: ${off.join(', ')}.` : ''
    if (!window.confirm(`Remove model ${model.name}?${note}`)) return
    setDraft({
      ...draft,
      models: draft.models.filter((_, i) => i !== index),
      tasks: draft.tasks.map(task => task.model === model.name ? {task: task.task, provider: '', model: ''} : task),
    })
    setProblems([])
    setNotice('')
  }

  function save(event: React.FormEvent) {
    event.preventDefault()
    if (!draft || !loaded) return
    const found = validateDraft(draft, loaded)
    setProblems(found)
    setNotice('')
    if (found.length) return
    const body = inputFromDraft(draft)
    const bodyText = JSON.stringify(body)
    const opID = attempt.current && attempt.current.body === bodyText ? attempt.current.opID : newOp()
    attempt.current = {body: bodyText, opID}
    setBusy(true)
    void (async () => {
      try {
        const next = await request<LLMSettingsData>('/v1/settings/llm', {method: 'PUT', body: JSON.stringify({...body, op_id: opID})})
        attempt.current = null
        setLoaded(next)
        setDraft(draftFromSettings(next))
        setProviderForm(null)
        setModelForm(false)
        setNotice('LLM services saved.')
      } catch (error) {
        if (error instanceof RequestError && error.status === 409) {
          // A conflict means the saved configuration moved on. Keep the draft
          // and make the reload explicit; never overwrite what changed.
          attempt.current = null
          setProblems(['These settings changed after you loaded them. Reload to get the current version; your edits are kept and nothing was overwritten.'])
        } else {
          setProblems([message(error, 'The settings did not save. Your edits are kept.')])
        }
      } finally {
        setBusy(false)
      }
    })()
  }

  return <section aria-labelledby="llm-services-heading">
    <h2 id="llm-services-heading">LLM services</h2>
    <p>These settings are shared by the whole server and affect every account. API keys are encrypted on the server and are never sent back to the browser. Every task stays off until it has both a provider and a model, so nothing leaves the server by default.</p>
    {loaded && loaded.revision === 0 && <p className="hint">The server is still using its environment defaults. Saving stores this configuration in the database; from then on the saved configuration takes precedence and survives restarts.</p>}
    {problems.length > 0 && <div role="alert" className="form-error"><ul>{problems.map((problem, i) => <li key={i}>{problem}</li>)}</ul></div>}
    {notice && <p role="status">{notice}</p>}
    {busy && <p role="status" className="muted">Working…</p>}
    {!loaded && <div className="button-row"><button className="primary" disabled={busy} onClick={() => void load('LLM services loaded.')}>{busy ? 'Loading…' : 'Load'}</button></div>}
    {loaded && draft && <form onSubmit={save} onChange={() => setNotice('')}>
      <div className="button-row">
        <button className="primary" disabled={busy || !dirty || hasOpenEditor}>Save</button>
        <button type="button" disabled={busy} onClick={() => { if ((!dirty && !hasOpenEditor) || window.confirm('Discard your unsaved LLM service changes and reload?')) void load('LLM services reloaded.') }}>Reload</button>
        <button type="button" disabled={busy || providerForm !== null} onClick={() => setProviderForm({mode: 'add'})}>Add provider</button>
        {dirty ? <span className="muted">Unsaved changes</span> : <span className="muted">No unsaved changes</span>}
      </div>

      <h3>Providers</h3>
      <p className="hint">Each provider is a named API destination. Point the base URL at any compatible server; a local server on a private address needs EARNEST_OUTBOUND_ALLOW_PRIVATE allowlisting, which this page cannot bypass.</p>
      <ul className="settings-list">
        {draft.providers.map((provider, index) => <li key={index}>
          {providerForm?.mode === 'edit' && providerForm.index === index
            ? <ProviderForm initial={provider} onCancel={() => setProviderForm(null)} onSubmit={values => updateProvider(index, values)} />
            : <>
              <div className="settings-row-head">
                <strong>{provider.name}</strong>
                <span className="chip">{provider.kind}</span>
                {provider.has_api_key ? <span className="chip" data-tone="green">API key saved</span> : <span className="chip" data-tone="amber">No API key</span>}
              </div>
              <p className="settings-row-detail muted">{provider.base_url} · {/^0+(?:\.0+)?$/.test(provider.monthly_limit_usd) ? 'no monthly limit' : `$${provider.monthly_limit_usd} per month`} · {provider.timeout_seconds}s timeout · {provider.token_field}</p>
              <div className="button-row">
                <button type="button" disabled={busy || providerForm !== null} onClick={() => setProviderForm({mode: 'edit', index})}>Edit</button>
                <button type="button" disabled={busy || providerForm !== null} onClick={() => removeProvider(index)}>Remove</button>
              </div>
            </>}
        </li>)}
      </ul>
      {providerForm?.mode === 'add' && <ProviderForm initial={emptyProvider()} onCancel={() => setProviderForm(null)} onSubmit={addProvider} />}

      <h3>Task routing</h3>
      <p className="hint">All seven tasks are always listed. Choose a provider and a model to turn a task on, or set the provider to Disabled to turn it off.</p>
      <ul className="settings-list">
        {draft.tasks.map((task, index) => <li key={index}>
          <div className="settings-row-head"><strong>{task.task}</strong>{!task.provider && <span className="chip">Off</span>}</div>
          <label>Provider
            <select aria-label={`${task.task} provider`} value={task.provider} disabled={busy} onChange={e => {
              const provider = e.target.value
              setTask(index, {provider, model: provider ? task.model : ''})
            }}>
              <option value="">Disabled</option>
              {draft.providers.map(item => <option key={item.name} value={item.name}>{item.name} ({item.kind})</option>)}
            </select>
          </label>
          <label>Model
            <select aria-label={`${task.task} model`} value={task.model} disabled={busy || !task.provider} onChange={e => setTask(index, {model: e.target.value})}>
              <option value="">No model</option>
              {draft.models.map(item => <option key={item.name} value={item.name}>{item.name}</option>)}
            </select>
          </label>
        </li>)}
      </ul>

      <details>
        <summary>Model pricing</summary>
        <p className="hint">Prices are USD per million tokens and must be explicit; use 0 for a local model. Editing one model keeps every other entry and its prices.</p>
        <ul className="settings-list">
          {draft.models.map((model, index) => <li key={index}>
            <div className="settings-row-head"><strong>{model.name || 'Unnamed model'}</strong>{model.truncatable && <span className="chip">truncatable</span>}</div>
            <label>Input price per 1M tokens<input aria-label={`${model.name || 'new model'} input price`} value={model.input_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {input_per_1m: e.target.value})} /></label>
            <label>Output price per 1M tokens<input aria-label={`${model.name || 'new model'} output price`} value={model.output_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {output_per_1m: e.target.value})} /></label>
            <label>Embedding price per 1M tokens<input aria-label={`${model.name || 'new model'} embedding price`} value={model.embedding_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {embedding_per_1m: e.target.value})} /></label>
            <label>Dimensions<input aria-label={`${model.name || 'new model'} dimensions`} type="number" min="0" step="1" value={model.dimensions} disabled={busy} onChange={e => setModel(index, {dimensions: e.target.value})} /></label>
            <label><input aria-label={`${model.name || 'new model'} truncatable`} type="checkbox" checked={model.truncatable} disabled={busy} onChange={e => setModel(index, {truncatable: e.target.checked})} /> Truncatable</label>
            <div className="button-row"><button type="button" disabled={busy} onClick={() => removeModel(index)}>Remove model</button></div>
          </li>)}
        </ul>
        {modelForm
          ? <ModelForm onCancel={() => setModelForm(false)} onAdd={addModel} />
          : <button type="button" disabled={busy} onClick={() => setModelForm(true)}>Add model</button>}
      </details>
    </form>}
  </section>
}

function ProviderForm({initial, onSubmit, onCancel}: {initial: ProviderDraft; onSubmit: (values: ProviderFormValues) => void; onCancel: () => void}) {
  const [name, setName] = useState(initial.name)
  const [kind, setKind] = useState<LLMProviderKind>(initial.kind)
  const [baseURL, setBaseURL] = useState(initial.base_url)
  const [apiKey, setApiKey] = useState(initial.api_key)
  const [clearKey, setClearKey] = useState(initial.clear_api_key)
  const [limit, setLimit] = useState(initial.monthly_limit_usd)
  const [timeoutSeconds, setTimeoutSeconds] = useState(initial.timeout_seconds)
  const [tokenField, setTokenField] = useState<LLMTokenField>(initial.token_field)
  return <div className="llm-provider-form">
    <label>Name<input value={name} onChange={e => setName(e.target.value)} autoCapitalize="none" spellCheck={false} /></label>
    <label>Kind
      <select value={kind} onChange={e => {
        const next = e.target.value as LLMProviderKind
        setKind(next)
        if (baseURL === '' || (Object.values(LLM_DEFAULT_BASE_URLS) as string[]).includes(baseURL)) setBaseURL(LLM_DEFAULT_BASE_URLS[next])
      }}>{LLM_PROVIDER_KINDS.map(option => <option key={option} value={option}>{option}</option>)}</select>
    </label>
    <label>Base URL<input value={baseURL} onChange={e => setBaseURL(e.target.value)} inputMode="url" autoCapitalize="none" spellCheck={false} /></label>
    <p className="hint">The URL must be http(s) with a host and no credentials, query or fragment.</p>
    <label>API key<input type="password" value={apiKey} onChange={e => setApiKey(e.target.value)} autoComplete="new-password" placeholder={initial.has_api_key ? 'Leave blank to keep the saved key' : 'Optional'} /></label>
    {initial.has_api_key
      ? <><p className="hint">An API key is saved. Leave the field blank to keep it, or type a replacement.</p><label><input type="checkbox" checked={clearKey} onChange={e => setClearKey(e.target.checked)} /> Clear the saved API key</label></>
      : <p className="hint">No key is saved. Leave the field blank if the server needs none.</p>}
    <label>Monthly limit (USD)<input value={limit} onChange={e => setLimit(e.target.value)} inputMode="decimal" autoCapitalize="none" spellCheck={false} /></label>
    <p className="hint">0 means no limit; at most 99999999.999999.</p>
    <label>Timeout (seconds)<input type="number" min="1" max="600" step="1" value={timeoutSeconds} onChange={e => setTimeoutSeconds(e.target.value)} /></label>
    <label>Token field<select value={tokenField} onChange={e => setTokenField(e.target.value as LLMTokenField)}>{LLM_TOKEN_FIELDS.map(option => <option key={option} value={option}>{option}</option>)}</select></label>
    <div className="button-row">
      <button type="button" onClick={() => onSubmit({name: name.trim(), kind, base_url: baseURL.trim(), api_key: apiKey, clear_api_key: clearKey, monthly_limit_usd: limit, timeout_seconds: timeoutSeconds, token_field: tokenField})}>Apply provider</button>
      <button type="button" onClick={onCancel}>Cancel</button>
    </div>
  </div>
}

function ModelForm({onAdd, onCancel}: {onAdd: (values: ModelFormValues) => void; onCancel: () => void}) {
  const [name, setName] = useState('')
  const [input, setInput] = useState('')
  const [output, setOutput] = useState('')
  const [embedding, setEmbedding] = useState('')
  const [dimensions, setDimensions] = useState('0')
  const [truncatable, setTruncatable] = useState(false)
  return <div className="llm-model-form">
    <label>Name<input aria-label="New model name" value={name} onChange={e => setName(e.target.value)} autoCapitalize="none" spellCheck={false} /></label>
    <label>Input price per 1M tokens<input aria-label="New model input price" value={input} onChange={e => setInput(e.target.value)} inputMode="decimal" autoCapitalize="none" spellCheck={false} /></label>
    <label>Output price per 1M tokens<input aria-label="New model output price" value={output} onChange={e => setOutput(e.target.value)} inputMode="decimal" autoCapitalize="none" spellCheck={false} /></label>
    <label>Embedding price per 1M tokens<input aria-label="New model embedding price" value={embedding} onChange={e => setEmbedding(e.target.value)} inputMode="decimal" autoCapitalize="none" spellCheck={false} /></label>
    <p className="hint">Set all three rates explicitly; use 0 for a local model.</p>
    <label>Dimensions<input aria-label="New model dimensions" type="number" min="0" step="1" value={dimensions} onChange={e => setDimensions(e.target.value)} /></label>
    <label><input type="checkbox" checked={truncatable} onChange={e => setTruncatable(e.target.checked)} /> Truncatable</label>
    <div className="button-row">
      <button type="button" onClick={() => onAdd({name: name.trim(), input_per_1m: input, output_per_1m: output, embedding_per_1m: embedding, dimensions, truncatable})}>Apply model</button>
      <button type="button" onClick={onCancel}>Cancel</button>
    </div>
  </div>
}
