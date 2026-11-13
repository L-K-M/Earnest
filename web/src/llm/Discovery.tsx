import {useEffect, useRef, useState} from 'react'
import {request, RequestError} from '../request'
import {
  MAX_MODELS,
  capabilityFor,
  draftFromDiscovered,
  geometryIncompatible,
  missingApplicableFields,
  modelKey,
  providerInput,
  sameDiscoveryProvider,
  sameModel,
  validateModel,
  validateMoney,
  validateDimensions,
  type DiscoveredModel,
  type LLMCapability,
  type LLMModels,
  type LLMProviderInput,
  type LLMModelsCheck,
  type LLMModelsCheckInput,
  type ModelDraft,
  type ProviderDraft,
} from './model'

const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback

// Catalogue results live only in the settings session's memory, keyed by the
// exact provider values and settings revision. They are never written to
// storage. Closing and reopening the picker reuses them; remounting the
// settings tree (a new session or account) starts with an empty cache.
function catalogueKey(revision: number, provider: ProviderDraft): string {
  return JSON.stringify({revision, provider: {
    name: provider.name, kind: provider.kind, base_url: provider.base_url,
    api_key: provider.api_key, clear_api_key: provider.clear_api_key,
    has_api_key: provider.has_api_key, original_name: provider.original_name,
    monthly_limit_usd: provider.monthly_limit_usd, timeout_seconds: provider.timeout_seconds,
    token_field: provider.token_field,
  }})
}

