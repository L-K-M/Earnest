// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {request} from '../request'
import {type LLMProviderInput, type LLMSettings} from './model'
import {LLMSettings as LLMSettingsSection} from './LLMSettings'

vi.mock('../request', async importOriginal => {
  const actual = await importOriginal<typeof import('../request')>()
  return {...actual, request: vi.fn()}
})

// Two providers with their own explicit routes, defaults and scoped models,
// so a rename that moves the wrong rows is observable.
const fixture: LLMSettings = {
  revision: 5,
  providers: [
    {name: 'openai', kind: 'openai', base_url: 'https://api.openai.com/v1', has_api_key: true, monthly_limit_usd: '20', timeout_seconds: 60, token_field: 'max_completion_tokens'},
    {name: 'local', kind: 'openai', base_url: 'http://ollama:11434/v1', has_api_key: false, monthly_limit_usd: '0', timeout_seconds: 120, token_field: 'max_tokens'},
  ],
  defaults: {
    text: {provider: 'local', model: 'local-chat'},
    embedding: {provider: 'openai', model: 'text-embedding-3-small'},
  },
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
    {name: 'text-embedding-3-small', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0.02', dimensions: 1536, truncatable: true, provider: 'openai'},
    {name: 'local-chat', input_per_1m: '0', output_per_1m: '0', embedding_per_1m: '0', dimensions: 0, truncatable: false, provider: 'local'},
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
const lastPut = () => JSON.parse(String(putCalls().at(-1)?.[1]?.body)) as Body

function renderSection() {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  render(<QueryClientProvider client={client}><LLMSettingsSection /></QueryClientProvider>)
}

function mount(server: LLMSettings = fixture) {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm/models') return {models: []}
    if (path === '/v1/settings/llm' && !options?.method) return server
    return {...server, revision: server.revision + 1}
  })
  renderSection()
}

afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.resetAllMocks()})

it('rejects renaming a provider onto a sibling current name without moving routes', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'local'}})
  expect(await screen.findByText(/already used by another provider/)).toBeTruthy()
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('openai')
  expect(screen.getByLabelText('Name').getAttribute('aria-invalid')).toBe('true')
  expect(screen.queryByText('Save blocked. Fix the following:')).toBeNull()
  // The rejected rename leaves the draft clean; a neutral edit makes it
  // saveable so the intact rows can be inspected.
  fireEvent.change(screen.getByLabelText('Monthly limit (USD)'), {target: {value: '21'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toEqual(['openai', 'local'])
  expect(body.providers.find(provider => provider.name === 'openai')).toMatchObject({monthly_limit_usd: '21'})
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'local', model: 'local-chat'})
  expect(body.defaults).toEqual({text: {provider: 'local', model: 'local-chat'}, embedding: {provider: 'openai', model: 'text-embedding-3-small'}})
  expect(body.models.find(model => model.name === 'text-embedding-3-small')).toMatchObject({provider: 'openai'})
  expect(body.models.find(model => model.name === 'local-chat')).toMatchObject({provider: 'local'})
})

it('rejects renaming a provider onto a sibling retained routed name without moving routes', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: ''}})
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'local'}})
  expect(await screen.findByText(/already used by another provider/)).toBeTruthy()
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('openai')
  // Restore the blank name so the draft is valid, then save. If the rejected
  // rename had moved the first provider's rows, the save would be blocked by
  // the duplicate name or would show the moved rows.
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'local'}})
  // Restoring the name alone returns the draft to the loaded state; a neutral
  // edit makes it saveable so the intact rows can be inspected.
  fireEvent.change(screen.getByLabelText('Monthly limit (USD)'), {target: {value: '1'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toEqual(['openai', 'local'])
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'local', model: 'local-chat'})
  expect(body.defaults).toEqual({text: {provider: 'local', model: 'local-chat'}, embedding: {provider: 'openai', model: 'text-embedding-3-small'}})
  expect(body.models.find(model => model.name === 'text-embedding-3-small')).toMatchObject({provider: 'openai'})
  expect(body.models.find(model => model.name === 'local-chat')).toMatchObject({provider: 'local'})
})

it('keeps the retained routed name through a rejected collision and renames from it', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: ''}})
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
  // A colliding name is rejected, so it must not become the routed name.
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'openai'}})
  expect(await screen.findByText(/already used by another provider/)).toBeTruthy()
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'renamed-local'}})
  expect(screen.queryByText(/already used by another provider/)).toBeNull()
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toEqual(['openai', 'renamed-local'])
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'renamed-local', model: 'local-chat'})
  expect(body.defaults).toEqual({text: {provider: 'renamed-local', model: 'local-chat'}, embedding: {provider: 'openai', model: 'text-embedding-3-small'}})
  expect(body.models.find(model => model.name === 'local-chat')).toMatchObject({provider: 'renamed-local'})
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
})

it('reserves a retained name when adding a preset so it cannot capture the first provider', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: ''}})
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
  fireEvent.click(screen.getByRole('button', {name: 'Add OpenAI'}))
  expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('openai-2')
  // Restore the first provider's name; the preset keeps its own identity and
  // the first provider's rows stay with it.
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'openai'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toEqual(['openai', 'local', 'openai-2'])
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'local', model: 'local-chat'})
  expect(body.defaults).toEqual({text: {provider: 'local', model: 'local-chat'}, embedding: {provider: 'openai', model: 'text-embedding-3-small'}})
  expect(body.models.find(model => model.name === 'text-embedding-3-small')).toMatchObject({provider: 'openai'})
  expect(body.models.find(model => model.name === 'local-chat')).toMatchObject({provider: 'local'})
})

it('moves only the renamed provider routes defaults and scoped models', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'renamed-local'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await screen.findByText(/AI settings saved/)
  const body = lastPut()
  expect(body.providers.map(provider => provider.name)).toEqual(['openai', 'renamed-local'])
  expect(body.tasks.find(task => task.task === 'blurb')).toEqual({task: 'blurb', mode: 'custom', provider: 'renamed-local', model: 'local-chat'})
  expect(body.tasks.find(task => task.task === 'embed')).toEqual({task: 'embed', mode: 'custom', provider: 'openai', model: 'text-embedding-3-small'})
  expect(body.defaults).toEqual({text: {provider: 'renamed-local', model: 'local-chat'}, embedding: {provider: 'openai', model: 'text-embedding-3-small'}})
  expect(body.models.find(model => model.name === 'local-chat')).toMatchObject({provider: 'renamed-local'})
  expect(body.models.find(model => model.name === 'text-embedding-3-small')).toMatchObject({provider: 'openai'})
})

it('clears only the name collision error after a valid rename', async () => {
  mount()
  await screen.findByText(/https:\/\/api\.openai\.com\/v1/)
  // An unrelated validation error that a rename must not clear.
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[1])
  fireEvent.change(screen.getByLabelText('Monthly limit (USD)'), {target: {value: 'lots'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect(await screen.findByText(/monthly limit must be a non-negative amount/)).toBeTruthy()
  fireEvent.click(screen.getAllByRole('button', {name: 'Edit'})[0])
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'local'}})
  expect(await screen.findByText(/already used by another provider/)).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Name'), {target: {value: 'openai-2'}})
  expect(screen.queryByText(/already used by another provider/)).toBeNull()
  expect(screen.getByText(/monthly limit must be a non-negative amount/)).toBeTruthy()
})
