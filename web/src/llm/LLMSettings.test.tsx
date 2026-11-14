// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider, useQuery} from '@tanstack/react-query'
import {request, RequestError} from '../request'
import {type DiscoveredModel, type LLMProviderInput, type LLMSettings} from './model'
import {LLMSettings as LLMSettingsSection} from './LLMSettings'

vi.mock('../request', async importOriginal => {
  const actual = await importOriginal<typeof import('../request')>()
  return {...actual, request: vi.fn()}
})

const fixture: LLMSettings = {
  revision: 3,
  providers: [
    {name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '20', timeout_seconds: 60, token_field: 'max_completion_tokens'},
    {name: 'local', kind: 'openai', base_url: 'http://ollama:11434/v1', has_api_key: false, monthly_limit_usd: '0', timeout_seconds: 120, token_field: 'max_tokens'},
  ],
  tasks: [
    {task: 'embed', provider: 'openai', model: 'text-embedding-3-small'},
    {task: 'classify', provider: '', model: ''},
    {task: 'blurb', provider: 'local', model: 'local-chat'},
    {task: 'summary', provider: '', model: ''},
    {task: 'edition', provider: '', model: ''},
    {task: 'compile_rule', provider: '', model: ''},
    {task: 'ask', provider: '', model: ''},
  ],
  models: [
    {name: 'text-embedding-3-small', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.02', dimensions: 1536, truncatable: true},
    {name: 'local-chat', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0', dimensions: 0, truncatable: false},
  ],
}

type ProviderBody = LLMProviderInput & {has_api_key?: boolean}
type Body = {
  revision: number
  op_id: string
  defaults?: {text: {provider: string; model: string}; embedding: {provider: string; model: string}}
  providers: ProviderBody[]
  tasks: Array<{task: string; provider: string; model: string; mode?: string}>
  models: Array<Record<string, unknown>>
}

const putCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path === '/v1/settings/llm' && options?.method === 'PUT')
const getCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path === '/v1/settings/llm' && !options?.method)
const lastPut = () => JSON.parse(String(putCalls().at(-1)?.[1]?.body)) as Body
const providerOf = (body: Body, name: string): ProviderBody => {
  const found = body.providers.find(provider => provider.name === name)
  if (!found) throw new Error(`the saved body has no provider ${name}`)
  return found
}
const valueOf = (label: string) => (screen.getByLabelText(label) as HTMLSelectElement).value
const confirm = (value = true) => vi.spyOn(window, 'confirm').mockReturnValue(value)

function renderSection(pipeline = false) {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  render(<QueryClientProvider client={client}>
    <LLMSettingsSection />
    {pipeline && <PipelineStatus />}
  </QueryClientProvider>)
}

function PipelineStatus() {
  const status = useQuery({
    queryKey: ['pipeline'],
    queryFn: () => request<{ask: string}>('/v1/pipeline'),
    staleTime: 30_000,
  })
  return <p>Ask {status.data?.ask ?? 'loading'}</p>
}

function mount(server: LLMSettings = fixture, catalogue: DiscoveredModel[] = []) {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm/models') return {models: catalogue}
    if (path === '/v1/settings/llm' && !options?.method) return server
    if (path === '/v1/settings/llm' && options?.method === 'PUT') return {...server, revision: server.revision + 1}
    throw new Error(`Unexpected request: ${path}`)
  })
  renderSection()
}

afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.resetAllMocks()})

it('loads the shared settings and lists all seven features with readable labels', async () => {
  mount()
  expect(await screen.findByText(/Server-wide settings/)).toBeTruthy()
  await screen.findByText(/API keys are stored encrypted/)
  for (const label of ['Embeddings', 'Classification', 'Blurbs', 'Summaries', 'Editions', 'Rule compilation', 'Ask my feeds']) {
    expect(screen.getByText(label, {exact: true, selector: 'strong'})).toBeTruthy()
  }
  expect(getCalls()).toHaveLength(1)
  expect(putCalls()).toHaveLength(0)
})

