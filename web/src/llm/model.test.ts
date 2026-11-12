import {expect, it} from 'vitest'
import {
  LLM_TASKS,
  apiKeyStatus,
  capabilityFor,
  draftFromDiscovered,
  draftFromSettings,
  effectiveTaskRoute,
  findModel,
  inputFromDraft,
  missingApplicableFields,
  sameDiscoveryProvider,
  taskCapability,
  uniqueProviderName,
  validateBaseURL,
  validateDefaults,
  validateDimensions,
  validateDraft,
  validateMoney,
  validateModel,
  validateTask,
  validateTimeout,
  type LLMSettings,
  type ProviderDraft,
} from './model'

it('accepts money up to the server maximum and rejects anything above it', () => {
  expect(validateMoney('0')).toBe('')
  expect(validateMoney('20')).toBe('')
  expect(validateMoney('20.000000')).toBe('')
  expect(validateMoney('99999999.999999')).toBe('')
  expect(validateMoney('100000000')).toMatch(/above the server maximum/)
  expect(validateMoney('99999999.9999999')).toMatch(/at most six decimals/)
  expect(validateMoney('-1')).toMatch(/non-negative/)
  expect(validateMoney('1.5.5')).toMatch(/at most six decimals/)
})

it('validates base URLs without credentials, query or fragment', () => {
  expect(validateBaseURL('https://api.openai.com/v1')).toBe('')
  expect(validateBaseURL('http://ollama:11434/v1')).toBe('')
  expect(validateBaseURL('https://user:pass@example.com/v1')).toMatch(/credentials/)
  expect(validateBaseURL('https://example.com/v1?x=1')).toMatch(/query or fragment/)
  expect(validateBaseURL('https://example.com/v1?')).toMatch(/query or fragment/)
  expect(validateBaseURL('https://example.com/v1#')).toMatch(/query or fragment/)
  expect(validateBaseURL('ftp://example.com')).toMatch(/http or https/)
  expect(validateBaseURL('not a url')).toMatch(/complete http/)
})

it('validates timeouts and embedding dimensions', () => {
  expect(validateTimeout('60')).toBe('')
  expect(validateTimeout('0')).toMatch(/between 1 and 600/)
  expect(validateTimeout('601')).toMatch(/between 1 and 600/)
  expect(validateTimeout('1.5')).toMatch(/whole number/)
  expect(validateDimensions('1024', false)).toBe('')
  expect(validateDimensions('1025', false)).toMatch(/need truncatable/)
  expect(validateDimensions('1025', false, true)).toBe('')
  expect(validateDimensions('1025', true, false)).toBe('')
  expect(validateDimensions('65536', true)).toBe('')
  expect(validateDimensions('65537', true)).toMatch(/at most 65536/)
})

it('reports blank dimensions as missing rather than an invalid integer', () => {
  expect(validateDimensions('', false)).toMatch(/required/i)
  expect(validateDimensions('', false)).not.toMatch(/whole number/)
  expect(validateDimensions('   ', false)).toMatch(/required/i)
})

const draftProvider = (patch: Partial<ProviderDraft>): ProviderDraft => ({
  name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true,
  monthly_limit_usd: '0', timeout_seconds: '60', token_field: 'max_tokens',
  api_key: '', clear_api_key: false, original_name: 'openai', ...patch,
})

it('summarizes the draft key status over saved, entered, removal and none', () => {
  expect(apiKeyStatus(draftProvider({}))).toBe('saved')
  expect(apiKeyStatus(draftProvider({api_key: '  sk-typed  '}))).toBe('entered')
  expect(apiKeyStatus(draftProvider({clear_api_key: true}))).toBe('removal')
  expect(apiKeyStatus(draftProvider({has_api_key: false}))).toBe('none')
  expect(apiKeyStatus(draftProvider({api_key: 'sk-x', clear_api_key: true}))).toBe('entered')
})

it('compares exact provider drafts including colliding key hashes', () => {
  const base = draftProvider({})
  expect(sameDiscoveryProvider(base, draftProvider({}))).toBe(true)
  expect(sameDiscoveryProvider(base, draftProvider({base_url: 'https://other.example/v1'}))).toBe(false)
  expect(sameDiscoveryProvider(draftProvider({api_key: 'Aa'}), draftProvider({api_key: 'BB'}))).toBe(false)
})

