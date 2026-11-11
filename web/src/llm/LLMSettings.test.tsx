// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request, RequestError} from '../request'
import {LLM_TASKS, type LLMProviderInput, type LLMSettings} from './model'
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
const lastPut = () => JSON.parse(String(putCalls().at(-1)?.[1]?.body)) as Body
const providerOf = (body: Body, name: string): ProviderBody => {
  const found = body.providers.find(provider => provider.name === name)
  if (!found) throw new Error(`the saved body has no provider ${name}`)
  return found
}

function mount(server: LLMSettings = fixture) {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm' && !options?.method) return server
    return {...server, revision: server.revision + 1}
  })
  render(<LLMSettingsSection />)
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
  render(<LLMSettingsSection />)
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getByText(/offline/)).toBeTruthy()
  expect(screen.getByRole('button', {name: 'Load'})).toBeTruthy()
  expect(putCalls()).toHaveLength(0)
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
