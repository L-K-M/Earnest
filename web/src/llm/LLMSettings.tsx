import {useCallback, useEffect, useRef, useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {request, RequestError} from '../request'
import {useBlocker} from '../router'
import {
  LLM_DEFAULT_BASE_URLS,
  LLM_KEY_STATUS_LABELS,
  LLM_PROVIDER_KINDS,
  LLM_PROVIDER_PRESETS,
  LLM_TOKEN_FIELDS,
  MAX_MODELS,
  apiKeyStatus,
  defaultRouteFor,
  draftFromSettings,
  effectiveTaskRoute,
  inputFromDraft,
  modelKey,
  sameModel,
  taskCapability,
  taskLabel,
  uniqueProviderName,
  validateDraft,
  validateMoney,
  validateTimeout,
  type DiscoveredModel,
  type LLMCapability,
  type LLMDefaults,
  type LLMProviderKind,
  type LLMProviderPreset,
  type LLMSettings as LLMSettingsData,
  type LLMSettingsDraft,
  type LLMTokenField,
  type LLMTaskDraft,
  type ModelDraft,
  type ProviderDraft,
} from './model'
import {ModelPicker} from './Discovery'
import {withoutModel} from './mutations'

const newOp = () => crypto.randomUUID()
const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback

// A live name-collision error is kept in the shared problem list so Save
// already reports it, but it carries this prefix so a later valid rename can
// clear exactly this error without discarding unrelated problems.
const NAME_TAKEN_PREFIX = 'Provider name is already used by another provider'
const nameTakenMessage = (name: string) => `${NAME_TAKEN_PREFIX}: ${name}. Choose a different name.`

type PickerTarget = {kind: 'task'; index: number} | {kind: 'default'; which: keyof LLMDefaults} | {kind: 'provider'; index: number}

// toggleTask switches a feature on or off. Turning it on restores a saved
// custom override when there is one, otherwise it inherits the default.
function toggleTask(task: LLMTaskDraft, checked: boolean): Partial<LLMTaskDraft> {
  if (!checked) return {mode: 'off'}
  return {mode: task.provider || task.model ? 'custom' : 'default'}
}

// selectableModels lists the models a route may use. A provider-scoped model
// only applies to its own provider; the legacy global record applies to every
// provider. Embedding routes additionally need real geometry.
function selectableModels(models: ModelDraft[], provider: string, capability: LLMCapability): ModelDraft[] {
  const scoped = models.filter(model => model.provider === provider)
  // A scoped record wins over the global record with the same name, so the
  // global one is not offered a second time for this provider.
  const scopedNames = new Set(scoped.map(model => model.name))
  const candidates = [...scoped, ...models.filter(model => model.provider === '' && !scopedNames.has(model.name))]
  if (capability === 'chat') return candidates
  return candidates.filter(model => Number(model.dimensions) > 0 || model.supports_custom_dimensions)
}

// LLMSettings is the server-wide AI service configuration section. The server
// owns the only copy; this form edits a draft and Save replaces the whole
// provider, default, task and model configuration in one request. Provider
// fields are edited directly in the draft; there is no separate apply step.
export function LLMSettings() {
  const queryClient = useQueryClient()
  const [loaded, setLoaded] = useState<LLMSettingsData | null>(null)
  const [draft, setDraft] = useState<LLMSettingsDraft | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [providerForm, setProviderForm] = useState<{mode: 'edit'; index: number} | null>(null)
  const [conflict, setConflict] = useState(false)
  const [picker, setPicker] = useState<PickerTarget | null>(null)
  // The op_id of the request whose outcome is uncertain, so a retry of the
  // identical body reuses it instead of risking a second apply.
  const attempt = useRef<{body: string; opID: string} | null>(null)
  // Catalogue results are kept for this settings session only. Closing and
  // reopening the picker reuses them; remounting the section starts fresh.
  const catalogueCache = useRef(new Map<string, DiscoveredModel[]>())
  // The provider name that references currently point at. A half-typed empty
  // name must not capture unrelated routes, so the last valid name is kept
  // until a non-empty replacement arrives.
  const providerEditName = useRef<string | null>(null)

  const load = useCallback(async (notice: string) => {
    setBusy(true)
    setProblems([])
    setNotice('')
    try {
      const next = await request<LLMSettingsData>('/v1/settings/llm')
      attempt.current = null
      setLoaded(next)
      setDraft(draftFromSettings(next))
      providerEditName.current = null
      setProviderForm(null)
      setConflict(false)
      setPicker(null)
      setNotice(notice)
    } catch (error) {
      setProblems([message(error, 'Could not load the AI services.')])
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => { void load('') }, [load])

  const loadedDraft = loaded ? draftFromSettings(loaded) : null
  const dirty = draft !== null && loadedDraft !== null
    && JSON.stringify(inputFromDraft(draft)) !== JSON.stringify(inputFromDraft(loadedDraft))
  useBlocker(dirty, 'Leave without saving your AI settings?')

  function setTask(index: number, patch: Partial<LLMTaskDraft>) {
    setDraft(current => current ? {...current, tasks: current.tasks.map((task, i) => i === index ? {...task, ...patch} : task)} : current)
    if (patch.mode === 'off' || patch.mode === 'default' || patch.provider === '') {
      setPicker(current => current?.kind === 'task' && current.index === index ? null : current)
    }
    setNotice('')
  }

  function setDefault(which: keyof LLMDefaults, route: {provider: string; model: string}) {
    if (draft?.defaults[which].provider !== route.provider) {
      setPicker(current => current?.kind === 'default' && current.which === which ? null : current)
    }
    setDraft(current => current ? {...current, defaults: {...current.defaults, [which]: route}} : current)
    setNotice('')
  }

  function setModel(index: number, patch: Partial<ModelDraft>) {
    setDraft(current => current ? {...current, models: current.models.map((model, i) => i === index ? {...model, ...patch} : model)} : current)
    setNotice('')
  }

  // Renaming a provider moves its task routes, defaults and scoped models with
  // it, so nothing is left pointing at the old name. A name that collides
  // with a sibling's current or retained name is rejected before the draft
  // changes, because moving onto it would hand the sibling's rows to this
  // provider. The move is deferred while the name field is empty: an empty
  // name is the global model scope, not this provider, and matching it would
  // capture unrelated records.
  function updateProvider(index: number, patch: Partial<ProviderDraft>) {
    if (!draft) return
    const name = patch.name
    if (name !== undefined) {
      const collision = name !== '' && draft.providers.some((provider, i) => i !== index
        && (provider.name === name || provider.routed_name === name))
      if (collision) {
        setProblems(current => [...current.filter(problem => !problem.startsWith(NAME_TAKEN_PREFIX)), nameTakenMessage(name)])
        return
      }
      setProblems(current => current.filter(problem => !problem.startsWith(NAME_TAKEN_PREFIX)))
    }
    setDraft(current => {
      if (!current) return current
      const previous = current.providers[index]
      const to = patch.name !== undefined ? patch.name : previous.name
      const from = previous.routed_name ?? previous.name
      const move = patch.name !== undefined && to !== '' && from !== '' && to !== from ? {from, to} : null
      const routedName = patch.name === undefined ? previous.routed_name : to || previous.routed_name || previous.name
      if (move) providerEditName.current = to
      return {
        ...current,
        providers: current.providers.map((provider, i) => i === index ? {...provider, ...patch, name: to, routed_name: routedName} : provider),
        tasks: move ? current.tasks.map(task => task.provider === move.from ? {...task, provider: move.to} : task) : current.tasks,
        defaults: move ? {
          text: current.defaults.text.provider === move.from ? {...current.defaults.text, provider: move.to} : current.defaults.text,
          embedding: current.defaults.embedding.provider === move.from ? {...current.defaults.embedding, provider: move.to} : current.defaults.embedding,
        } : current.defaults,
        models: move ? current.models.map(model => model.provider === move.from ? {...model, provider: move.to} : model) : current.models,
      }
    })
    setNotice('')
  }

  function addProvider(preset: LLMProviderPreset) {
    if (!draft) return
    // Reserve both the current names and the names a half-typed empty field
    // still routes through, so a preset cannot capture a retained identity.
    const taken = new Set<string>()
    for (const provider of draft.providers) {
      if (provider.name) taken.add(provider.name)
      if (provider.routed_name) taken.add(provider.routed_name)
    }
    const name = uniqueProviderName(preset.label, taken)
    const provider: ProviderDraft = {
      name,
      kind: preset.kind,
      base_url: preset.base_url,
      has_api_key: false,
      monthly_limit_usd: '0',
      timeout_seconds: '60',
      token_field: 'max_completion_tokens',
      api_key: '',
      clear_api_key: false,
      original_name: null,
    }
    setDraft({...draft, providers: [...draft.providers, provider]})
    providerEditName.current = name
    setProviderForm({mode: 'edit', index: draft.providers.length})
    setPicker(null)
    setProblems([])
    setNotice(`${preset.label} provider added. Edit the details, then Save.`)
  }

  function removeProvider(index: number) {
    if (!draft) return
    const provider = draft.providers[index]
    // While a rename is half-typed, references still point at the last valid
    // name; removal must follow those references, not the empty field.
    const name = provider.routed_name || provider.name
    const usesProvider = (candidate: string) => candidate === name
    const affected = draft.tasks.filter(task => usesProvider(task.provider)
      || (task.mode === 'default' && usesProvider(defaultRouteFor(task.task, draft.defaults).provider)))
    const off = affected.map(task => taskLabel(task.task))
    const note = off.length ? ` Removing it turns off ${off.join(', ')}.` : ''
    if (!window.confirm(`Remove provider ${name}?${note}`)) return
    setDraft({
      ...draft,
      providers: draft.providers.filter((_, i) => i !== index),
      // Scoped prices belong to the provider and cannot outlive it; global
      // records stay available to every other provider.
      models: draft.models.filter(model => model.provider !== name),
      tasks: draft.tasks.map(task => usesProvider(task.provider)
        || (task.mode === 'default' && usesProvider(defaultRouteFor(task.task, draft.defaults).provider))
        ? {task: task.task, mode: 'off', provider: '', model: ''}
        : task),
      defaults: {
        text: usesProvider(draft.defaults.text.provider) ? {provider: '', model: ''} : draft.defaults.text,
        embedding: usesProvider(draft.defaults.embedding.provider) ? {provider: '', model: ''} : draft.defaults.embedding,
      },
    })
    if (providerForm?.mode === 'edit') {
      if (providerForm.index === index) {
        providerEditName.current = null
        setProviderForm(null)
      } else if (providerForm.index > index) {
        setProviderForm({mode: 'edit', index: providerForm.index - 1})
      }
    }
    setPicker(null)
    setProblems([])
    setNotice('')
  }

  // pickModel adds the selected model to the draft unless its provider+name
  // identity is already configured; an existing record keeps its saved prices.
  // The picker target decides which route the model becomes.
  function pickModel(model: ModelDraft) {
    if (!draft) return
    const target = picker
    const existing = draft.models.find(item => sameModel(item, model))
    if (!existing && draft.models.length >= MAX_MODELS) {
      setProblems([`The model table is full at ${MAX_MODELS} entries; remove one before adding ${model.name}.`])
      return
    }
    setDraft(current => {
      if (!current) return current
      const found = current.models.find(item => sameModel(item, model))
      const models = found ? current.models : [...current.models, model]
      if (target?.kind === 'task') {
        return {...current, models, tasks: current.tasks.map((task, i) => i === target.index ? {...task, mode: 'custom', provider: model.provider, model: model.name} : task)}
      }
      if (target?.kind === 'default') {
        return {...current, models, defaults: {...current.defaults, [target.which]: {provider: model.provider, model: model.name}}}
      }
      return {...current, models}
    })
    setPicker(null)
    setProblems([])
    setNotice(`${model.name} selected. Save to persist it.`)
  }

  // Observed geometry from a model check updates only geometry in the draft;
  // rates stay exactly as the user entered them.
  function applyGeometry(patch: {provider: string; name: string; dimensions?: number; supports_custom_dimensions?: boolean}) {
    setDraft(current => current ? {
      ...current,
      models: current.models.map(model => model.provider === patch.provider && model.name === patch.name ? {
        ...model,
        ...(patch.dimensions !== undefined ? {dimensions: String(patch.dimensions)} : {}),
        ...(patch.supports_custom_dimensions !== undefined ? {supports_custom_dimensions: patch.supports_custom_dimensions} : {}),
      } : model),
    } : current)
  }

  function removeModel(index: number) {
    if (!draft) return
    const model = draft.models[index]
    const next = withoutModel(draft, model)
    const affected = draft.tasks.filter((task, i) => task.mode !== 'off' && next.tasks[i].mode === 'off')
    const note = affected.length ? ` Removing it turns off ${affected.map(task => taskLabel(task.task)).join(', ')}.` : ''
    if (!window.confirm(`Remove model ${model.name}?${note}`)) return
    setDraft(next)
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
        providerEditName.current = null
        setProviderForm(null)
        setConflict(false)
        setPicker(null)
        setNotice('AI settings saved.')
        // The shell gates Ask on this cache, not on the settings response.
        void queryClient.invalidateQueries({queryKey: ['pipeline']})
      } catch (error) {
        if (error instanceof RequestError && error.status === 409) {
          // A conflict means the saved configuration moved on. Keep the draft
          // and make the reload explicit; never overwrite what changed.
          attempt.current = null
          setConflict(true)
          setProblems(['These settings changed after you loaded them. Reload to get the current version; your edits are kept and nothing was overwritten.'])
        } else {
          setProblems([message(error, 'The settings did not save. Your edits are kept.')])
        }
      } finally {
        setBusy(false)
      }
    })()
  }

  function discard() {
    if (!dirty || window.confirm('Discard your unsaved AI changes and reload?')) void load('AI settings reloaded.')
  }

  function pickerProvider(target: PickerTarget): {provider: ProviderDraft | null; capability: LLMCapability | null; taskLabel: string | null} {
    if (target.kind === 'provider') {
      const provider = draft?.providers[target.index] ?? null
      return {provider, capability: null, taskLabel: null}
    }
    if (target.kind === 'default') {
      const which = target.which
      const route = draft?.defaults[which]
      const provider = draft?.providers.find(item => item.name === route?.provider) ?? draft?.providers[0] ?? null
      return {provider, capability: which === 'embedding' ? 'embedding' : 'chat', taskLabel: which === 'embedding' ? 'the default embedding model' : 'the default text model'}
    }
    const task = draft?.tasks[target.index]
    const provider = task?.mode === 'custom' ? draft?.providers.find(item => item.name === task.provider) ?? null : null
    return {provider, capability: task ? taskCapability(task.task) : null, taskLabel: task ? taskLabel(task.task) : null}
  }

  const activePicker = picker ? pickerProvider(picker) : null
  const providerNameError = problems.find(problem => problem.startsWith(NAME_TAKEN_PREFIX))
  const saveProblems = problems.filter(problem => !problem.startsWith(NAME_TAKEN_PREFIX))

  return <section aria-labelledby="llm-services-heading">
    <h2 id="llm-services-heading">AI</h2>
    <p>Server-wide settings. Add a provider, choose models, then enable the features you want.</p>
    {loaded && loaded.revision === 0 && <p className="hint">The server is still using its environment defaults. Saving stores this configuration in the database; from then on the saved configuration takes precedence and survives restarts.</p>}
    {saveProblems.length > 0 && <div role="alert" className="form-error">{loaded ? <p>Save blocked. Fix the following:</p> : null}<ul>{saveProblems.map((problem, i) => <li key={i}>{problem}</li>)}</ul></div>}
    {notice && <p role="status">{notice}</p>}
    {busy && <p role="status" className="muted">Working…</p>}
    {!loaded && <div className="button-row"><button className="primary" disabled={busy} onClick={() => void load('AI settings loaded.')}>{busy ? 'Loading…' : 'Retry loading'}</button></div>}
    {loaded && draft && <form onSubmit={save} onChange={() => setNotice('')}>
      <div className="button-row">
        <button className="primary" disabled={busy || !dirty || conflict}>Save</button>
        <button type="button" disabled={busy || !dirty} onClick={discard}>Discard</button>
        {conflict && <button type="button" disabled={busy} onClick={discard}>Reload</button>}
        {dirty ? <span className="muted">Unsaved changes</span> : <span className="muted">No unsaved changes</span>}
      </div>

      <h3>Providers</h3>
      <p className="hint">Choose a service or connect your own compatible server. API keys are stored encrypted.</p>
      <div className="button-row">
        {LLM_PROVIDER_PRESETS.map(preset => <button key={preset.label} type="button" disabled={busy} onClick={() => addProvider(preset)}>Add {preset.label}</button>)}
      </div>
      <ul className="settings-list">
        {draft.providers.map((provider, index) => <li key={index}>
          {providerForm?.mode === 'edit' && providerForm.index === index
            ? <ProviderForm provider={provider} nameError={providerNameError} busy={busy} onChange={patch => updateProvider(index, patch)} onRemove={() => removeProvider(index)} />
            : <>
              <div className="settings-row-head">
                <strong>{provider.name}</strong>
                <span className="chip">{provider.kind}</span>
                <span className="chip" data-tone={apiKeyStatus(provider) === 'saved' ? 'green' : apiKeyStatus(provider) === 'entered' ? 'blue' : 'amber'}>{LLM_KEY_STATUS_LABELS[apiKeyStatus(provider)]}</span>
              </div>
              <p className="settings-row-detail muted">{provider.base_url} · {/^0+(?:\.0+)?$/.test(provider.monthly_limit_usd) ? 'no monthly limit' : `$${provider.monthly_limit_usd} per month`}</p>
              <div className="button-row">
                <button type="button" disabled={busy} onClick={() => { providerEditName.current = provider.name; setProblems(current => current.filter(problem => !problem.startsWith(NAME_TAKEN_PREFIX))); setProviderForm({mode: 'edit', index}) }}>Edit</button>
                <button type="button" disabled={busy} onClick={() => removeProvider(index)}>Remove</button>
                <button type="button" disabled={busy} onClick={() => setPicker({kind: 'provider', index})}>Browse models</button>
              </div>
            </>}
        </li>)}
      </ul>
      {picker?.kind === 'provider' && activePicker?.provider && <ModelPicker
        disabled={busy}
        provider={activePicker.provider}
        revision={draft.revision}
        capability={null}
        taskLabel={null}
        models={draft.models}
        room={MAX_MODELS - draft.models.length}
        cache={catalogueCache.current}
        onConflict={() => setConflict(true)}
        onPick={pickModel}
        onGeometry={applyGeometry}
        onClose={() => setPicker(null)} />}

      <h3>Defaults</h3>
      <p className="hint">Choose the models your enabled features will use. Existing custom routes stay unchanged.</p>
      <RouteSelect label="Default text model" route={draft.defaults.text} providers={draft.providers} models={draft.models} capability="chat" busy={busy} onChange={route => setDefault('text', route)} onBrowse={() => setPicker({kind: 'default', which: 'text'})} />
      <RouteSelect label="Default embedding model" route={draft.defaults.embedding} providers={draft.providers} models={draft.models} capability="embedding" busy={busy} onChange={route => setDefault('embedding', route)} onBrowse={() => setPicker({kind: 'default', which: 'embedding'})} />
      {picker?.kind === 'default' && activePicker?.provider && <ModelPicker
        disabled={busy}
        provider={activePicker.provider}
        revision={draft.revision}
        capability={activePicker.capability}
        taskLabel={activePicker.taskLabel}
        models={draft.models}
        room={MAX_MODELS - draft.models.length}
        cache={catalogueCache.current}
        onConflict={() => setConflict(true)}
        onPick={pickModel}
        onGeometry={applyGeometry}
        onClose={() => setPicker(null)} />}

      <h3>Features</h3>
      <p className="hint">Enable each feature separately. Open Advanced to use a different model for a feature.</p>
      <ul className="settings-list">
        {draft.tasks.map((task, index) => {
          const label = taskLabel(task.task)
          const capability = taskCapability(task.task)
          const resolved = effectiveTaskRoute(task, draft.defaults)
          const enabled = task.mode !== 'off'
          const on = enabled && !!resolved.provider && !!resolved.model
          return <li key={index}>
            <div className="settings-row-head">
              <label><input type="checkbox" checked={enabled} disabled={busy} onChange={e => setTask(index, toggleTask(task, e.target.checked))} /> <strong>{label}</strong></label>
              <span className="chip" data-tone={on ? (task.mode === 'default' ? 'blue' : 'green') : undefined}>{on ? task.mode : enabled ? 'Default' : 'Off'}</span>
            </div>
            {enabled && <>
              <p className="settings-row-detail muted">{resolved.provider && resolved.model ? `Uses ${resolved.provider} · ${resolved.model}.` : 'Choose a default model before saving this feature.'}</p>
              <details className="llm-advanced">
                <summary>Advanced</summary>
                <label>Provider
                  <select aria-label={`${label} provider`} value={task.provider} disabled={busy} onChange={e => setTask(index, {provider: e.target.value, model: '', mode: e.target.value ? 'custom' : 'default'})}>
                    <option value="">Default</option>
                    {draft.providers.map(item => <option key={item.name} value={item.name}>{item.name} ({item.kind})</option>)}
                  </select>
                </label>
                <label>Model
                  <select aria-label={`${label} model`} value={task.model} disabled={busy || !task.provider} onChange={e => setTask(index, {model: e.target.value, mode: e.target.value ? 'custom' : 'default'})}>
                    <option value="">Default</option>
                    {selectableModels(draft.models, task.provider, capability).map(item => <option key={modelKey(item.provider, item.name)} value={item.name}>{item.name}{item.provider ? '' : ' (any provider)'}</option>)}
                  </select>
                </label>
                <div className="button-row"><button type="button" disabled={busy || !task.provider} onClick={() => setPicker({kind: 'task', index})}>Browse models</button></div>
              </details>
            </>}
            {picker?.kind === 'task' && picker.index === index && activePicker?.provider && <ModelPicker
              disabled={busy}
              provider={activePicker.provider}
              revision={draft.revision}
              capability={activePicker.capability}
              taskLabel={activePicker.taskLabel}
              models={draft.models}
              room={MAX_MODELS - draft.models.length}
              cache={catalogueCache.current}
        onConflict={() => setConflict(true)}
              onPick={pickModel}
              onGeometry={applyGeometry}
              onClose={() => setPicker(null)} />}
          </li>
        })}
      </ul>

      <details>
        <summary>Model pricing</summary>
        <p className="hint">Prices are USD per million tokens and must be explicit; use 0 for a local model. Editing one model keeps every other entry and its prices. A model scoped to a provider wins over the global entry with the same name.</p>
        <ul className="settings-list">
          {draft.models.map((model, index) => <li key={index}>
            <div className="settings-row-head">
              <strong>{model.name || 'Unnamed model'}</strong>
              {model.provider && <span className="chip">{model.provider}</span>}
              {model.truncatable && <span className="chip">truncatable</span>}
              {model.supports_custom_dimensions && <span className="chip">provider resizing</span>}
            </div>
            {!model.provider && <p className="hint">Legacy shared pricing</p>}
            <label>Input price per 1M tokens<input aria-label={`${model.name || 'new model'} input price`} value={model.input_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {input_per_1m: e.target.value})} /></label>
            <label>Output price per 1M tokens<input aria-label={`${model.name || 'new model'} output price`} value={model.output_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {output_per_1m: e.target.value})} /></label>
            <label>Embedding price per 1M tokens<input aria-label={`${model.name || 'new model'} embedding price`} value={model.embedding_per_1m} inputMode="decimal" autoCapitalize="none" spellCheck={false} disabled={busy} onChange={e => setModel(index, {embedding_per_1m: e.target.value})} /></label>
            <label>Dimensions<input aria-label={`${model.name || 'new model'} dimensions`} type="number" min="0" step="1" value={model.dimensions} disabled={busy} onChange={e => setModel(index, {dimensions: e.target.value})} /></label>
            <label><input aria-label={`${model.name || 'new model'} truncatable`} type="checkbox" checked={model.truncatable} disabled={busy} onChange={e => setModel(index, {truncatable: e.target.checked})} /> Truncatable</label>
            <label><input aria-label={`${model.name || 'new model'} supports custom dimensions`} type="checkbox" checked={model.supports_custom_dimensions} disabled={busy} onChange={e => setModel(index, {supports_custom_dimensions: e.target.checked})} /> Provider supports custom dimensions</label>
            <div className="button-row"><button type="button" disabled={busy} onClick={() => removeModel(index)}>Remove model</button></div>
          </li>)}
        </ul>

      </details>
    </form>}
  </section>
}

