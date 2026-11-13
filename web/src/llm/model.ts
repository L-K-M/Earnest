import type {components} from '../api.generated'

type Schemas = components['schemas']

export type LLMTaskMode = NonNullable<Schemas['LLMTaskSettings']['mode']>
export type LLMRoute = Schemas['TaskConfig']
export type LLMDefaults = Schemas['DefaultRoutes']

export type LLMTask = Schemas['LLMTaskSettings']
export type LLMModel = Schemas['LLMModelSettings']
export type LLMProvider = Schemas['LLMProviderSettings']
export type LLMSettings = Schemas['LLMSettings']
export type LLMProviderInput = Schemas['LLMProviderInput']
export type LLMSettingsInput = Schemas['LLMSettingsInput']
export type LLMSettingsPayload = Omit<LLMSettingsInput, 'op_id'>
export type DiscoveredModel = Schemas['DiscoveredModel']
export type LLMModels = Schemas['LLMModels']
export type LLMModelsCheckInput = Schemas['LLMModelCheckInput']
export type LLMModelsCheck = Schemas['ModelCheck']

export const LLM_TASKS = ['embed', 'classify', 'blurb', 'summary', 'edition', 'compile_rule', 'ask'] as const
export type LLMTaskName = (typeof LLM_TASKS)[number]

// User-readable names for the seven AI features. Machine names stay in the
// draft and the request body; these are for headings, labels and errors.
export const LLM_TASK_LABELS: Record<LLMTaskName, string> = {
  embed: 'Embeddings',
  classify: 'Classification',
  blurb: 'Blurbs',
  summary: 'Summaries',
  edition: 'Editions',
  compile_rule: 'Rule compilation',
  ask: 'Ask my feeds',
}

export function taskLabel(task: string): string {
  return (LLM_TASK_LABELS as Record<string, string>)[task] ?? task
}

export const LLM_PROVIDER_KINDS = ['openai', 'anthropic', 'gemini'] as const
export type LLMProviderKind = (typeof LLM_PROVIDER_KINDS)[number]
export const LLM_TOKEN_FIELDS = ['max_completion_tokens', 'max_tokens'] as const
export type LLMTokenField = (typeof LLM_TOKEN_FIELDS)[number]

export const LLM_DEFAULT_BASE_URLS: Record<LLMProviderKind, string> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com/v1beta',
}

export const LLM_PROVIDER_PRESETS = [
  {label: 'Venice', kind: 'openai', base_url: 'https://api.venice.ai/api/v1'},
  {label: 'OpenAI', kind: 'openai', base_url: 'https://api.openai.com/v1'},
  {label: 'Anthropic', kind: 'anthropic', base_url: 'https://api.anthropic.com'},
  {label: 'Gemini', kind: 'gemini', base_url: 'https://generativelanguage.googleapis.com/v1beta'},
  {label: 'Custom', kind: 'openai', base_url: ''},
] as const
export type LLMProviderPreset = (typeof LLM_PROVIDER_PRESETS)[number]

const MAX_PROVIDERS = 32
export const MAX_MODELS = 128
const MAX_MONEY = '99999999.999999'
const PROVIDER_NAME = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const DECIMAL = /^\d+(?:\.\d{1,6})?$/
const WHOLE = /^\d+$/

// The draft is the editable form state. Numeric fields stay strings so a
// half-typed value never becomes NaN, and each provider carries the typed key
// plus the clear flag. Nothing here is ever written to storage.
export type ProviderDraft = Omit<LLMProvider, 'kind' | 'token_field' | 'timeout_seconds'> & {
  kind: LLMProviderKind
  token_field: LLMTokenField
  timeout_seconds: string
  api_key: string
  clear_api_key: boolean
  // The name the provider had when it was loaded, so a rename can be told
  // apart from a new provider. Null for a provider that is not saved yet.
  original_name: string | null
  routed_name?: string
}
export type ModelDraft = Omit<LLMModel, 'dimensions' | 'provider' | 'supports_custom_dimensions'> & {
  dimensions: string
  provider: string
  supports_custom_dimensions: boolean
}
export type LLMTaskDraft = {task: string; mode: LLMTaskMode; provider: string; model: string}

