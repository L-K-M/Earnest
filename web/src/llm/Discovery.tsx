import {useEffect, useRef, useState} from 'react'
import {request, RequestError} from '../request'
import {
  MAX_MODELS,
  capabilityFor,
  draftFromDiscovered,
  sameDiscoveryProvider,
  providerInput,
  validateDiscoveredModel,
  type DiscoveredModel,
  type LLMCapability,
  type LLMModels,
  type ModelDraft,
  type ProviderDraft,
} from './model'

const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback

// DiscoveryPanel browses one provider's model catalogue over the admin
// no-store preview endpoint. Results live only in this component's memory and
// are bound to the provider draft they were fetched with: any edit, reload or
// session change drops them. Nothing here persists settings; imports only
// enter the draft and Save persists.
export function DiscoveryPanel({provider, revision, capability, taskLabel, configured, room, onPick, onImport, onClose, disabled = false}: {
  disabled?: boolean
  provider: ProviderDraft
  revision: number
  capability: LLMCapability | null
  taskLabel: string | null
  configured: Set<string>
  room: number
  onPick?: (model: string) => void
  onImport: (models: ModelDraft[]) => void
  onClose: () => void
}) {
  // Scope contains credentials only in component memory, never in a cache
  // key or rendered attribute. Each edit gets a distinct generation.
  const scope = useRef({provider: {...provider}, revision, generation: 0})
  if (scope.current.revision !== revision || !sameDiscoveryProvider(scope.current.provider, provider)) {
    scope.current = {provider: {...provider}, revision, generation: scope.current.generation + 1}
  }
  const generation = scope.current.generation
  const requestID = useRef(Symbol())
  const [results, setResults] = useState<{generation: number; models: DiscoveredModel[]} | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [reviewIntent, setReviewIntent] = useState<'import' | 'route'>('import')
  const [review, setReview] = useState<DiscoveredModel[] | null>(null)

  useEffect(() => {
    requestID.current = Symbol()
    setResults(null)
    setSelected(new Set())
    setReview(null)
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
      setResults({generation: launch.generation, models: found.models ?? []})
      setSelected(new Set())
      setReview(null)
    } catch (problem) {
      if (!current()) return
      setError(problem instanceof RequestError && problem.status === 404
        ? 'This server does not support model discovery; add models manually.'
        : problem instanceof RequestError && problem.status === 409
          ? 'The saved settings changed since this page loaded. Reload, then fetch again.'
          : message(problem, 'Could not fetch the model catalogue.'))
    } finally {
      if (current()) setBusy(false)
    }
  }

  useEffect(() => {
    void fetchModels()
    return () => { requestID.current = Symbol() }
  }, [])

  function importSelected() {
    if (disabled || !results || results.generation !== generation) return
    const picked = results.models.filter(model => selected.has(model.id))
    const fresh = picked.filter(model => !configured.has(model.id))
    if (!fresh.length) {
      setNotice(picked.length
        ? 'Every selected model is already configured; its existing prices are kept because prices are shared by model ID.'
        : 'Select catalogue entries to import.')
      return
    }
    if (fresh.length > room) {
      setError(`The configured-model table holds ${MAX_MODELS} entries and ${room} slot${room === 1 ? ' is' : 's are'} free. Deselect ${fresh.length - room} or remove configured models first.`)
      return
    }
    setReviewIntent('import')
    setReview(fresh)
    setNotice('')
  }

  function pick(model: DiscoveredModel) {
    if (disabled || !onPick || results?.generation !== generation) return
    if (configured.has(model.id)) {
      onPick(model.id)
      setNotice(`${model.id} is already configured; its existing prices are kept because prices are shared by model ID.`)
      return
    }
    if (room < 1) {
      setError(`The configured-model table is full at ${MAX_MODELS} entries; remove one before importing.`)
      return
    }
    setReviewIntent('route')
    setReview([model])
  }

  function applyReview(drafts: ModelDraft[]) {
    if (disabled || results?.generation !== generation) return
    if (drafts.length > room) { setError(`At most ${MAX_MODELS} configured models are allowed.`); return }
    const pickName = reviewIntent === 'route' && review?.length === 1 && onPick ? drafts[0].name : ''
    onImport(drafts)
    setReview(null)
    setSelected(new Set())
    if (pickName) onPick?.(pickName)
    setNotice(`${drafts.length === 1 ? '1 model' : `${drafts.length} models`} added to the draft. Review the prices below, then Save to persist.`)
  }

  const needle = query.trim().toLowerCase()
  const catalogue = results?.generation === generation ? results.models : []
  const searched = needle ? catalogue.filter(model => model.id.toLowerCase().includes(needle) || model.name.toLowerCase().includes(needle)) : catalogue
  // A task context hides only what the provider says cannot do the work;
  // unknown capability stays visible and labelled.
  const hidden = capability ? searched.filter(model => capabilityFor(model, capability) === false) : []
  const shown = capability ? searched.filter(model => capabilityFor(model, capability) !== false) : searched

  return <fieldset disabled={disabled} style={{border: 0, padding: 0, margin: 0}} className="llm-provider-form" aria-label={`Model catalogue for ${provider.name || 'the new provider'}`}>
    <p className="hint">Fetching previews the provider details currently in this draft, including an unsaved replacement key. It saves nothing and sends no inference request; results stay in this page and are discarded when the provider changes. Apply provider writes edits to the draft; Save persists the whole configuration.</p>
    {busy && <p role="status" className="muted">Fetching models…</p>}
    {error && <div role="alert" className="form-error"><ul><li>{error}</li></ul></div>}
    {notice && <p role="status">{notice}</p>}
    {results && <>
      <label>Search catalogue<input aria-label="Search catalogue" value={query} onChange={e => setQuery(e.target.value)} autoCapitalize="none" spellCheck={false} placeholder="Filter by ID or name" /></label>
      {!shown.length && <p className="muted">{searched.length ? `Every matching entry is known to lack ${capability} capability.` : 'No catalogue entries match the search.'}</p>}
      <ul className="settings-list">
        {shown.map(model => {
          const isConfigured = configured.has(model.id)
          const capabilityUnknown = capability === null ? model.chat === undefined && model.embedding === undefined : capabilityFor(model, capability) === undefined
          return <li key={model.id}>
            <div className="settings-row-head">
              <strong>{model.name === model.id ? model.id : `${model.name} (${model.id})`}</strong>
              {isConfigured && <span className="chip" data-tone="green">configured</span>}
              {capabilityUnknown && <span className="chip" data-tone="amber">capabilities unknown</span>}
              {model.chat === true && <span className="chip">chat</span>}
              {model.embedding === true && <span className="chip">embedding</span>}
            </div>
            <CatalogueDetail model={model} />
            {isConfigured && <p className="hint">Already in the model table; its existing prices are kept because prices are shared by model ID.</p>}
            <div className="button-row">
              <label><input type="checkbox" aria-label={`Select ${model.id}`} checked={selected.has(model.id)} onChange={e => {
                const next = new Set(selected)
                if (e.target.checked) next.add(model.id); else next.delete(model.id)
                setSelected(next)
              }} /> Select</label>
              {onPick && <button type="button" onClick={() => pick(model)}>Use for {taskLabel}</button>}
            </div>
          </li>
        })}
      </ul>
      {hidden.length > 0 && <p className="hint">{hidden.length} catalogue {hidden.length === 1 ? 'entry is' : 'entries are'} hidden because the provider says {hidden.length === 1 ? 'it' : 'they'} cannot do {capability} work.</p>}
      <div className="button-row">
        <button type="button" disabled={!selected.size} onClick={importSelected}>Import selected{selected.size ? ` (${selected.size})` : ''}</button>
        <button type="button" disabled={busy} onClick={() => void fetchModels()}>Fetch again</button>
        <button type="button" onClick={onClose}>Close</button>
      </div>
      <p className="hint">Only selected entries enter the draft; configured and manual models stay available even when the catalogue fails or omits them.</p>
    </>}
    {review && <PriceReview key={JSON.stringify(review.map(model => model.id))} models={review} taken={configured} onApply={applyReview} onCancel={() => setReview(null)} />}
    {!results && !busy && <div className="button-row"><button type="button" onClick={() => void fetchModels()}>Fetch again</button><button type="button" onClick={onClose}>Close</button></div>}
  </fieldset>
}