// RouteSelect edits one default route. The provider choice scopes the model
// list; the model choice includes global records, which apply to any provider.
function RouteSelect({label, route, providers, models, capability, busy, onChange, onBrowse}: {
  label: string
  route: {provider: string; model: string}
  providers: ProviderDraft[]
  models: ModelDraft[]
  capability: LLMCapability
  busy: boolean
  onChange: (route: {provider: string; model: string}) => void
  onBrowse: () => void
}) {
  return <div className="llm-route-field">
    <span>{label}</span>
    <span className="llm-route">
      <select aria-label={`${label} provider`} value={route.provider} disabled={busy} onChange={e => onChange({provider: e.target.value, model: ''})}>
        <option value="">No provider</option>
        {providers.map(provider => <option key={provider.name} value={provider.name}>{provider.name} ({provider.kind})</option>)}
      </select>
      <select aria-label={`${label} model`} value={route.model} disabled={busy || !route.provider} onChange={e => onChange({provider: route.provider, model: e.target.value})}>
        <option value="">No model</option>
        {selectableModels(models, route.provider, capability).map(model => <option key={modelKey(model.provider, model.name)} value={model.name}>{model.name}{model.provider ? '' : ' (any provider)'}</option>)}
      </select>
      <button type="button" disabled={busy || !route.provider} onClick={onBrowse}>Browse models</button>
    </span>
  </div>
}