// The badge summarizes the draft, not the server: a typed key is only
// "entered" until Save, and the clear flag schedules removal.
export type LLMKeyStatus = 'saved' | 'entered' | 'removal' | 'none'
export function apiKeyStatus(provider: Pick<ProviderDraft, 'has_api_key' | 'api_key' | 'clear_api_key'>): LLMKeyStatus {
  if (provider.api_key.trim()) return 'entered'
  if (provider.clear_api_key) return 'removal'
  return provider.has_api_key ? 'saved' : 'none'
}
export const LLM_KEY_STATUS_LABELS: Record<LLMKeyStatus, string> = {
  saved: 'API key saved',
  entered: 'API key entered · unsaved',
  removal: 'Key removal pending',
  none: 'No API key',
}

export type LLMSettingsDraft = {
  revision: number
  providers: ProviderDraft[]
  defaults: LLMDefaults
  tasks: LLMTaskDraft[]
  models: ModelDraft[]
}

export function emptyDefaults(): LLMDefaults {
  return {text: {provider: '', model: ''}, embedding: {provider: '', model: ''}}
}

// draftFromSettings starts every provider key blank: the server never returns
// a key, and a blank field means "keep the saved key". Tasks the server did
// not enumerate are added as disabled, and unknown task names are kept so a
// saved document is never narrowed by the form. A task without a mode is the
// legacy explicit route: custom when it names a provider and model, off
// otherwise. Configurations saved before defaults existed keep every route as
// custom or off, never default.
export function draftFromSettings(settings: LLMSettings): LLMSettingsDraft {
  const defaults = settings.defaults ? {
    text: {provider: settings.defaults.text?.provider ?? '', model: settings.defaults.text?.model ?? ''},
    embedding: {provider: settings.defaults.embedding?.provider ?? '', model: settings.defaults.embedding?.model ?? ''},
  } : emptyDefaults()
  const hasDefaults = settings.defaults !== undefined && settings.defaults !== null
  const saved = settings.tasks ?? []
  // A default task's provider/model are resolved from the defaults document,
  // so the draft keeps its own route empty. Only a custom task stores an
  // override here; the PUT body resolves the effective route for old readers.
  const taskDraft = (task: LLMTask): LLMTaskDraft => {
    const mode: LLMTaskMode = hasDefaults && task.mode ? task.mode : (task.provider || task.model ? 'custom' : 'off')
    const custom = mode === 'custom'
    return {task: task.task, mode, provider: custom ? (task.provider ?? '') : '', model: custom ? (task.model ?? '') : ''}
  }
  const tasks: LLMTaskDraft[] = LLM_TASKS.map(name => {
    const found = saved.find(task => task.task === name)
    return found ? taskDraft(found) : {task: name, mode: 'off', provider: '', model: ''}
  })
  for (const task of saved) {
    if ((LLM_TASKS as readonly string[]).includes(task.task)) continue
    tasks.push(taskDraft(task))
  }
  return {
    revision: settings.revision,
    providers: (settings.providers ?? []).map(provider => ({
      ...provider,
      kind: provider.kind as LLMProviderKind,
      token_field: provider.token_field as LLMTokenField,
      timeout_seconds: String(provider.timeout_seconds),
      api_key: '',
      clear_api_key: false,
      original_name: provider.name,
    })),
    defaults,
    tasks,
    models: (settings.models ?? []).map(model => ({
      ...model,
      dimensions: String(model.dimensions),
      provider: model.provider ?? '',
      supports_custom_dimensions: model.supports_custom_dimensions ?? false,
    })),
  }
}

// providerInput builds the per-provider request object shared by the settings
// PUT and model discovery. A typed key is sent as a replacement, a checked
// clear flag removes the saved key, and neither sends the read-only
// has_api_key field. A blank field keeps whatever the server holds, so an
// unchanged saved provider lets the server inherit its stored key.
export function providerInput(provider: ProviderDraft): LLMProviderInput {
  const input: LLMProviderInput = {
    name: provider.name,
    kind: provider.kind,
    base_url: provider.base_url,
    monthly_limit_usd: provider.monthly_limit_usd,
    timeout_seconds: Number(provider.timeout_seconds),
    token_field: provider.token_field,
    clear_api_key: provider.clear_api_key,
  }
  const key = provider.api_key.trim()
  if (key) return {...input, api_key: key, clear_api_key: false}
  return input
}

