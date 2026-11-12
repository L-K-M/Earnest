import type {components} from '../api.generated'

type Schemas = components['schemas']
export type LLMProvider = Schemas['LLMProviderSettings']
export type LLMTask = Schemas['LLMTaskSettings']
export type LLMModel = Schemas['LLMModelSettings']
export type LLMSettings = Schemas['LLMSettings']
export type LLMProviderInput = Schemas['LLMProviderInput']
export type LLMSettingsInput = Schemas['LLMSettingsInput']
export type DiscoveredModel = Schemas['DiscoveredModel']
export type LLMModels = Schemas['LLMModels']

export const LLM_TASKS = ['embed', 'classify', 'blurb', 'summary', 'edition', 'compile_rule', 'ask'] as const
export type LLMTaskName = (typeof LLM_TASKS)[number]
export const LLM_PROVIDER_KINDS = ['openai', 'anthropic', 'gemini'] as const
export type LLMProviderKind = (typeof LLM_PROVIDER_KINDS)[number]
export const LLM_TOKEN_FIELDS = ['max_completion_tokens', 'max_tokens'] as const
export type LLMTokenField = (typeof LLM_TOKEN_FIELDS)[number]

export const LLM_DEFAULT_BASE_URLS: Record<LLMProviderKind, string> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com/v1beta',
}

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
}
export type ModelDraft = Omit<LLMModel, 'dimensions'> & {dimensions: string}

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
  tasks: LLMTask[]
  models: ModelDraft[]
}

// draftFromSettings starts every provider key blank: the server never returns
// a key, and a blank field means "keep the saved key". Tasks the server did
// not enumerate are added as disabled, and unknown task names are kept so a
// saved document is never narrowed by the form.
export function draftFromSettings(settings: LLMSettings): LLMSettingsDraft {
  const saved = settings.tasks ?? []
  const tasks = LLM_TASKS.map(name => {
    const found = saved.find(task => task.task === name)
    return found ? {...found} : {task: name, provider: '', model: ''}
  })
  for (const task of saved) {
    if (!(LLM_TASKS as readonly string[]).includes(task.task)) tasks.push({...task})
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
    tasks,
    models: (settings.models ?? []).map(model => ({...model, dimensions: String(model.dimensions)})),
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

// draftFromDiscovered seeds the price review. Catalogue fields prefill when the
// provider published them; only a model known to be chat-only may default
// embedding price and geometry to zero, and only a known embedding-only model
// may default chat prices to zero. Everything else stays blank so a missing
// applicable price or embedding geometry demands explicit input: unknown is
// never free.
export function draftFromDiscovered(model: DiscoveredModel): ModelDraft {
  const chatOnly = model.chat === true && model.embedding === false
  const embeddingOnly = model.embedding === true && model.chat === false
  return {
    name: model.id,
    input_per_1m: model.input_per_1m ?? (embeddingOnly ? '0' : ''),
    output_per_1m: model.output_per_1m ?? (embeddingOnly ? '0' : ''),
    embedding_per_1m: model.embedding_per_1m ?? (chatOnly ? '0' : ''),
    dimensions: model.dimensions === undefined ? (chatOnly ? '0' : '') : String(model.dimensions),
    truncatable: model.truncatable ?? false,
  }
}

// validateDiscoveredModel reuses the configured-model rules and adds the
// geometry a known embedding model needs: the provider saying a model can
// embed means its dimensions must be real, not a placeholder.
export function validateDiscoveredModel(draft: ModelDraft, model: DiscoveredModel, otherNames: Set<string>): string[] {
  const problems = validateModel(draft, otherNames)
  if (model.embedding === true && (!WHOLE.test(draft.dimensions) || Number(draft.dimensions) < 1)) {
    problems.push(`${draft.name ? `Model ${draft.name}` : 'A model'}: dimensions are required for a model the provider says can embed.`)
  }
  return problems
}

// inputFromDraft builds the PUT body; see providerInput for the key rules.
export function inputFromDraft(draft: LLMSettingsDraft): Omit<LLMSettingsInput, 'op_id'> {
  return {
    revision: draft.revision,
    providers: draft.providers.map(providerInput),
    tasks: draft.tasks.map(task => ({task: task.task, provider: task.provider, model: task.model})),
    models: draft.models.map(model => ({
      name: model.name,
      input_per_1m: model.input_per_1m,
      output_per_1m: model.output_per_1m,
      embedding_per_1m: model.embedding_per_1m,
      dimensions: Number(model.dimensions),
      truncatable: model.truncatable,
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

export function validateDimensions(value: string, truncatable: boolean): string {
  if (!WHOLE.test(value)) return 'dimensions must be a whole number.'
  const dimensions = Number(value)
  if (dimensions > 65536) return 'dimensions are at most 65536.'
  if (dimensions > 1024 && !truncatable) return 'dimensions above 1024 need truncatable vectors.'
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

export function validateModel(model: ModelDraft, otherNames: Set<string>): string[] {
  const label = model.name ? `Model ${model.name}` : 'A model'
  const problems: string[] = []
  if (!model.name) problems.push('Every model needs a name.')
  else {
    if (model.name.length > 256) problems.push(`${label} name is at most 256 characters.`)
    if (/\s/.test(model.name) || hasControlCharacters(model.name)) problems.push(`${label} name must not contain whitespace or control characters.`)
    if (otherNames.has(model.name)) problems.push(`${label} name is already used; model names must be unique.`)
  }
  for (const [field, value] of [['input', model.input_per_1m], ['output', model.output_per_1m], ['embedding', model.embedding_per_1m]] as const) {
    const problem = validateMoney(value)
    if (problem) problems.push(`${label}: ${field} price ${problem}`)
  }
  const dimensions = validateDimensions(model.dimensions, model.truncatable)
  if (dimensions) problems.push(`${label}: ${dimensions}`)
  return problems
}

export function validateTask(task: LLMTask, draft: LLMSettingsDraft): string[] {
  if (!(LLM_TASKS as readonly string[]).includes(task.task)) return []
  const label = `Task ${task.task}`
  if (!!task.provider !== !!task.model) return [`${label} needs both a provider and a model, or neither to stay off.`]
  if (!task.provider) return []
  const provider = draft.providers.find(item => item.name === task.provider)
  if (!provider) return [`${label} names provider ${task.provider}, which is not configured.`]
  const model = draft.models.find(item => item.name === task.model)
  if (!model) return [`${label} names model ${task.model}, which is not in the model table.`]
  if (task.task === 'embed' && provider.kind === 'anthropic') return [`${label} cannot use Anthropic, which has no embeddings API.`]
  return []
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
  const modelNames = new Set<string>()
  for (const model of draft.models) {
    problems.push(...validateModel(model, modelNames))
    modelNames.add(model.name)
  }
  for (const task of draft.tasks) problems.push(...validateTask(task, draft))
  return problems
}