// ProviderForm edits one provider in place. Every change writes to the draft;
// there is no apply step. Monthly budget and key stay visible, while timeout
// and token field live under Advanced.
function ProviderForm({provider, nameError, busy, onChange, onRemove}: {provider: ProviderDraft; nameError?: string; busy: boolean; onChange: (patch: Partial<ProviderDraft>) => void; onRemove: () => void}) {
  return <div className="llm-provider-form">
    <label>Name<input value={provider.name} disabled={busy} aria-invalid={!!nameError} aria-describedby={nameError ? 'provider-name-error' : undefined} onChange={e => onChange({name: e.target.value})} autoCapitalize="none" spellCheck={false} /></label>
    {nameError && <span id="provider-name-error" className="field-error" role="alert">{nameError}</span>}
    <label>Base URL<input value={provider.base_url} disabled={busy} onChange={e => onChange({base_url: e.target.value})} inputMode="url" autoCapitalize="none" spellCheck={false} /></label>

    <label>API key<input type="password" value={provider.api_key} disabled={busy} onChange={e => onChange({api_key: e.target.value})} autoComplete="new-password" placeholder={provider.has_api_key ? 'Leave blank to keep the saved key' : 'Enter your provider API key'} /></label>
    <p className="hint">Key status: <span data-tone={apiKeyStatus(provider) === 'saved' ? 'green' : apiKeyStatus(provider) === 'entered' ? 'blue' : apiKeyStatus(provider) === 'removal' ? 'amber' : undefined}>{LLM_KEY_STATUS_LABELS[apiKeyStatus(provider)]}</span></p>
    {provider.has_api_key
      ? <><p className="hint">An API key is saved. Leave the field blank to keep it, or type a replacement.</p><label><input type="checkbox" checked={provider.clear_api_key} disabled={busy} onChange={e => onChange({clear_api_key: e.target.checked})} /> Clear the saved API key</label></>
      : <p className="hint">No key is saved. Leave the field blank if the server needs none.</p>}
    <label>Monthly limit (USD)<input value={provider.monthly_limit_usd} aria-invalid={!!validateMoney(provider.monthly_limit_usd)} disabled={busy} onChange={e => onChange({monthly_limit_usd: e.target.value})} inputMode="decimal" autoCapitalize="none" spellCheck={false} /></label>
    {validateMoney(provider.monthly_limit_usd) ? <p className="field-error">Monthly limit {validateMoney(provider.monthly_limit_usd)}</p> : <p className="hint">Set 0 for no monthly limit.</p>}
    <details className="llm-advanced">
      <summary>Advanced</summary>
    <label>Kind
      <select value={provider.kind} disabled={busy} onChange={e => {
        const kind = e.target.value as LLMProviderKind
        const patch: Partial<ProviderDraft> = {kind}
        if (provider.base_url === '' || (Object.values(LLM_DEFAULT_BASE_URLS) as string[]).includes(provider.base_url)) patch.base_url = LLM_DEFAULT_BASE_URLS[kind]
        onChange(patch)
      }}>{LLM_PROVIDER_KINDS.map(option => <option key={option} value={option}>{option}</option>)}</select>
    </label>
      <label>Timeout (seconds)<input type="number" min="1" max="600" step="1" value={provider.timeout_seconds} aria-invalid={!!validateTimeout(provider.timeout_seconds)} disabled={busy} onChange={e => onChange({timeout_seconds: e.target.value})} /></label>
      {validateTimeout(provider.timeout_seconds) && <p className="field-error">Timeout {validateTimeout(provider.timeout_seconds)}</p>}
      <label>Token field<select value={provider.token_field} disabled={busy} onChange={e => onChange({token_field: e.target.value as LLMTokenField})}>{LLM_TOKEN_FIELDS.map(option => <option key={option} value={option}>{option}</option>)}</select></label>
    </details>
    <div className="button-row"><button type="button" disabled={busy} onClick={onRemove}>Remove provider</button></div>

  </div>
}