// Compare exact in-memory draft values. A hash can collide after a key edit
// and wrongly accept a catalogue fetched with different credentials.
export function sameDiscoveryProvider(a: ProviderDraft, b: ProviderDraft): boolean {
  return a.name === b.name && a.kind === b.kind && a.base_url === b.base_url
    && a.api_key === b.api_key && a.clear_api_key === b.clear_api_key
    && a.has_api_key === b.has_api_key && a.original_name === b.original_name
    && a.monthly_limit_usd === b.monthly_limit_usd
    && a.timeout_seconds === b.timeout_seconds && a.token_field === b.token_field
}

// The only task with an embedding contract; everything else is chat work.
export type LLMCapability = 'chat' | 'embedding'
export function taskCapability(task: string): LLMCapability {
  return task === 'embed' ? 'embedding' : 'chat'
}

// capabilityFor reads the catalogue flag for one capability. Undefined means
// the provider did not say, never that the model cannot do it.
export function capabilityFor(model: DiscoveredModel, capability: LLMCapability): boolean | undefined {
  return capability === 'chat' ? model.chat : model.embedding
}

// Model identity is provider+name. An empty provider is the legacy global
// record; a provider-scoped record wins over the global one for that provider.
export function modelKey(provider: string, name: string): string {
  return provider ? `${provider}\u0000${name}` : name
}

export function findModel(models: ModelDraft[], provider: string, name: string): ModelDraft | undefined {
  if (!name) return undefined
  return models.find(model => model.provider === provider && model.name === name)
    ?? models.find(model => model.provider === '' && model.name === name)
}

export function sameModel(a: Pick<ModelDraft, 'provider' | 'name'>, b: Pick<ModelDraft, 'provider' | 'name'>): boolean {
  return a.provider === b.provider && a.name === b.name
}

// defaultRouteFor resolves the default route a feature inherits. Every task
// uses the text default except embeddings.
export function defaultRouteFor(task: string, defaults: LLMDefaults): LLMRoute {
  return task === 'embed' ? defaults.embedding : defaults.text
}

// effectiveTaskRoute is the route old readers see. A default task resolves
// through the defaults document, a custom task through its own route, and an
// off task is empty. Saving must send this resolved route alongside mode.
export function effectiveTaskRoute(task: LLMTaskDraft, defaults: LLMDefaults): LLMRoute {
  if (task.mode === 'default') return defaultRouteFor(task.task, defaults)
  if (task.mode === 'custom') return {provider: task.provider, model: task.model}
  return {provider: '', model: ''}
}

// draftFromDiscovered seeds a selection. The capability decides which side is
// applicable: a chat selection ignores embedding geometry and rate (both are
// zeroed), an embedding selection ignores chat rates. Catalogue fields
// prefill when published; an unknown applicable field stays blank so the
// selection pane can demand explicit input. Unknown is never free.
export function draftFromDiscovered(model: DiscoveredModel, provider: string, capability: LLMCapability | null): ModelDraft {
  // A catalogue that already knows a model is chat-only or embedding-only
  // decides the nonapplicable side even when the picker has no task context.
  const chatOnly = model.chat === true && model.embedding === false
  const embeddingOnly = model.embedding === true && model.chat === false
  const chat = capability === null ? !embeddingOnly : capability === 'chat'
  const embedding = capability === null ? !chatOnly : capability === 'embedding'
  return {
    name: model.id,
    provider,
    input_per_1m: chat ? (model.input_per_1m ?? '') : '0',
    output_per_1m: chat ? (model.output_per_1m ?? '') : '0',
    embedding_per_1m: embedding ? (model.embedding_per_1m ?? '') : '0',
    dimensions: embedding ? (model.dimensions === undefined ? '' : String(model.dimensions)) : '0',
    truncatable: embedding ? (model.truncatable ?? false) : false,
    supports_custom_dimensions: embedding ? (model.supports_custom_dimensions ?? false) : false,
  }
}