it('maps task routes to capabilities and reads catalogue flags honestly', () => {
  expect(taskCapability('embed')).toBe('embedding')
  expect(taskCapability('ask')).toBe('chat')
  expect(capabilityFor({id: 'a', name: 'a'}, 'chat')).toBeUndefined()
  expect(capabilityFor({id: 'a', name: 'a', chat: false}, 'chat')).toBe(false)
  expect(capabilityFor({id: 'a', name: 'a', embedding: true}, 'embedding')).toBe(true)
})

it('zeroes only the nonapplicable side of a catalogue selection', () => {
  const chatter = draftFromDiscovered({id: 'chat', name: 'Chat', chat: true, embedding: false, input_per_1m: '2', output_per_1m: '6'}, 'openai', 'chat')
  expect(chatter).toMatchObject({name: 'chat', provider: 'openai', input_per_1m: '2', output_per_1m: '6', embedding_per_1m: '0', dimensions: '0'})
  const embedder = draftFromDiscovered({id: 'emb', name: 'Emb', chat: false, embedding: true, embedding_per_1m: '0.1', dimensions: 512}, 'venice', 'embedding')
  expect(embedder).toMatchObject({provider: 'venice', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.1', dimensions: '512'})
  const both = draftFromDiscovered({id: 'both', name: 'Both', chat: true, embedding: true}, 'openai', null)
  expect(both).toMatchObject({input_per_1m: '', output_per_1m: '', embedding_per_1m: '', dimensions: ''})
})

it('names only the metadata a selection still needs', () => {
  expect(missingApplicableFields({id: 'a', name: 'A', input_per_1m: '1', output_per_1m: '2'}, 'chat')).toEqual([])
  expect(missingApplicableFields({id: 'a', name: 'A', input_per_1m: '1', output_per_1m: '2'}, 'embedding')).toEqual(['embedding price', 'dimensions'])
  expect(missingApplicableFields({id: 'a', name: 'A', embedding_per_1m: '0.1', dimensions: 512}, 'embedding')).toEqual([])
  expect(missingApplicableFields({id: 'a', name: 'A'}, null)).toEqual(['input price', 'output price', 'embedding price', 'dimensions'])
})

it('keeps legacy routes as custom and reads modes only when defaults exist', () => {
  const legacy: LLMSettings = {
    revision: 2,
    providers: [{name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_tokens'}],
    tasks: [
      {task: 'embed', provider: 'openai', model: 'text-embedding-3-small'},
      {task: 'classify', provider: '', model: ''},
      ...LLM_TASKS.slice(2).map(task => ({task, provider: '', model: ''})),
    ],
    models: [{name: 'text-embedding-3-small', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.02', dimensions: 1536, truncatable: true}],
  }
  const draft = draftFromSettings(legacy)
  expect(draft.defaults).toEqual({text: {provider: '', model: ''}, embedding: {provider: '', model: ''}})
  expect(draft.tasks.find(task => task.task === 'embed')).toMatchObject({mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(draft.tasks).toHaveLength(LLM_TASKS.length)
  expect(draft.tasks.find(task => task.task === 'classify')).toMatchObject({mode: 'off', provider: '', model: ''})
})

it('resolves default tasks through text except embeddings, and sends the effective route', () => {
  const settings: LLMSettings = {
    revision: 4,
    providers: [
      {name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_tokens'},
      {name: 'venice', kind: 'openai', base_url: 'https://api.venice.ai/api/v1', has_api_key: false, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_tokens'},
    ],
    defaults: {text: {provider: 'openai', model: 'gpt-5'}, embedding: {provider: 'venice', model: 'text-embedding-qwen3-8b'}},
    tasks: [
      {task: 'embed', provider: '', model: '', mode: 'default'},
      {task: 'classify', provider: '', model: '', mode: 'default'},
      {task: 'blurb', provider: 'venice', model: 'llama', mode: 'custom'},
      ...LLM_TASKS.slice(3).map(task => ({task, provider: '', model: '', mode: 'off' as const})),
    ],
    models: [
      {name: 'gpt-5', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: 0, truncatable: false},
      {name: 'llama', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0', dimensions: 0, truncatable: false},
      {name: 'text-embedding-qwen3-8b', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.01', dimensions: 4096, truncatable: false, provider: 'venice', supports_custom_dimensions: true},
    ],
  }
  const draft = draftFromSettings(settings)
  expect(draft.tasks.find(task => task.task === 'embed')).toMatchObject({mode: 'default'})
  expect(effectiveTaskRoute(draft.tasks.find(task => task.task === 'embed')!, draft.defaults)).toEqual({provider: 'venice', model: 'text-embedding-qwen3-8b'})
  expect(effectiveTaskRoute(draft.tasks.find(task => task.task === 'classify')!, draft.defaults)).toEqual({provider: 'openai', model: 'gpt-5'})

  const body = inputFromDraft(draft)
  expect(body.defaults).toEqual(settings.defaults)
  expect(body.tasks?.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'default', provider: 'venice', model: 'text-embedding-qwen3-8b'})
  expect(body.tasks?.find(task => task.task === 'classify')).toEqual({task: 'classify', mode: 'default', provider: 'openai', model: 'gpt-5'})
  expect(body.tasks?.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'venice', model: 'llama'})
  const scoped = body.models?.find(model => model.name === 'text-embedding-qwen3-8b')
  expect(scoped).toMatchObject({provider: 'venice', supports_custom_dimensions: true, dimensions: 4096})
})

it('requires a default model before a feature can use Default', () => {
  const draft = draftFromSettings({
    revision: 0, providers: [], defaults: {text: {provider: '', model: ''}, embedding: {provider: '', model: ''}},
    tasks: LLM_TASKS.map(task => ({task, provider: '', model: '', mode: 'default' as const})), models: [],
  })
  for (const task of draft.tasks) expect(effectiveTaskRoute(task, draft.defaults)).toEqual({provider: '', model: ''})
  expect(validateTask(draft.tasks[0]!, draft)).toEqual([expect.stringMatching(/no default embedding model is configured/)])
  expect(validateTask(draft.tasks[1]!, draft)).toEqual([expect.stringMatching(/no default text model is configured/)])
})

it('validates default routes and custom overrides', () => {
  const draft = draftFromSettings({
    revision: 1,
    providers: [{name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_tokens'}],
    defaults: {text: {provider: 'openai', model: 'gpt-5'}, embedding: {provider: 'missing', model: 'gone'}},
    tasks: LLM_TASKS.map(task => ({task, provider: '', model: '', mode: 'off' as const})),
    models: [{name: 'gpt-5', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: 0, truncatable: false}],
  })
  expect(validateDefaults(draft)).toContainEqual(expect.stringMatching(/Default embedding model names provider missing/))
  expect(validateDraft(draft, null)).toEqual(expect.arrayContaining([expect.stringMatching(/Default embedding model/)]))
})

it('validates task modes and allows neither half of a custom route', () => {
  const draft = draftFromSettings({
    revision: 1,
    providers: [{name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_tokens'}],
    tasks: [{task: 'classify', provider: 'openai', model: '', mode: 'custom' as const}, ...LLM_TASKS.filter(task => task !== 'classify').map(task => ({task, provider: '', model: '', mode: 'off' as const}))],
    models: [{name: 'gpt-5', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: 0, truncatable: false}],
  })
  expect(validateTask(draft.tasks.find(task => task.task === 'classify')!, draft)).toEqual([expect.stringMatching(/Classification needs both a provider and a model/)])
})

it('scopes model identity by provider and prefers the scoped record', () => {
  const global = {name: 'shared', provider: '', input_per_1m: '1', output_per_1m: '1', embedding_per_1m: '1', dimensions: '0', truncatable: false, supports_custom_dimensions: false}
  const scoped = {name: 'shared', provider: 'venice', input_per_1m: '2', output_per_1m: '2', embedding_per_1m: '2', dimensions: '512', truncatable: false, supports_custom_dimensions: false}
  expect(findModel([global, scoped], 'venice', 'shared')).toBe(scoped)
  expect(findModel([global, scoped], 'openai', 'shared')).toBe(global)
  expect(validateModel(scoped, new Set())).toEqual([])
  expect(validateModel(scoped, new Set(['venice\u0000shared']))).toEqual([expect.stringMatching(/already configured/)])
})

it('generates a unique editable provider name from a preset', () => {
  const taken = new Set(['venice', 'venice-2'])
  expect(uniqueProviderName('Venice', taken)).toBe('venice-3')
  expect(uniqueProviderName('OpenAI', new Set())).toBe('openai')
})

it('applies the blank-dimensions regression through the whole draft validation', () => {
  const draft = draftFromSettings({
    revision: 0, providers: [], tasks: LLM_TASKS.map(task => ({task, provider: '', model: '', mode: 'off' as const})),
    models: [{name: 'emb', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.1', dimensions: 0, truncatable: false}],
  })
  draft.models[0]!.dimensions = ''
  expect(validateDraft(draft, null)).toEqual([expect.stringMatching(/dimensions are required/)])
})