it('shows an error and a retry button when the settings cannot load', async () => {
  vi.mocked(request).mockRejectedValue(new Error('offline'))
  renderSection()
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getByText(/offline/)).toBeTruthy()
  expect(screen.getByRole('button', {name: 'Retry loading'})).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('shows the draft key status live while a provider is edited', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  expect(screen.getByText('API key saved')).toBeTruthy()
  expect(screen.getByText('No API key')).toBeTruthy()

  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-unsaved'}})
  expect(screen.getByText('API key entered · unsaved')).toBeTruthy()

  fireEvent.change(screen.getByLabelText('API key'), {target: {value: ''}})
  fireEvent.click(screen.getByLabelText('Clear the saved API key'))
  expect(screen.getByText('Key removal pending')).toBeTruthy()
})

it('keeps a saved key when the replacement field is blank and omits has_api_key', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  const form = screen.getByLabelText('Name').closest('li')!
  fireEvent.click(within(form).getByText('Advanced'))
  fireEvent.change(screen.getByLabelText('Timeout (seconds)'), {target: {value: '90'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  const openai = providerOf(body, 'openai')
  expect(openai.api_key).toBeUndefined()
  expect(openai.clear_api_key).toBe(false)
  expect(openai.has_api_key).toBeUndefined()
  expect(openai.timeout_seconds).toBe(90)
  expect(body.revision).toBe(3)
  expect(body.op_id).toEqual(expect.any(String))
  expect(body.tasks).toHaveLength(7)
})

it('keeps a replacement key visibly unsaved through failure and clears it after a successful retry', async () => {
  let fail = true
  let revision = fixture.revision
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path !== '/v1/settings/llm') throw new Error(`Unexpected request: ${path}`)
    if (options?.method === 'PUT') {
      if (fail) throw new Error('Save failed')
      revision++
    }
    return {...fixture, revision}
  })
  renderSection()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'test-replacement-key'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText('Save failed')
  expect(screen.getByText('API key entered · unsaved')).toBeTruthy()
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('test-replacement-key')
  const first = lastPut()
  fail = false
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText('AI settings saved.')
  expect(lastPut().op_id).toBe(first.op_id)
  expect(providerOf(lastPut(), 'openai').api_key).toBe('test-replacement-key')
  expect(screen.queryByText('API key entered · unsaved')).toBeNull()
  expect(screen.getByText('API key saved')).toBeTruthy()
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('')
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'temporary'}})
  confirm()
  fireEvent.click(screen.getByRole('button', {name: 'Discard'}))
  await screen.findByText('AI settings reloaded.')
  expect(screen.getByText('API key saved')).toBeTruthy()
  expect(screen.queryByText('API key entered · unsaved')).toBeNull()
})

it('edits providers directly in the draft without an apply step', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  expect(screen.queryByRole('button', {name: 'Apply provider'})).toBeNull()
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Monthly limit (USD)'), {target: {value: '30'}})
  expect((screen.getByRole('button', {name: 'Save'}) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(providerOf(lastPut(), 'openai').monthly_limit_usd).toBe('30')
})

it('keeps budget and key visible while timeout and token field stay advanced', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  expect(screen.getByLabelText('Monthly limit (USD)')).toBeTruthy()
  expect(screen.getByLabelText('API key')).toBeTruthy()
  const form = screen.getByLabelText('Name').closest('li')!
  const advanced = within(form).getByText('Advanced').closest('details')!
  expect(within(advanced).getByLabelText('Timeout (seconds)')).toBeTruthy()
  expect(within(advanced).getByLabelText('Token field')).toBeTruthy()
})

it('adds preset providers with unique editable names', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByRole('button', {name: 'Add Venice'}))
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('venice')
  expect((screen.getByLabelText('Kind') as HTMLSelectElement).value).toBe('openai')
  expect((screen.getByLabelText('Base URL') as HTMLInputElement).value).toBe('https://api.venice.ai/api/v1')

  fireEvent.click(screen.getByRole('button', {name: 'Add Venice'}))
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('venice-2')

  fireEvent.click(screen.getByRole('button', {name: 'Add Custom'}))
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('custom')
  expect((screen.getByLabelText('Base URL') as HTMLInputElement).value).toBe('')
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'my-server'}})
  fireEvent.change(screen.getByLabelText('Base URL'), {target: {value: 'https://example.test/v1'}})
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-new'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(providerOf(lastPut(), 'my-server')).toMatchObject({kind: 'openai', base_url: 'https://example.test/v1', api_key: 'sk-new'})
})