// ModelPicker is the provider-scoped selector that replaces the old
// fetch/import workflow. Opening it fetches the catalogue automatically;
// results stay in component memory, bound to the exact provider values,
// settings revision and session they were fetched with. Nothing is written
// to storage. Selecting a ready model hands a complete draft to the parent
// immediately; a model with missing applicable metadata opens an inline form
// in this pane, where the user supplies it explicitly or marks it free.
export function ModelPicker({provider, revision, capability: requestedCapability, taskLabel, models, room, cache, onPick, onGeometry, onConflict, onClose, disabled = false}: {
  disabled?: boolean
  provider: ProviderDraft
  revision: number
  capability: LLMCapability | null
  taskLabel: string | null
  models: ModelDraft[]
  room: number
  cache: Map<string, DiscoveredModel[]>
  onPick: (model: ModelDraft) => void
  onGeometry?: (patch: {provider: string; name: string; dimensions?: number; supports_custom_dimensions?: boolean}) => void
  onConflict?: () => void
  onClose: () => void
}) {
  // Scope and cache keys contain credentials only in component memory, never
  // in storage or rendered attributes. Each provider edit gets a distinct generation. The
  // cache itself belongs to the settings session, so results cannot cross an
  // account change.
  const scope = useRef({provider: {...provider}, revision, generation: 0})
  if (scope.current.revision !== revision || !sameDiscoveryProvider(scope.current.provider, provider)) {
    scope.current = {provider: {...provider}, revision, generation: scope.current.generation + 1}
  }
  const generation = scope.current.generation
  const requestID = useRef(Symbol())
  const cacheRef = useRef(cache)
  cacheRef.current = cache
  const [results, setResults] = useState<{generation: number; models: DiscoveredModel[]} | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState<ModelDraft | null>(null)
  const [manualID, setManualID] = useState('')
  const [manualOpen, setManualOpen] = useState(false)
  const [browsingCapability, setBrowsingCapability] = useState<LLMCapability>('chat')
  const capability = requestedCapability ?? browsingCapability

  useEffect(() => {
    requestID.current = Symbol()
    setResults(null)
    setDraft(null)
    setError('')
    setBusy(false)
    if (generation > 0) setNotice('The provider details or settings revision changed; the earlier results were discarded. Fetch again to browse the new destination.')
  }, [generation])

  async function fetchModels() {
    const launch = scope.current
    const id = Symbol()
    requestID.current = id
    const current = () => scope.current === launch && requestID.current === id
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const found = await request<LLMModels>('/v1/settings/llm/models', {
        method: 'POST',
        body: JSON.stringify({revision: launch.revision, provider: providerInput(launch.provider)}),
      })
      if (!current()) return
      const models = found.models ?? []
      cacheRef.current.set(catalogueKey(launch.revision, launch.provider), models)
      setResults({generation: launch.generation, models})
    } catch (problem) {
      if (!current()) return
      if (problem instanceof RequestError && problem.status === 409) onConflict?.()
      setError(problem instanceof RequestError && problem.status === 404
        ? 'This server does not support model discovery; add models manually.'
        : problem instanceof RequestError && problem.status === 409
          ? 'The saved settings changed since this page loaded. Reload, then fetch again.'
          : message(problem, 'Could not fetch the model catalogue.'))
    } finally {
      if (current()) setBusy(false)
    }
  }

  // Fetch once per picker mount. The cache is stable for the settings
  // session; the scope ref and request id guard stale responses. A ref keeps
  // the effect from depending on the fetch closure, which changes every render.
  const fetchRef = useRef(fetchModels)
  fetchRef.current = fetchModels
  useEffect(() => {
    const launch = scope.current
    const cached = cacheRef.current.get(catalogueKey(launch.revision, launch.provider))
    if (cached) {
      setResults({generation: launch.generation, models: cached})
      return () => { requestID.current = Symbol() }
    }
    void fetchRef.current()
    return () => { requestID.current = Symbol() }
  }, [])

  function choose(model: DiscoveredModel) {
    if (disabled || !results || results.generation !== generation) return
    const base = draftFromDiscovered(model, provider.name, capability)
    // Identity is provider+name. A legacy global record is a different entry;
    // selecting this provider's catalogue model creates its scoped record and
    // leaves the global record's saved prices untouched.
    const existing = models.find(item => sameModel(item, base))
    if (existing) {
      onPick(existing)
      setNotice(`${model.id} is already configured; its saved prices are kept.`)
      return
    }
    if (missingApplicableFields(model, capability).length > 0 || validateModel(base, new Set()).length > 0) {
      setDraft(base)
      setError('')
      setNotice('')
      return
    }
    if (room < 1) {
      setError(`The configured-model table is full at ${MAX_MODELS} entries; remove one before adding.`)
      return
    }
    onPick(base)
    setNotice(`${model.id} added to the draft. Save to persist it.`)
  }

  function openManual() {
    const id = manualID.trim()
    if (!id) { setError('Enter a model ID to add manually.'); return }
    setError('')
    const base = draftFromDiscovered({id, name: id}, provider.name, capability)
    const existing = models.find(item => sameModel(item, base))
    if (existing) {
      onPick(existing)
      setNotice(`${id} is already configured; its saved prices are kept.`)
      setManualOpen(false)
      setManualID('')
      return
    }
    setDraft(base)
    setManualOpen(false)
    setManualID('')
    setNotice('')
  }

  const needle = query.trim().toLowerCase()
  const catalogue = results?.generation === generation ? results.models : []
  const searched = needle ? catalogue.filter(model => model.id.toLowerCase().includes(needle) || model.name.toLowerCase().includes(needle)) : catalogue
  // A task context hides only what the provider says cannot do the work;
  // unknown capability stays visible and labelled.
  const hidden = capability ? searched.filter(model => capabilityFor(model, capability) === false) : []
  const incompatible = searched.filter(model => geometryIncompatible(model, capability))
  const shown = searched.filter(model => !hidden.includes(model) && !incompatible.includes(model))
    .sort((a, b) => capability ? Number(capabilityFor(b, capability) === true) - Number(capabilityFor(a, capability) === true) : 0)

  return <fieldset disabled={disabled} style={{border: 0, padding: 0, margin: 0}} className="llm-picker" aria-label={`Choose a model from ${provider.name || 'the new provider'}`}>
    <p className="hint">Choose a model for this provider. Published prices are filled in automatically.</p>
    {!requestedCapability && <label>Use for<select aria-label="Model use" value={browsingCapability} onChange={e => {
      setBrowsingCapability(e.target.value as LLMCapability)
      setDraft(null)
    }}><option value="chat">Text</option><option value="embedding">Embedding</option></select></label>}
    {busy && <p role="status" className="muted">Fetching models…</p>}
    {error && <div role="alert" className="form-error"><ul><li>{error}</li></ul></div>}
    {notice && <p role="status">{notice}</p>}
    {results && <>
      <label>Search models<input aria-label="Search models" value={query} onChange={e => setQuery(e.target.value)} autoCapitalize="none" spellCheck={false} placeholder="Filter by ID or name" /></label>
      {!shown.length && <p className="muted">{searched.length ? 'No compatible models match the search.' : 'No catalogue entries match the search.'}</p>}
      <ul className="llm-picker-list">
        {shown.map(model => {
          const base = draftFromDiscovered(model, provider.name, capability)
          const isConfigured = models.some(item => sameModel(item, base))
          const missing = missingApplicableFields(model, capability)
          return <li key={model.id}>
            <div className="llm-picker-row">
              <button type="button" className="llm-picker-select" onClick={() => choose(model)} aria-label={`Select ${model.id}${missing.length ? `, missing ${missing.join(', ')}` : ''}`}>
                <span className="llm-picker-name"><strong>{model.name === model.id ? model.id : `${model.name} (${model.id})`}</strong></span>
                <span className="llm-picker-chips">
                  {isConfigured && <span className="chip" data-tone="green">configured</span>}
                  {model.chat === true && <span className="chip">chat</span>}
                  {model.embedding === true && <span className="chip">embedding</span>}
                  {model.chat === undefined && model.embedding === undefined && <span className="chip" data-tone="amber">capabilities unknown</span>}
                  {capability === 'embedding' && model.dimensions !== undefined && model.dimensions > 1024 && model.supports_custom_dimensions === undefined && !model.truncatable && <span className="chip" data-tone="amber">resizing support unknown</span>}
                  {missing.length > 0 && <span className="chip" data-tone="amber">missing {missing.join(', ')}</span>}
                </span>
                <CatalogueDetail model={model} />
              </button>
            </div>
          </li>
        })}
      </ul>
      {hidden.length > 0 && <p className="hint">{hidden.length} catalogue {hidden.length === 1 ? 'entry is' : 'entries are'} hidden because the provider says {hidden.length === 1 ? 'it' : 'they'} cannot do {capability} work.</p>}
      {incompatible.length > 0 && <p className="hint">{incompatible.length} catalogue {incompatible.length === 1 ? 'entry is' : 'entries are'} hidden because {incompatible.length === 1 ? 'it publishes' : 'they publish'} embedding dimensions above 1024 without truncation or provider resizing.</p>}
      <div className="button-row">
        <button type="button" disabled={busy} onClick={() => void fetchModels()}>Fetch again</button>
        <button type="button" onClick={() => setManualOpen(!manualOpen)}>Enter model ID manually</button>
        <button type="button" onClick={onClose}>Close</button>
      </div>
      <p className="hint">Configured and manual models stay available even when the catalogue fails or omits them.</p>
    </>}
    {draft && <MetadataForm
      key={draft.name}
      draft={draft}
      capability={capability}
      taskLabel={taskLabel}
      revision={revision}
      provider={providerInput(provider)}
      taken={new Set(models.filter(item => !sameModel(item, draft)).map(item => modelKey(item.provider, item.name)))}
      onGeometry={onGeometry}
      onAdd={values => {
        onPick(values)
        setDraft(null)
        setNotice(`${values.name} added to the draft. Save to persist it.`)
      }}
      onCancel={() => setDraft(null)} />}
    {!results && !busy && <div className="button-row"><button type="button" onClick={() => void fetchModels()}>Fetch again</button><button type="button" onClick={() => setManualOpen(true)}>Enter model ID manually</button><button type="button" onClick={onClose}>Close</button></div>}
    {manualOpen && <div className="llm-manual">

      <label>Model ID<input aria-label="Manual model ID" value={manualID} onChange={e => setManualID(e.target.value)} autoCapitalize="none" spellCheck={false} placeholder="Provider model ID" /></label>
      <div className="button-row"><button type="button" onClick={openManual}>Add model ID</button></div>
    </div>}
  </fieldset>
}