// missingApplicableFields names the metadata a selection still needs before it
// can enter the draft. The selection pane shows these inline instead of
// sending the user to a separate review step.
export function missingApplicableFields(model: DiscoveredModel, capability: LLMCapability | null): string[] {
  const missing: string[] = []
  const chatOnly = model.chat === true && model.embedding === false
  const embeddingOnly = model.embedding === true && model.chat === false
  const chat = capability === null ? !embeddingOnly : capability === 'chat'
  const embedding = capability === null ? !chatOnly : capability === 'embedding'
  if (chat) {
    if (model.input_per_1m === undefined) missing.push('input price')
    if (model.output_per_1m === undefined) missing.push('output price')
  }
  if (embedding) {
    if (model.embedding_per_1m === undefined) missing.push('embedding price')
    if (model.dimensions === undefined) missing.push('dimensions')
  }
  return missing
}

// geometryIncompatible reports an embedding model whose published width cannot
// be used: above the storage width with resizing explicitly unsupported and no
// local truncation. Unknown resizing remains available for an explicit check.
// Chat selections ignore embedding geometry entirely.
export function geometryIncompatible(model: DiscoveredModel, capability: LLMCapability | null): boolean {
  if (model.dimensions === undefined || model.dimensions <= 1024) return false
  if (model.truncatable || model.supports_custom_dimensions !== false) return false
  if (capability === 'chat') return false
  if (capability === 'embedding') return model.embedding !== false
  // Without a task context, only a model the catalogue says can embed is
  // judged on embedding geometry; an unknown model may be chat-only.
  return model.embedding === true
}

// inputFromDraft builds the PUT body; see providerInput for the key rules.
// Default tasks carry their resolved route so readers that predate modes
// still route them correctly. Scoped model records keep provider+name
// identity and the two distinct geometry flags.
export function inputFromDraft(draft: LLMSettingsDraft): LLMSettingsPayload {
  return {
    revision: draft.revision,
    // Defaults are always sent, including empty routes, so removing the last
    // default clears it for old readers too.
    defaults: draft.defaults,
    providers: draft.providers.map(providerInput),
    tasks: draft.tasks.map(task => {
      const route = effectiveTaskRoute(task, draft.defaults)
      return {task: task.task, mode: task.mode, provider: route.provider, model: route.model}
    }),
    models: draft.models.map(model => ({
      name: model.name,
      input_per_1m: model.input_per_1m,
      output_per_1m: model.output_per_1m,
      embedding_per_1m: model.embedding_per_1m,
      dimensions: Number(model.dimensions),
      truncatable: model.truncatable,
      supports_custom_dimensions: model.supports_custom_dimensions,
      ...(model.provider ? {provider: model.provider} : {}),
    })),
  }
}

export function validateBaseURL(value: string): string {
  if (!value) return 'the base URL is required.'
  // The raw check catches a trailing '?' or '#' that the URL parser drops.
  if (value.includes('?') || value.includes('#')) return 'the base URL must not contain a query or fragment.'
  let parsed: URL
  try { parsed = new URL(value) } catch { return 'the base URL must be a complete http(s) URL.' }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return 'the base URL must use http or https.'
  if (!parsed.hostname) return 'the base URL needs a host.'
  if (parsed.username || parsed.password) return 'the base URL must not contain credentials.'
  return ''
}

// Money stays a string end to end. The maximum is compared digit by digit so
// no exact amount ever passes through a double.
function aboveMaxMoney(value: string): boolean {
  const [whole, fraction = ''] = value.split('.')
  const [maxWhole, maxFraction] = MAX_MONEY.split('.')
  if (whole.length !== maxWhole.length) return whole.length > maxWhole.length
  if (whole !== maxWhole) return whole > maxWhole
  return (fraction + '000000').slice(0, maxFraction.length) > maxFraction
}

export function validateMoney(value: string): string {
  if (!DECIMAL.test(value)) return 'must be a non-negative amount with at most six decimals.'
  if (aboveMaxMoney(value)) return `is above the server maximum of ${MAX_MONEY}.`
  return ''
}