it('renames a provider and moves its task routes and scoped models', async () => {
  const catalogue = [{id: 'scoped-chat', name: 'Scoped Chat', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '1'}]
  mount(fixture, catalogue)
  await screen.findByText(/http:\/\/ollama:11434\/v1/)
  const row = screen.getByText('Blurbs', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  fireEvent.click(await within(row).findByRole('button', {name: 'Select scoped-chat'}))

  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'renamed-local'}})
  expect((within(row).getByLabelText('Blurbs provider') as HTMLSelectElement).value).toBe('renamed-local')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toContain('renamed-local')
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'renamed-local', model: 'scoped-chat'})
  expect(body.models.find(model => model.name === 'scoped-chat')).toMatchObject({provider: 'renamed-local'})
})

it('sends default selections and preserves legacy routes as custom', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.change(screen.getByLabelText('Default text model provider'), {target: {value: 'openai'}})
  fireEvent.change(screen.getByLabelText('Default text model model'), {target: {value: 'local-chat'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.defaults).toEqual({text: {provider: 'openai', model: 'local-chat'}, embedding: {provider: '', model: ''}})
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'local', model: 'local-chat'})
  expect(body.tasks.find(task => task.task === 'classify')).toEqual({task: 'classify', mode: 'off', provider: '', model: ''})
})

it('does not enable an off feature when a default is chosen', async () => {
  mount({...fixture, defaults: {text: {provider: 'openai', model: 'local-chat'}, embedding: {provider: '', model: ''}}})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Classification', {selector: 'strong'}).closest('li')!
  const toggle = within(row).getByLabelText('Classification') as HTMLInputElement
  expect(toggle.checked).toBe(false)
  fireEvent.change(screen.getByLabelText('Default text model model'), {target: {value: 'text-embedding-3-small'}})
  expect(toggle.checked).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(lastPut().tasks.find(task => task.task === 'classify')).toEqual({task: 'classify', mode: 'off', provider: '', model: ''})
})

it('resolves a default feature through the text default and shows the route', async () => {
  const server: LLMSettings = {
    ...fixture,
    defaults: {text: {provider: 'openai', model: 'local-chat'}, embedding: {provider: 'local', model: 'text-embedding-3-small'}},
    tasks: fixture.tasks!.map(task => task.task === 'classify' ? {task: task.task, provider: '', model: '', mode: 'default'} : task),
  }
  mount(server)
  await screen.findByText(/Uses openai · local-chat/)
  const row = screen.getByText('Classification', {selector: 'strong'}).closest('li')!
  expect((within(row).getByLabelText('Classification') as HTMLInputElement).checked).toBe(true)
})

it('adds a ready model from the provider picker and routes the feature to it', async () => {
  const catalogue = [{id: 'new-chat', name: 'New Chat', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '2'}]
  mount(fixture, catalogue)
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Blurbs', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  expect(await within(row).findByText('New Chat (new-chat)')).toBeTruthy()
  fireEvent.click(within(row).getByRole('button', {name: 'Select new-chat'}))
  expect(valueOf('Blurbs model')).toBe('new-chat')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'local', model: 'new-chat'})
  expect(body.models.find(model => model.name === 'new-chat')).toMatchObject({provider: 'local', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: 0})
})

