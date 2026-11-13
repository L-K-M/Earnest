// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider, useQuery} from '@tanstack/react-query'
import {request, RequestError} from '../request'
import {LLM_TASKS, type DiscoveredModel, type LLMProviderInput, type LLMSettings} from './model'
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

type Task = {task: string; provider: string; model: string}
type ProviderBody = LLMProviderInput & {has_api_key?: boolean}
type Body = {revision: number; op_id: string; providers: ProviderBody[]; tasks: Task[]; models: Record<string, unknown>[]}

const putCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path === '/v1/settings/llm' && options?.method === 'PUT')
const getCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path === '/v1/settings/llm' && !options?.method)
const modelCalls = () => vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/settings/llm/models')
const lastDiscovery = () => JSON.parse(String(modelCalls().at(-1)?.[1]?.body)) as {revision: number; provider: ProviderBody}
const lastPut = () => JSON.parse(String(putCalls().at(-1)?.[1]?.body)) as Body
const providerOf = (body: Body, name: string): ProviderBody => {
  const found = body.providers.find(provider => provider.name === name)
  if (!found) throw new Error(`the saved body has no provider ${name}`)
  return found
}

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

function mount(server: LLMSettings = fixture) {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm' && !options?.method) return server
    return {...server, revision: server.revision + 1}
  })
  renderSection()
}

const confirm = (value = true) => vi.spyOn(window, 'confirm').mockReturnValue(value)
const valueOf = (label: string) => (screen.getByLabelText(label) as HTMLSelectElement).value

afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.resetAllMocks()})

it('loads the shared settings, lists all seven tasks and issues no mutations on load', async () => {
  mount()
  expect(await screen.findByText(/https:\/\/api\.openai\.com\/v1/)).toBeTruthy()
  expect(screen.getByText(/http:\/\/ollama:11434\/v1/)).toBeTruthy()
  expect(screen.getByText(/shared by the whole server/)).toBeTruthy()
  expect(screen.getByText(/never sent back to the browser/)).toBeTruthy()
  for (const task of LLM_TASKS) expect(screen.getByText(task, {exact: true})).toBeTruthy()
  expect(getCalls()).toHaveLength(1)
  expect(putCalls()).toHaveLength(0)
})

it('shows an error and a Load button when the settings cannot load', async () => {
  vi.mocked(request).mockRejectedValue(new Error('offline'))
  renderSection()
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getByText(/offline/)).toBeTruthy()
  expect(screen.getByRole('button', {name: 'Load'})).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('shows the draft key status: saved, entered-unsaved, removal pending and none', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  expect(screen.getByText('API key saved')).toBeTruthy()
  expect(screen.getByText('No API key')).toBeTruthy()

  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-unsaved'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(screen.getByText('API key entered · unsaved')).toBeTruthy()
  expect(screen.queryByText('API key saved')).toBeNull()

  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: ''}})
  fireEvent.click(screen.getByLabelText('Clear the saved API key'))
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(screen.getByText('Key removal pending')).toBeTruthy()
})

it('keeps a saved key when the replacement field is blank and omits has_api_key', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Timeout (seconds)'), {target: {value: '90'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
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

it.each([true, false])('refreshes cached task availability after setting Ask enabled=%s', async enabled => {
  let active = !enabled
  const settings = (revision: number): LLMSettings => ({
    ...fixture,
    revision,
    tasks: (fixture.tasks ?? []).map(task => task.task === 'ask'
      ? {...task, provider: active ? 'local' : '', model: active ? 'local-chat' : ''}
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

  fireEvent.change(screen.getByLabelText('ask provider'), {target: {value: enabled ? 'local' : ''}})
  if (enabled) fireEvent.change(screen.getByLabelText('ask model'), {target: {value: 'local-chat'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))

  await screen.findByText(/LLM services saved/)
  expect(await screen.findByText(`Ask ${enabled ? 'enabled' : 'disabled'}`)).toBeTruthy()
  expect(vi.mocked(request).mock.calls.filter(([path]) => path === '/v1/pipeline')).toHaveLength(2)
})

it('does not mark the draft dirty when a saved key is left blank', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-typed-then-cleared'}})
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: ''}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(screen.getByText('No unsaved changes')).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('sends a typed replacement key and clears only when the clear box is checked', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-replacement'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  const replaced = providerOf(lastPut(), 'openai')
  expect(replaced.api_key).toBe('sk-replacement')
  expect(replaced.clear_api_key).toBe(false)

  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.click(screen.getByLabelText('Clear the saved API key'))
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls()).toHaveLength(2))
  const cleared = providerOf(lastPut(), 'openai')
  expect(cleared.api_key).toBeUndefined()
  expect(cleared.clear_api_key).toBe(true)
})

it('adds a provider with the kind default base URL and its key', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByRole('button', {name: 'Add provider'}))
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'claude'}})
  fireEvent.change(screen.getByLabelText('Kind'), {target: {value: 'anthropic'}})
  expect((screen.getByLabelText('Base URL') as HTMLInputElement).value).toBe('https://api.anthropic.com')
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-ant'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  const claude = providerOf(lastPut(), 'claude')
  expect(claude).toMatchObject({kind: 'anthropic', base_url: 'https://api.anthropic.com', api_key: 'sk-ant', clear_api_key: false})
  expect(claude.has_api_key).toBeUndefined()
})