function CatalogueDetail({model}: {model: DiscoveredModel}) {
  const parts: string[] = []
  if (model.input_per_1m !== undefined) parts.push(`in $${model.input_per_1m}/1M`)
  if (model.output_per_1m !== undefined) parts.push(`out $${model.output_per_1m}/1M`)
  if (model.embedding_per_1m !== undefined) parts.push(`embed $${model.embedding_per_1m}/1M`)
  if (model.dimensions !== undefined) parts.push(`${model.dimensions} dimensions`)
  if (model.truncatable === true) parts.push('truncatable')
  if (model.supports_custom_dimensions === true) parts.push('provider resizing')
  if (!parts.length) return null
  return <span className="llm-picker-detail">{parts.join(' · ')}</span>
}

// MetadataForm completes a selection whose applicable metadata the catalogue
// did not publish. Rates must be entered or explicitly marked free; unknown
// is never treated as free. Check is explicit because a fixed sample can cost
// money, and its observed geometry is applied to the selection only.
function MetadataForm({draft, capability, taskLabel, revision, provider, taken, onAdd, onCancel, onGeometry}: {
  draft: ModelDraft
  capability: LLMCapability | null
  taskLabel: string | null
  revision: number
  provider: LLMProviderInput
  taken: Set<string>
  onAdd: (model: ModelDraft) => void
  onCancel: () => void
  onGeometry?: (patch: {provider: string; name: string; dimensions?: number; supports_custom_dimensions?: boolean}) => void
}) {
  const [values, setValues] = useState<ModelDraft>(draft)
  const [free, setFree] = useState(false)
  const [problems, setProblems] = useState<string[]>([])
  const [checking, setChecking] = useState(false)
  const [checkMessage, setCheckMessage] = useState('')
  const checkScope = JSON.stringify({provider, revision, values})
  const currentScope = useRef(checkScope)
  currentScope.current = checkScope
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const firstField = useRef<HTMLInputElement>(null)
  useEffect(() => { firstField.current?.focus() }, [])
  const chat = capability !== 'embedding'
  const embedding = capability !== 'chat'
  const applicableRates: Array<{label: string; field: 'input_per_1m' | 'output_per_1m' | 'embedding_per_1m'}> = [
    ...(chat ? [{label: 'Input price per 1M tokens', field: 'input_per_1m' as const}] : []),
    ...(chat ? [{label: 'Output price per 1M tokens', field: 'output_per_1m' as const}] : []),
    ...(embedding ? [{label: 'Embedding price per 1M tokens', field: 'embedding_per_1m' as const}] : []),
  ]
  const ratesReady = free || applicableRates.every(({field}) => validateMoney(values[field]) === '')

  function set(patch: Partial<ModelDraft>) {
    setValues(current => ({...current, ...patch}))
    setProblems([])
    setCheckMessage('')
  }

  function toggleFree(checked: boolean) {
    setFree(checked)
    if (!checked) return
    setValues(current => ({
      ...current,
      ...(chat ? {input_per_1m: '0', output_per_1m: '0'} : {}),
      ...(embedding ? {embedding_per_1m: '0'} : {}),
    }))
  }

  function check() {
    if (checking) return
    if (!ratesReady) {
      setProblems(['Provide every applicable rate or mark this model free before checking it.'])
      return
    }
    const launchScope = checkScope
    const current = () => mounted.current && currentScope.current === launchScope
    setChecking(true)
    setProblems([])
    setCheckMessage('')
    const body: LLMModelsCheckInput = {
      revision,
      provider,
      model: {
        name: values.name,
        input_per_1m: values.input_per_1m,
        output_per_1m: values.output_per_1m,
        embedding_per_1m: values.embedding_per_1m,
        dimensions: Number(values.dimensions),
        truncatable: values.truncatable,
        supports_custom_dimensions: values.supports_custom_dimensions,
        ...(values.provider ? {provider: values.provider} : {}),
      },
      capability: capability ?? 'chat',
    }
    void (async () => {
      try {
        const result = await request<LLMModelsCheck>('/v1/settings/llm/models/check', {method: 'POST', body: JSON.stringify(body)})
        if (!current()) return
        setCheckMessage(result.message || (result.compatible ? 'Model is compatible.' : 'Model is not compatible.'))
        if (!result.compatible) return
        // Observed geometry is applied to the selection only. Rates are
        // never changed by a check; they came from the user or free mark.
        if (result.dimensions !== undefined) setValues(current => ({...current, dimensions: String(result.dimensions)}))
        if (result.supports_custom_dimensions !== undefined) setValues(current => ({...current, supports_custom_dimensions: result.supports_custom_dimensions!}))
        onGeometry?.({provider: values.provider, name: values.name, dimensions: result.dimensions, supports_custom_dimensions: result.supports_custom_dimensions})
      } catch (problem) {
        if (!current()) return
        setCheckMessage(message(problem, 'The model check failed.'))
      } finally {
        if (mounted.current) setChecking(false)
      }
    })()
  }

  function add() {
    const found = validateModel({...values, name: values.name.trim()}, taken)
    setProblems(found)
    if (!found.length) onAdd({...values, name: values.name.trim()})
    else firstField.current?.focus()
  }

  return <div className="llm-model-form" role="group" aria-label={`Complete ${draft.name}`}>
    <div className="settings-row-head"><strong>{draft.name}</strong>
      {draft.provider && <span className="chip">{draft.provider}</span>}
      {free && <span className="chip" data-tone="green">free</span>}
    </div>
    {problems.length > 0 && <div role="alert" className="form-error"><ul>{problems.map((problem, i) => <li key={i}>{problem}</li>)}</ul></div>}
    {applicableRates.map(({label, field}, index) => <label key={field}>{label}
      <input
        ref={index === 0 ? firstField : undefined}
        aria-label={`${draft.name} ${label.toLowerCase()}`}
        value={values[field]}
        aria-invalid={problems.length > 0 && !!validateMoney(values[field])}
        inputMode="decimal"
        autoCapitalize="none"
        spellCheck={false}
        onChange={e => set({[field]: e.target.value} as Partial<ModelDraft>)} />
      {problems.length > 0 && validateMoney(values[field]) && <span className="field-error">Enter a price or select “This model is free”.</span>}
    </label>)}
    <label><input type="checkbox" checked={free} onChange={e => toggleFree(e.target.checked)} /> This model is free</label>
    {embedding && <>
      <label>Dimensions<input aria-label={`${draft.name} dimensions`} type="number" min="0" step="1" value={values.dimensions} aria-invalid={!!validateDimensions(values.dimensions, values.truncatable, values.supports_custom_dimensions)} onChange={e => set({dimensions: e.target.value})} />
        {validateDimensions(values.dimensions, values.truncatable, values.supports_custom_dimensions) && <span className="field-error">Enter the native embedding width, or use Check model to detect it.</span>}
      </label>
      <label><input aria-label={`${draft.name} truncatable`} type="checkbox" checked={values.truncatable} onChange={e => set({truncatable: e.target.checked})} /> Truncatable</label>
      <label><input aria-label={`${draft.name} supports custom dimensions`} type="checkbox" checked={values.supports_custom_dimensions} onChange={e => set({supports_custom_dimensions: e.target.checked})} /> Provider supports custom dimensions</label>
    </>}
    <div className="button-row">
      <button type="button" onClick={add}>Add to draft{taskLabel ? ` and use for ${taskLabel}` : ''}</button>
      <button type="button" disabled={checking} onClick={check}>{checking ? 'Checking…' : 'Check model'}</button>
      <button type="button" onClick={onCancel}>Cancel</button>
    </div>
    <p className="hint">Check sends a fixed sample to the provider and may cost money. It never changes the rates above.</p>
    {checkMessage && <p role="status" className="muted">{checkMessage}</p>}
  </div>
}