it('adds a provider-scoped model and keeps the global record', async () => {
  const catalogue = [{id: 'shared', name: 'Shared', chat: true, embedding: false, input_per_1m: '5', output_per_1m: '5'}]
  const server: LLMSettings = {
    ...fixture,
    models: [...fixture.models!, {name: 'shared', input_per_1m: '1', output_per_1m: '1', embedding_per_1m: '1', dimensions: 0, truncatable: false}],
  }
  mount(server, catalogue)
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Blurbs', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  await within(row).findByText('Shared (shared)')
  fireEvent.click(within(row).getByRole('button', {name: 'Select shared'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.models.find(model => model.name === 'shared' && model.provider === 'local')).toMatchObject({input_per_1m: '5', output_per_1m: '5'})
  expect(body.models.find(model => model.name === 'shared' && !model.provider)).toMatchObject({input_per_1m: '1', output_per_1m: '1'})
})

it('accepts Venice embedding geometry with provider resizing above 1024', async () => {
  const venice = {name: 'venice', kind: 'openai', base_url: 'https://api.venice.ai/api/v1', has_api_key: false, monthly_limit_usd: '0', timeout_seconds: 60, token_field: 'max_completion_tokens'}
  const server: LLMSettings = {
    ...fixture,
    providers: [venice, fixture.providers![1]!],
    tasks: fixture.tasks!.map(task => task.task === 'embed' ? {task: 'embed', provider: 'venice', model: ''} : task),
  }
  const catalogue = [{id: 'text-embedding-qwen3-8b', name: 'Qwen3 8B', embedding: true, embedding_per_1m: '0.0125', dimensions: 4096, supports_custom_dimensions: true}]
  mount(server, catalogue)
  await screen.findByText(/https:\/\/api\.venice\.ai\/api\/v1/)
  const row = screen.getByText('Embeddings', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  await within(row).findByText('Qwen3 8B (text-embedding-qwen3-8b)')
  fireEvent.click(within(row).getByRole('button', {name: 'Select text-embedding-qwen3-8b'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.models.find(model => model.name === 'text-embedding-qwen3-8b')).toMatchObject({provider: 'venice', dimensions: 4096, supports_custom_dimensions: true, embedding_per_1m: '0.0125'})
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'venice', model: 'text-embedding-qwen3-8b'})
})

it('reports missing dimensions inline instead of an invalid integer', async () => {
  const catalogue = [{id: 'emb-no-dims', name: 'Emb No Dims', embedding: true, embedding_per_1m: '0.1'}]
  mount(fixture, catalogue)
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Embeddings', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  fireEvent.click(await within(row).findByRole('button', {name: 'Select emb-no-dims, missing dimensions'}))
  fireEvent.click(within(row).getByRole('button', {name: 'Add to draft and use for Embeddings'}))
  expect(await within(row).findByText(/dimensions are required/)).toBeTruthy()
  expect(within(row).queryByText(/must be a whole number/)).toBeNull()
  expect(putCalls()).toHaveLength(0)
  fireEvent.change(within(row).getByLabelText('emb-no-dims dimensions'), {target: {value: '512'}})
  fireEvent.click(within(row).getByRole('button', {name: 'Add to draft and use for Embeddings'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
})

it('applies observed geometry from a model check to the selection only', async () => {
  const catalogue = [{id: 'emb-check', name: 'Emb Check', embedding: true, embedding_per_1m: '0.1'}]
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm/models') return {models: catalogue}
    if (path === '/v1/settings/llm/models/check') return {compatible: true, dimensions: 2048, supports_custom_dimensions: true, message: 'compatible'}
    if (path === '/v1/settings/llm' && !options?.method) return fixture
    return {...fixture, revision: fixture.revision + 1}
  })
  renderSection()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Embeddings', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  fireEvent.click(await within(row).findByRole('button', {name: 'Select emb-check, missing dimensions'}))
  fireEvent.change(within(row).getByLabelText('emb-check dimensions'), {target: {value: '4096'}})
  fireEvent.click(within(row).getByRole('button', {name: 'Check model'}))
  await within(row).findByText('compatible')
  expect((within(row).getByLabelText('emb-check dimensions') as HTMLInputElement).value).toBe('2048')
  expect((within(row).getByLabelText('emb-check embedding price per 1m tokens') as HTMLInputElement).value).toBe('0.1')
  fireEvent.click(within(row).getByRole('button', {name: 'Add to draft and use for Embeddings'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(lastPut().models.find(model => model.name === 'emb-check')).toMatchObject({dimensions: 2048, supports_custom_dimensions: true, embedding_per_1m: '0.1'})
})

it('turns a feature off with its switch', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByLabelText('Embeddings'))
  expect((screen.getByLabelText('Embeddings') as HTMLInputElement).checked).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'off', provider: '', model: ''})
})

it.each(['disable', 'default'])('closes a feature picker when its route is set to %s', async action => {
  mount(fixture, [{id: 'new-chat', name: 'New Chat', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '2'}])
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Blurbs', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  await within(row).findByRole('button', {name: 'Select new-chat'})
  if (action === 'disable') fireEvent.click(within(row).getByLabelText('Blurbs'))
  else fireEvent.change(within(row).getByLabelText('Blurbs provider'), {target: {value: ''}})
  expect(within(row).queryByRole('button', {name: 'Select new-chat'})).toBeNull()
  expect(within(row).queryByRole('button', {name: 'Enter model ID manually'})).toBeNull()
})

it('requires an explicit model selection after a feature changes provider', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.change(screen.getByLabelText('Blurbs provider'), {target: {value: 'openai'}})
  expect(valueOf('Blurbs model')).toBe('')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/Blurbs needs both a provider and a model/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('refuses to save a half-configured custom route', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByLabelText('Classification'))
  fireEvent.change(screen.getByLabelText('Classification provider'), {target: {value: 'openai'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/Classification needs both a provider and a model/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('blocks a Default feature when no default is configured', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByLabelText('Classification'))
  expect(screen.getByText(/Choose a default model/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/no default text model is configured/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('keeps the draft and asks for a reload on a revision conflict', async () => {
  confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') throw new RequestError(409, 'revision conflict')
    return fixture
  })
  fireEvent.click(screen.getByLabelText('Embeddings'))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/changed after you loaded/)).toBeTruthy()
  expect((screen.getByLabelText('Embeddings') as HTMLInputElement).checked).toBe(false)
  expect(getCalls()).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', {name: 'Discard'}))
  await screen.findByText(/AI settings reloaded/)
  expect((screen.getByLabelText('Embeddings') as HTMLInputElement).checked).toBe(true)
})

it('keeps the draft and reuses the op_id when a save fails and is retried', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') throw new Error('server exploded')
    return fixture
  })
  fireEvent.click(screen.getByLabelText('Embeddings'))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/server exploded/)).toBeTruthy()
  expect((screen.getByLabelText('Embeddings') as HTMLInputElement).checked).toBe(false)
  const first = lastPut().op_id
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls()).toHaveLength(2))
  expect(lastPut().op_id).toBe(first)
})