export function validateTimeout(value: string): string {
  if (!WHOLE.test(value)) return 'must be a whole number of seconds.'
  const seconds = Number(value)
  if (seconds < 1 || seconds > 600) return 'must be between 1 and 600 seconds.'
  return ''
}

// validateDimensions separates three cases: a blank value is missing, not a
// malformed integer; a whole number above 1024 is valid when the vector can
// be cut locally (truncatable) or resized by the provider
// (supports_custom_dimensions). The two flags are distinct and either one is
// enough.
export function validateDimensions(value: string, truncatable: boolean, supportsCustomDimensions = false): string {
  if (value.trim() === '') return 'dimensions are required; enter 0 when the model does not embed.'
  if (!WHOLE.test(value)) return 'dimensions must be a whole number.'
  const dimensions = Number(value)
  if (dimensions > 65536) return 'dimensions are at most 65536.'
  if (dimensions > 1024 && !truncatable && !supportsCustomDimensions) return 'dimensions above 1024 need truncatable vectors or provider-side resizing.'
  return ''
}

function normalizeBaseURL(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

// Control characters are invalid in keys and model names; a regex would trip
// the linter, so walk the code points instead.
function hasControlCharacters(value: string): boolean {
  return Array.from(value).some(char => {
    const code = char.charCodeAt(0)
    return code < 0x20 || code === 0x7f
  })
}

// validateProvider mirrors the server rules. `original` is the saved provider
// the draft entry came from, matched by original_name, and `otherNames` holds
// the names of every other provider in the draft.
export function validateProvider(provider: ProviderDraft, original: LLMProvider | undefined, otherNames: Set<string>): string[] {
  const label = provider.name ? `Provider ${provider.name}` : 'A provider'
  const problems: string[] = []
  if (!provider.name) problems.push('Every provider needs a name.')
  else {
    if (provider.name.length > 64) problems.push(`${label} name is at most 64 characters.`)
    if (!PROVIDER_NAME.test(provider.name)) problems.push(`${label} name uses lowercase letters, digits and single dashes.`)
    if (otherNames.has(provider.name)) problems.push(`${label} name is already used; provider names must be unique.`)
  }
  const baseURL = validateBaseURL(provider.base_url)
  if (baseURL) problems.push(`${label}: ${baseURL}`)
  const limit = validateMoney(provider.monthly_limit_usd)
  if (limit) problems.push(`${label}: monthly limit ${limit}`)
  const timeout = validateTimeout(provider.timeout_seconds)
  if (timeout) problems.push(`${label}: timeout ${timeout}`)
  if (hasControlCharacters(provider.api_key)) problems.push(`${label}: the API key must not contain control characters.`)
  if (provider.api_key && provider.clear_api_key) problems.push(`${label}: choose either a replacement API key or clearing the saved key, not both.`)
  if (original?.has_api_key) {
    const renamed = provider.original_name !== null && provider.name !== provider.original_name
    // The server trims spaces and trailing slashes before comparing, so the
    // client does too; otherwise a harmless slash would demand a new key.
    const moved = normalizeBaseURL(provider.base_url) !== normalizeBaseURL(original.base_url) || provider.kind !== original.kind
    if ((renamed || moved) && !provider.api_key && !provider.clear_api_key) {
      problems.push(`Provider ${original.name} keeps its saved API key only when the ${renamed ? 'name, base URL and kind' : 'base URL and kind'} stay unchanged. Re-enter the key or clear it.`)
    }
  }
  return problems
}

export function validateModel(model: ModelDraft, otherKeys: Set<string>): string[] {
  const label = model.name ? `Model ${model.name}` : 'A model'
  const problems: string[] = []
  if (!model.name) problems.push('Every model needs a name.')
  else {
    if (model.name.length > 256) problems.push(`${label} name is at most 256 characters.`)
    if (/\s/.test(model.name) || hasControlCharacters(model.name)) problems.push(`${label} name must not contain whitespace or control characters.`)
    if (otherKeys.has(modelKey(model.provider, model.name))) problems.push(`${label} is already configured${model.provider ? ` for provider ${model.provider}` : ''}; model names must be unique within their scope.`)
  }
  for (const [field, value] of [['input', model.input_per_1m], ['output', model.output_per_1m], ['embedding', model.embedding_per_1m]] as const) {
    const problem = validateMoney(value)
    if (problem) problems.push(`${label}: ${field} price ${problem}`)
  }
  const dimensions = validateDimensions(model.dimensions, model.truncatable, model.supports_custom_dimensions)
  if (dimensions) problems.push(`${label}: ${dimensions}`)
  return problems
}

export function validateTask(task: LLMTaskDraft, draft: LLMSettingsDraft): string[] {
  if (!(LLM_TASKS as readonly string[]).includes(task.task)) return []
  const label = `Task ${taskLabel(task.task)}`
  if (task.mode === 'off') return []
  const route = effectiveTaskRoute(task, draft.defaults)
  if (task.mode === 'default' && (!route.provider || !route.model)) {
    // A feature set to Default must have a default to inherit. It must not
    // silently stay off, and a default alone never enables an Off feature.
    return [`Task ${label} uses Default, but no default ${task.task === 'embed' ? 'embedding' : 'text'} model is configured.`]
  }
  if (task.mode === 'custom' && !!task.provider !== !!task.model) {
    return [`${label} needs both a provider and a model, or neither to stay off.`]
  }
  if (!route.provider || !route.model) return []
  const provider = draft.providers.find(item => item.name === route.provider)
  if (!provider) return [`${label} names provider ${route.provider}, which is not configured.`]
  const model = findModel(draft.models, route.provider, route.model)
  if (!model) return [`${label} names model ${route.model}, which is not in the model table.`]
  if (task.task === 'embed' && provider.kind === 'anthropic') return [`${label} cannot use Anthropic, which has no embeddings API.`]
  return []
}

// validateDefaults checks that each configured default route resolves to a
// provider and a model in the draft. Empty routes are allowed: a feature set
// to Default then stays off rather than turning on by accident.
export function validateDefaults(draft: LLMSettingsDraft): string[] {
  const problems: string[] = []
  const routes: Array<[string, LLMRoute]> = [
    ['Default text model', draft.defaults.text],
    ['Default embedding model', draft.defaults.embedding],
  ]
  for (const [label, route] of routes) {
    if (!route.provider && !route.model) continue
    if (!route.provider || !route.model) {
      problems.push(`${label} needs both a provider and a model, or neither.`)
      continue
    }
    const provider = draft.providers.find(item => item.name === route.provider)
    if (!provider) {
      problems.push(`${label} names provider ${route.provider}, which is not configured.`)
      continue
    }
    if (!findModel(draft.models, route.provider, route.model)) {
      problems.push(`${label} names model ${route.model}, which is not in the model table.`)
    }
    if (label === 'Default embedding model' && provider.kind === 'anthropic') {
      problems.push(`${label} cannot use Anthropic, which has no embeddings API.`)
    }
  }
  return problems
}

export function validateDraft(draft: LLMSettingsDraft, loaded: LLMSettings | null): string[] {
  const problems: string[] = []
  if (draft.providers.length > MAX_PROVIDERS) problems.push(`At most ${MAX_PROVIDERS} providers are allowed.`)
  const names = new Set<string>()
  for (const provider of draft.providers) {
    problems.push(...validateProvider(provider, loaded?.providers?.find(item => item.name === provider.original_name), names))
    names.add(provider.name)
  }
  if (draft.models.length > MAX_MODELS) problems.push(`At most ${MAX_MODELS} models are allowed.`)
  const modelKeys = new Set<string>()
  for (const model of draft.models) {
    problems.push(...validateModel(model, modelKeys))
    modelKeys.add(modelKey(model.provider, model.name))
  }
  problems.push(...validateDefaults(draft))
  for (const task of draft.tasks) problems.push(...validateTask(task, draft))
  return problems
}

// uniqueProviderName generates an editable provider name from a preset that
// does not collide with the names already in the draft.
export function uniqueProviderName(base: string, taken: Set<string>): string {
  const root = base.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '') || 'provider'
  if (!taken.has(root)) return root
  for (let suffix = 2; ; suffix++) {
    const candidate = `${root}-${suffix}`
    if (!taken.has(candidate)) return candidate
  }
}