it('keeps task routes on an unkeyed provider when it is renamed', async () => {
  mount()
  await screen.findByText(/http:\/\/ollama:11434\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'renamed-local'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))

  expect(valueOf('blurb provider')).toBe('renamed-local')
  expect(valueOf('blurb model')).toBe('local-chat')
  expect(valueOf('ask provider')).toBe('')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', provider: 'renamed-local', model: 'local-chat'})
})

it('requires a new key or an explicit clear when a keyed provider moves', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Base URL'), {target: {value: 'https://elsewhere.example/v1'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(await screen.findByText(/keeps its saved API key/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('round-trips model prices and adds a model with explicit rates', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.change(screen.getByLabelText('text-embedding-3-small embedding price'), {target: {value: '0.03'}})
  fireEvent.click(screen.getByRole('button', {name: 'Add model'}))
  fireEvent.change(screen.getByLabelText('New model name'), {target: {value: 'new-model'}})
  fireEvent.change(screen.getByLabelText('New model input price'), {target: {value: '1.5'}})
  fireEvent.change(screen.getByLabelText('New model output price'), {target: {value: '3'}})
  fireEvent.change(screen.getByLabelText('New model embedding price'), {target: {value: '0'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply model'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  const body = lastPut()
  const priced = body.models.find(model => model.name === 'text-embedding-3-small')
  expect(priced).toMatchObject({input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.03', dimensions: 1536, truncatable: true})
  const added = body.models.find(model => model.name === 'new-model')
  expect(added).toMatchObject({input_per_1m: '1.5', output_per_1m: '3', embedding_per_1m: '0', dimensions: 0, truncatable: false})
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

it('turns a task off by clearing its provider and its model', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.change(screen.getByLabelText('embed provider'), {target: {value: ''}})
  expect(valueOf('embed model')).toBe('')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', provider: '', model: ''})
})

it('refuses to save a half-configured task', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.change(screen.getByLabelText('classify provider'), {target: {value: 'openai'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/Task classify needs both a provider and a model/)).toBeTruthy()
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
  fireEvent.change(screen.getByLabelText('embed provider'), {target: {value: ''}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/changed after you loaded/)).toBeTruthy()
  expect(valueOf('embed provider')).toBe('')
  expect(getCalls()).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', {name: 'Reload'}))
  await screen.findByText(/reloaded/)
  expect(valueOf('embed provider')).toBe('openai')
})

it('keeps the draft and reuses the op_id when a save fails and is retried', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') throw new Error('server exploded')
    return fixture
  })
  fireEvent.change(screen.getByLabelText('embed provider'), {target: {value: ''}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/server exploded/)).toBeTruthy()
  expect(valueOf('embed provider')).toBe('')
  const first = lastPut().op_id
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls()).toHaveLength(2))
  expect(lastPut().op_id).toBe(first)
})

it('removes a provider and turns off the tasks that used it', async () => {
  const answer = confirm()
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Remove'})[1])
  expect(answer).toHaveBeenCalledWith(expect.stringMatching(/blurb/))
  expect(screen.queryByText(/http:\/\/ollama:11434\/v1/)).toBeNull()
  expect(valueOf('blurb provider')).toBe('')
})