function CatalogueDetail({model}: {model: DiscoveredModel}) {
  const parts: string[] = []
  if (model.input_per_1m !== undefined) parts.push(`in $${model.input_per_1m}/1M`)
  if (model.output_per_1m !== undefined) parts.push(`out $${model.output_per_1m}/1M`)
  if (model.embedding_per_1m !== undefined) parts.push(`embed $${model.embedding_per_1m}/1M`)
  if (model.dimensions !== undefined) parts.push(`${model.dimensions} dimensions`)
  if (model.truncatable === true) parts.push('truncatable')
  if (!parts.length) return null
  return <p className="settings-row-detail muted">{parts.join(' · ')}</p>
}

// PriceReview is the gate between the catalogue and the draft. Published
// fields arrive prefilled; missing applicable prices and embedding geometry
// must be entered explicitly before the model may be imported.
function PriceReview({models, taken, onApply, onCancel}: {
  models: DiscoveredModel[]
  taken: Set<string>
  onApply: (drafts: ModelDraft[]) => void
  onCancel: () => void
}) {
  const [drafts, setDrafts] = useState<ModelDraft[]>(() => models.map(draftFromDiscovered))
  const [problems, setProblems] = useState<string[]>([])
  function setModel(index: number, patch: Partial<ModelDraft>) {
    setDrafts(drafts.map((draft, i) => i === index ? {...draft, ...patch} : draft))
  }
  function apply() {
    const found: string[] = []
    drafts.forEach((draft, index) => {
      const others = new Set(taken)
      for (const [i, sibling] of drafts.entries()) if (i !== index) others.add(sibling.name)
      found.push(...validateDiscoveredModel({...draft, name: draft.name.trim()}, models[index], others))
    })
    setProblems(found)
    if (!found.length) onApply(drafts.map(draft => ({...draft, name: draft.name.trim()})))
  }
  return <div className="llm-model-form">
    <p className="hint">Published rates are suggestions in USD per million tokens. Missing applicable prices and embedding geometry need explicit input; an unknown price is never treated as free. Prices are shared by model ID, so an ID that is already configured keeps its existing prices instead of these.</p>
    {problems.length > 0 && <div role="alert" className="form-error"><ul>{problems.map((problem, i) => <li key={i}>{problem}</li>)}</ul></div>}
    {models.map((model, index) => {
      const draft = drafts[index]
      const label = model.id
      return <div key={model.id}>
        <div className="settings-row-head"><strong>{model.name === model.id ? model.id : `${model.name} (${model.id})`}</strong>
          {model.chat === true && <span className="chip">chat</span>}
          {model.embedding === true && <span className="chip">embedding</span>}
          {model.chat === undefined && model.embedding === undefined && <span className="chip" data-tone="amber">capabilities unknown</span>}
        </div>
        <label>Name<input aria-label={`${label} configured name`} value={draft.name} onChange={e => setModel(index, {name: e.target.value.trim()})} autoCapitalize="none" spellCheck={false} /></label>
        <label>Input price per 1M tokens<input aria-label={`${label} input price`} value={draft.input_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} onChange={e => setModel(index, {input_per_1m: e.target.value})} /></label>
        <label>Output price per 1M tokens<input aria-label={`${label} output price`} value={draft.output_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} onChange={e => setModel(index, {output_per_1m: e.target.value})} /></label>
        <label>Embedding price per 1M tokens<input aria-label={`${label} embedding price`} value={draft.embedding_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} onChange={e => setModel(index, {embedding_per_1m: e.target.value})} /></label>
        <label>Dimensions<input aria-label={`${label} dimensions`} type="number" min="0" step="1" value={draft.dimensions} onChange={e => setModel(index, {dimensions: e.target.value})} /></label>
        <label><input type="checkbox" aria-label={`${label} truncatable`} checked={draft.truncatable} onChange={e => setModel(index, {truncatable: e.target.checked})} /> Truncatable</label>
      </div>
    })}
    <div className="button-row">
      <button type="button" onClick={apply}>Import {models.length === 1 ? 'model' : `${models.length} models`} to draft</button>
      <button type="button" onClick={onCancel}>Back to catalogue</button>
    </div>
  </div>
}