it('discards unsaved changes after confirmation', async () => {
  confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  expect((screen.getByRole('button', {name: 'Save'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText('Default text model provider'), {target: {value: 'openai'}})
  expect((screen.getByRole('button', {name: 'Save'}) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: 'Discard'}))
  await screen.findByText(/AI settings reloaded/)
  expect(valueOf('Default text model provider')).toBe('')
})

it('keeps an open provider editor attached when another provider is removed', async () => {
  confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'edited-local'}})
  fireEvent.click(screen.getAllByRole('button', {name: 'Remove'})[0])
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('edited-local')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(lastPut().providers.map(provider => provider.name)).toEqual(['edited-local'])
  expect(lastPut().tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'edited-local', model: 'local-chat'})
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'off', provider: '', model: ''})
})

it('removes a provider whose name field is temporarily empty', async () => {
  confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: ''}})
  fireEvent.click(screen.getByRole('button', {name: 'Remove provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  expect(lastPut().providers.map(provider => provider.name)).toEqual(['local'])
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'off', provider: '', model: ''})
})

it('removes a provider and turns off the features that used it', async () => {
  const answer = confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Remove'})[1])
  expect(answer).toHaveBeenCalledWith(expect.stringMatching(/Blurbs/))
  expect(screen.queryByText(/http:\/\/ollama:11434\/v1/)).toBeNull()
  expect((screen.getByLabelText('Blurbs') as HTMLInputElement).checked).toBe(false)
})