it('refuses to route embed to Anthropic', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByRole('button', {name: 'Add provider'}))
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'claude'}})
  fireEvent.change(screen.getByLabelText('Kind'), {target: {value: 'anthropic'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.change(screen.getByLabelText('embed provider'), {target: {value: 'claude'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/Task embed cannot use Anthropic/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('rejects dimensions above 1024 when the model is not truncatable', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.click(screen.getByLabelText('text-embedding-3-small truncatable'))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/dimensions above 1024 need truncatable vectors/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('rejects a base URL with a query or fragment', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Base URL'), {target: {value: 'https://api.openai.com/v1?x=1'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(await screen.findByText(/must not contain a query or fragment/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('rejects a provider name that is not lowercase letters, digits and single dashes', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByRole('button', {name: 'Add provider'}))
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'OpenAI Prod'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(await screen.findByText(/lowercase letters, digits and single dashes/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('rejects a model name with whitespace', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getByText('Model pricing'))
  fireEvent.click(screen.getByRole('button', {name: 'Add model'}))
  fireEvent.change(screen.getByLabelText('New model name'), {target: {value: 'my model'}})
  fireEvent.change(screen.getByLabelText('New model input price'), {target: {value: '0'}})
  fireEvent.change(screen.getByLabelText('New model output price'), {target: {value: '0'}})
  fireEvent.change(screen.getByLabelText('New model embedding price'), {target: {value: '0'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply model'}))
  expect(await screen.findByText(/must not contain whitespace/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('rejects a key containing control characters', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-secret\u0001'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect(await screen.findByText(/control characters/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it.each([2, 3])('keeps an open provider editor attached when there are %i providers', async count => {
  const providers = [...fixture.providers!, {...fixture.providers![1], name: 'third', base_url: 'https://third.example/v1'}].slice(0, count)
  mount({...fixture, providers, tasks: LLM_TASKS.map(task => ({task, provider: '', model: ''}))})
  confirm()
  await screen.findByText(/http:\/\/ollama:11434\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'edited-local'}})
  const remove = screen.getAllByRole('button', {name: 'Remove'})[0]
  expect((remove as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(remove)
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('edited-local')
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getAllByRole('button', {name: 'Remove'})[0])
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().providers.map(provider => provider.name)).toEqual(count === 2 ? ['edited-local'] : ['edited-local', 'third'])
})

it('labels the server canonical zero cap as unlimited', async () => {
  mount({...fixture, providers: fixture.providers!.map(provider => ({...provider, monthly_limit_usd: '0.000000'}))})
  await screen.findByText(/http:\/\/ollama:11434\/v1/)
  expect(screen.getAllByText(/no monthly limit/)).toHaveLength(2)
})

it('requires applying provider edits before saving the whole configuration', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.change(screen.getByLabelText('ask provider'), {target: {value: 'local'}})
  fireEvent.change(screen.getByLabelText('ask model'), {target: {value: 'local-chat'}})
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-not-yet-applied'}})
  expect((screen.getByRole('button', {name: 'Save'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  expect((screen.getByRole('button', {name: 'Save'}) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(providerOf(lastPut(), 'openai').api_key).toBe('sk-not-yet-applied')
})

it('keeps typed keys in component state only', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-secret-value'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  for (const store of [localStorage, sessionStorage]) {
    for (let i = 0; i < store.length; i++) expect(store.getItem(store.key(i) ?? '') ?? '').not.toContain('sk-secret-value')
  }
})

const catalogue: DiscoveredModel[] = [
  {id: 'gpt-5', name: 'GPT-5', chat: true, embedding: false, input_per_1m: '1.25', output_per_1m: '10'},
  {id: 'embed-one', name: 'Embed One', chat: false, embedding: true, embedding_per_1m: '0.1', dimensions: 512},
  {id: 'dual-use', name: 'Dual Use', chat: true, embedding: true, input_per_1m: '2', output_per_1m: '4', embedding_per_1m: '0.5', dimensions: 768},
  {id: 'text-embedding-3-small', name: 'Embedding Small', embedding: true, embedding_per_1m: '0.04', dimensions: 512},
  {id: 'mystery', name: 'Mystery'},
]

function mountCatalogue(answer: {models: DiscoveredModel[]} | (() => never), server: LLMSettings = fixture) {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm/models') {
      if (typeof answer === 'function') answer()
      return answer as {models: DiscoveredModel[]}
    }
    if (path === '/v1/settings/llm' && !options?.method) return server
    return {...server, revision: server.revision + 1}
  })
  renderSection()
}

const fetchButtons = () => screen.getAllByRole('button', {name: 'Fetch models'})

it('fetches the catalogue with the draft provider and lets the server inherit a blank key', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  expect(await screen.findByText('GPT-5 (gpt-5)')).toBeTruthy()
  const sent = lastDiscovery()
  expect(sent.revision).toBe(3)
  expect(sent.provider.name).toBe('openai')
  expect(sent.provider.api_key).toBeUndefined()
  expect(sent.provider.clear_api_key).toBe(false)
  expect(sent.provider.has_api_key).toBeUndefined()
  expect(putCalls()).toHaveLength(0)
})

it('sends a replacement key typed in the editor without applying it', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'sk-editor-only'}})
  // The editor's Fetch models button renders before the other rows' buttons.
  fireEvent.click(fetchButtons()[0])
  expect(await screen.findByText('Mystery (mystery)')).toBeTruthy()
  expect(lastDiscovery().provider.api_key).toBe('sk-editor-only')
  expect(screen.getByText(/Apply provider writes these edits to the draft/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('filters known-incompatible models for a task and labels unknown capabilities', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  // Providers occupy the first two Fetch models buttons; embed's is third.
  fireEvent.click(fetchButtons()[2])
  expect(await screen.findByText('Embed One (embed-one)')).toBeTruthy()
  expect(screen.queryByText('GPT-5 (gpt-5)')).toBeNull()
  expect(screen.getByText(/cannot do embedding work/)).toBeTruthy()
  expect(screen.getByText('capabilities unknown')).toBeTruthy()
  expect(screen.getByText('Embedding Small (text-embedding-3-small)')).toBeTruthy()
})

it('searches the catalogue without changing the task route', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[3])
  expect(await screen.findByText('GPT-5 (gpt-5)')).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Search catalogue'), {target: {value: 'dual'}})
  expect(screen.queryByText('GPT-5 (gpt-5)')).toBeNull()
  expect(screen.getByText('Dual Use (dual-use)')).toBeTruthy()
  expect(valueOf('blurb provider')).toBe('local')
  expect(valueOf('blurb model')).toBe('local-chat')
})

it('imports a configured catalogue ID without touching its prices', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  await screen.findByText('Embedding Small (text-embedding-3-small)')
  fireEvent.click(screen.getByLabelText('Select text-embedding-3-small'))
  fireEvent.click(screen.getByLabelText('Select mystery'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  expect(await screen.findByText(/rates are suggestions/i)).toBeTruthy()
  // The already-configured ID keeps its prices and never reaches the review.
  expect(screen.queryByLabelText('text-embedding-3-small configured name')).toBeNull()
  expect(screen.getByLabelText('mystery configured name')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', {name: 'Back to catalogue'}))
  expect(screen.getByLabelText('Select text-embedding-3-small')).toBeTruthy()
})

it('requires missing applicable prices and geometry in the price review', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  await screen.findByText('Mystery (mystery)')
  fireEvent.click(screen.getByLabelText('Select mystery'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  fireEvent.click(await screen.findByRole('button', {name: 'Import model to draft'}))
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getAllByText(/non-negative amount/)).toHaveLength(3)
  expect(screen.getByText(/must be a whole number/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('defaults only the inapplicable side for a known embedding-only model', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  await screen.findByText('Embed One (embed-one)')
  fireEvent.click(screen.getByLabelText('Select embed-one'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  expect((await screen.findByLabelText('embed-one input price') as HTMLInputElement).value).toBe('0')
  expect((screen.getByLabelText('embed-one output price') as HTMLInputElement).value).toBe('0')
  expect((screen.getByLabelText('embed-one embedding price') as HTMLInputElement).value).toBe('0.1')
  expect((screen.getByLabelText('embed-one dimensions') as HTMLInputElement).value).toBe('512')
  fireEvent.click(screen.getByRole('button', {name: 'Import model to draft'}))
  await screen.findByText(/added to the draft/)
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().models.find(model => model.name === 'embed-one')).toMatchObject({input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.1', dimensions: 512})
})

it('defaults embedding price and geometry to zero for a known chat-only model', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  await screen.findByText('GPT-5 (gpt-5)')
  fireEvent.click(screen.getByLabelText('Select gpt-5'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  expect((await screen.findByLabelText('gpt-5 embedding price') as HTMLInputElement).value).toBe('0')
  expect((screen.getByLabelText('gpt-5 dimensions') as HTMLInputElement).value).toBe('0')
  expect((screen.getByLabelText('gpt-5 input price') as HTMLInputElement).value).toBe('1.25')
})

it('routes a task only after an explicit selection and review', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[2])
  expect(await screen.findByText('Embed One (embed-one)')).toBeTruthy()
  expect(valueOf('embed model')).toBe('text-embedding-3-small')
  fireEvent.click(screen.getAllByRole('button', {name: 'Use for embed'})[0])
  fireEvent.click(await screen.findByRole('button', {name: 'Import model to draft'}))
  await screen.findByText(/added to the draft/)
  expect(valueOf('embed model')).toBe('embed-one')
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', provider: 'openai', model: 'embed-one'})
})

it('discards catalogue results when the provider draft changes', async () => {
  mountCatalogue({models: catalogue})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[2])
  expect(await screen.findByText('Embed One (embed-one)')).toBeTruthy()
  fireEvent.change(screen.getByLabelText('embed provider'), {target: {value: 'local'}})
  expect(await screen.findByText(/results were discarded/)).toBeTruthy()
  expect(screen.queryByText('Embed One (embed-one)')).toBeNull()
})

it('keeps configured selections usable when discovery fails', async () => {
  mountCatalogue(() => { throw new RequestError(503, 'provider returned no model catalogue') })
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[2])
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getByText(/no model catalogue/)).toBeTruthy()
  expect(valueOf('embed model')).toBe('text-embedding-3-small')
  // Configured models remain the fallback when the catalogue is unavailable.
  fireEvent.change(screen.getByLabelText('embed model'), {target: {value: 'local-chat'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/LLM services saved/)
  expect(lastPut().tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', provider: 'openai', model: 'local-chat'})
})

it('falls back to manual entry when the server cannot discover', async () => {
  mountCatalogue(() => { throw new RequestError(404, 'not found') })
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  expect(await screen.findByText(/does not support model discovery; add models manually/)).toBeTruthy()
})

it('asks for a reload when the settings revision is stale', async () => {
  mountCatalogue(() => { throw new RequestError(409, 'revision conflict') })
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  expect(await screen.findByText(/Reload, then fetch again/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('refuses catalogue imports beyond the free configured-model slots', async () => {
  const models = Array.from({length: 127}, (_, i) => ({name: `m${i}`, input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0', dimensions: 0, truncatable: false}))
  mountCatalogue({models: catalogue}, {...fixture, models})
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(fetchButtons()[0])
  await screen.findByText('GPT-5 (gpt-5)')
  fireEvent.click(screen.getByLabelText('Select gpt-5'))
  fireEvent.click(screen.getByLabelText('Select mystery'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  expect(await screen.findByText(/1 slot is free/)).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
})

it('keeps an entered key visibly unsaved after failure, then shows saved after retry and reload', async () => {
  let fail = true
  vi.mocked(request).mockImplementation(async (_path, options) => {
    if (options?.method === 'PUT') {
      if (fail) throw new RequestError(503, 'Try again')
      return {...fixture, revision: 4}
    }
    return fixture
  })
  renderSection()
  await screen.findByText('API key saved')
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('API key'), {target: {value: 'replacement-key'}})
  fireEvent.click(screen.getByRole('button', {name: 'Apply provider'}))
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText('Try again')
  expect(screen.getByText('API key entered · unsaved')).toBeTruthy()
  fail = false
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText('LLM services saved.')
  expect(screen.getByText('API key saved')).toBeTruthy()
  expect(screen.queryByText('API key entered · unsaved')).toBeNull()
  fireEvent.click(screen.getByRole('button', {name: 'Reload'}))
  await screen.findByText('LLM services reloaded.')
  expect(screen.getByText('API key saved')).toBeTruthy()
})