it('refuses to route embeddings to Anthropic', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByRole('button', {name: 'Add Anthropic'}))
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'claude'}})
  fireEvent.change(screen.getByLabelText('Embeddings provider'), {target: {value: 'claude'}})
  fireEvent.change(screen.getByLabelText('Embeddings model'), {target: {value: 'text-embedding-3-small'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/Task Embeddings cannot use Anthropic/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('round-trips scoped model pricing and accepts provider resizing above 1024', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.change(screen.getByLabelText('text-embedding-3-small embedding price'), {target: {value: '0.03'}})
  fireEvent.change(screen.getByLabelText('Default embedding model provider'), {target: {value: 'openai'}})
  const route = screen.getByLabelText('Default embedding model provider').parentElement!
  fireEvent.click(within(route).getByRole('button', {name:'Browse models'}))
  await screen.findByText(/No catalogue entries match/)
  fireEvent.click(screen.getByRole('button', {name:'Enter model ID manually'}))
  fireEvent.change(screen.getByLabelText('Manual model ID'), {target:{value:'big-embed'}})
  fireEvent.click(screen.getByRole('button',{name:'Add model ID'}))
  fireEvent.change(screen.getByLabelText('big-embed embedding price per 1m tokens'), {target:{value:'0.1'}})
  fireEvent.change(screen.getByLabelText('big-embed dimensions'), {target:{value:'4096'}})
  fireEvent.click(screen.getByLabelText('big-embed supports custom dimensions'))
  fireEvent.click(screen.getByRole('button', {name: 'Add to draft and use for the default embedding model'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.models.find(model => model.name === 'text-embedding-3-small')).toMatchObject({embedding_per_1m: '0.03'})
  expect(body.models.find(model => model.name === 'big-embed')).toMatchObject({provider: 'openai', dimensions: 4096, supports_custom_dimensions: true, embedding_per_1m: '0.1'})
})

it('reports blank model dimensions as missing', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.change(screen.getByLabelText('text-embedding-3-small dimensions'), {target: {value: ''}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/dimensions are required/)).toBeTruthy()
  expect(screen.queryByText(/dimensions must be a whole number/)).toBeNull()
})

it('rejects an invalid price without saving', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.change(screen.getByLabelText('text-embedding-3-small embedding price'), {target: {value: '0.0000001'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/at most six decimals/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('keeps typed keys in component state only', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-secret-value'}})
  for (const store of [localStorage, sessionStorage]) {
    for (let i = 0; i < store.length; i++) expect(store.getItem(store.key(i) ?? '') ?? '').not.toContain('sk-secret-value')
  }
})

it.each([true, false])('refreshes cached task availability after changing Ask', async enabled => {
  let active = !enabled
  const settings = (revision: number): LLMSettings => ({
    ...fixture,
    revision,
    defaults: {text: {provider: 'local', model: 'local-chat'}, embedding: {provider: '', model: ''}},
    tasks: fixture.tasks!.map(task => task.task === 'ask'
      ? {...task, provider: active ? 'local' : '', model: active ? 'local-chat' : '', mode: active ? 'custom' : 'off'}
      : task),
  })
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/pipeline') return {ask: active ? 'enabled' : 'disabled'}
    if (options?.method === 'PUT') {
      active = enabled
      return settings(4)
    }
    return settings(3)
  })
  renderSection(true)
  await screen.findByText(`Ask ${enabled ? 'disabled' : 'enabled'}`)
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)

  fireEvent.click(screen.getByLabelText('Ask my feeds'))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))

  await screen.findByText(/AI settings saved/)
  expect(await screen.findByText(`Ask ${enabled ? 'enabled' : 'disabled'}`)).toBeTruthy()
  expect(vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/pipeline')).toHaveLength(2)
})

it('ignores stale picker responses after a provider edit', async () => {
  mount(fixture, [{id: 'late', name: 'Late', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '1'}])
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  const row = screen.getByText('Blurbs', {selector: 'strong'}).closest('li')!
  fireEvent.click(within(row).getByRole('button', {name: 'Browse models'}))
  await within(row).findByText(/Fetching models/)
  // Editing the task provider discards the in-flight catalogue.
  fireEvent.change(within(row).getByLabelText('Blurbs provider'), {target: {value: 'openai'}})
  expect(await within(row).findByText(/results were discarded/)).toBeTruthy()
  expect(within(row).queryByText('Late (late)')).toBeNull()
})
