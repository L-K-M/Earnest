// @vitest-environment jsdom
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {SettingsPage} from './SettingsPage'
import {defaultSettings} from './Settings'
import {resetLocation} from './router'
import {LLMSettings as AISettings} from './llm/LLMSettings'
import {ModelPicker} from './llm/Discovery'
import {draftFromSettings, validateDraft, type LLMSettings, type ProviderDraft} from './llm/model'

const {request} = vi.hoisted(() => ({request: vi.fn()}))
vi.mock('./request', async original => ({...await original<typeof import('./request')>(), request}))
vi.mock('./Settings', async original => ({...await original<typeof import('./Settings')>(), Settings: () => <h2>Reading preferences panel</h2>}))
vi.mock('./labels/LabelSettings', () => ({LabelSettings: () => <h2>Organization panel</h2>}))
vi.mock('./account/Account', () => ({Push: () => <h2>Notifications panel</h2>, Tokens: () => <h2>Access panel</h2>, Webhooks: () => <h2>Webhooks panel</h2>}))
vi.mock('./integrations/Integrations', () => ({Integrations: () => <h2>Connections panel</h2>}))

const settings: LLMSettings = {revision: 1, providers: [], models: [], tasks: []}
const provider: ProviderDraft = {name:'venice',kind:'openai',base_url:'https://api.venice.ai/api/v1',has_api_key:true,monthly_limit_usd:'20',timeout_seconds:'60',token_field:'max_completion_tokens',api_key:'',clear_api_key:false,original_name:'venice'}
beforeEach(() => {request.mockReset(); resetLocation('/settings#llm')})
afterEach(() => {cleanup(); vi.restoreAllMocks()})

it('opens only the selected settings section, preserving the old AI deep link', async () => {
  request.mockResolvedValue(settings)
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} /></QueryClientProvider>)
  await screen.findByRole('heading', {name:'AI'})
  expect(screen.queryByRole('heading',{name:'Reading preferences panel'})).toBeNull()
  expect(screen.queryByRole('heading',{name:'Organization panel'})).toBeNull()
  expect(screen.getByRole('link',{name:'AI'}).getAttribute('aria-current')).toBe('page')
})

it('keeps manual model entry usable when catalogue discovery fails', async () => {
  request.mockRejectedValue(new Error('Discovery is unavailable.'))
  render(<ModelPicker provider={provider} revision={1} capability="embedding" taskLabel={null} models={[]} room={128} cache={new Map()} onPick={vi.fn()} onClose={vi.fn()} />)
  await screen.findByRole('alert')
  fireEvent.click(screen.getByRole('button',{name:'Enter model ID manually'}))
  expect(screen.getByRole('textbox',{name:'Manual model ID'})).toBeTruthy()
})

it('requires a default model before an enabled feature can use it', () => {
  const draft=draftFromSettings(settings)
  draft.tasks[0].mode='default'
  expect(validateDraft(draft,settings).some(error=>/model/i.test(error))).toBe(true)
})


function aiFixture(): LLMSettings {
  const saved = {name: provider.name, kind: provider.kind, base_url: provider.base_url, has_api_key: provider.has_api_key, monthly_limit_usd: provider.monthly_limit_usd, timeout_seconds: 60, token_field: provider.token_field}
  return {...settings, providers: [saved],
    defaults: {text: {provider: '', model: ''}, embedding: {provider: 'venice', model: 'qwen3'}},
    tasks: [{task: 'embed', mode: 'default', provider: 'venice', model: 'qwen3'}],
    models: [{provider:'venice',name:'qwen3',input_per_1m:'0',output_per_1m:'0',embedding_per_1m:'0.012500',dimensions:4096,truncatable:false,supports_custom_dimensions:true}]}
}
function mountAI(config = aiFixture()) {
  request.mockImplementation(async (path: string, options?: RequestInit) => {
    if (path.endsWith('/models')) return {models: [{id:'qwen3',name:'Qwen3',embedding:true,chat:false,dimensions:4096,supports_custom_dimensions:true,embedding_per_1m:'0.012500'}]}
    if (options?.method === 'PUT') return {...config,...JSON.parse(String(options.body)),revision:2}
    return config
  })
  render(<QueryClientProvider client={new QueryClient()}><AISettings /></QueryClientProvider>)
}
function savedBody() {
  const call = request.mock.calls.find(([,options]) => options?.method === 'PUT')
  expect(call).toBeTruthy()
  return JSON.parse(String(call![1].body))
}
it('removes scoped metadata and inherited routes when removing their provider', async () => {
  vi.spyOn(window,'confirm').mockReturnValue(true)
  mountAI()
  fireEvent.click(await screen.findByRole('button',{name:'Remove',}))
  fireEvent.click(screen.getByRole('button',{name:'Save',}))
  await waitFor(() => expect(request.mock.calls.some(([,options]) => options?.method==='PUT')).toBe(true))
  const body=savedBody()
  expect(body.providers).toEqual([])
  expect(body.models).toEqual([])
  expect(body.defaults.embedding).toEqual({provider:'',model:''})
  expect(body.tasks.find((task:{task:string}) => task.task==='embed').mode).toBe('off')
})
it.each(['venice', ''])('clears defaults and disables inherited routes when removing a model scoped to %s', async scope => {
  vi.spyOn(window,'confirm').mockReturnValue(true)
  const config=aiFixture()
  config.models=config.models!.map(model=>({...model,provider:scope}))
  mountAI(config)
  await screen.findByText('qwen3',{selector:'strong'})
  fireEvent.click(screen.getByText('Model pricing',{exact:true}))
  fireEvent.click(screen.getByRole('button',{name:'Remove model',}))
  fireEvent.click(screen.getByRole('button',{name:'Save',}))
  await waitFor(() => expect(request.mock.calls.some(([,options]) => options?.method==='PUT')).toBe(true))
  const body=savedBody()
  expect(body.defaults.embedding).toEqual({provider:'',model:''})
  expect(body.tasks.find((task:{task:string}) => task.task==='embed').mode).toBe('off')
})
it('reuses the same provider catalogue after closing and reopening the picker', async () => {
  mountAI()
  const select=await screen.findByLabelText('Default embedding model provider')
  const browse=within(select.parentElement!).getByRole('button',{name:'Browse models'})
  fireEvent.click(browse)
  await screen.findByRole('button',{name:'Select qwen3'})
  fireEvent.click(screen.getByRole('button',{name:'Close',}))
  fireEvent.click(browse)
  await screen.findByRole('button',{name:'Select qwen3'})
  expect(request.mock.calls.filter(([path])=>path==='/v1/settings/llm/models')).toHaveLength(1)
})

it('puts providers first and offers feature switches with advanced overrides', async () => {
  mountAI()
  await screen.findByRole('heading',{name:'Providers'})
  const providersHeading=screen.getByRole('heading',{name:'Providers'})
  const defaultsHeading=screen.getByRole('heading',{name:'Defaults'})
  expect(providersHeading.compareDocumentPosition(defaultsHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(screen.getByRole('checkbox',{name:'Embeddings',})).toBeTruthy()
  expect(screen.queryByRole('combobox',{name:'Embeddings mode'})).toBeNull()
})
it('uses consistent section names and retains the old webhooks link', async () => {
  resetLocation('/settings#webhooks')
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(await screen.findByRole('heading',{name:'Webhooks panel'})).toBeTruthy()
  expect(screen.getByRole('link',{name:'Connections'}).getAttribute('aria-current')).toBe('page')
  for (const name of ['Reading & appearance','Notifications','Organization','Account & devices']) expect(screen.getByRole('link',{name})).toBeTruthy()
})
it('falls back to reading settings for an unknown section link', () => {
  resetLocation('/settings#unknown')
  render(<SettingsPage device={false} settings={defaultSettings} onSaved={vi.fn()} />)
  expect(screen.getByRole('heading',{name:'Reading preferences panel'})).toBeTruthy()
})

it('excludes a known incompatible embedding width from the picker', async () => {
  request.mockResolvedValue({models:[{id:'incompatible',name:'Incompatible',embedding:true,chat:false,embedding_per_1m:'1',dimensions:4096,supports_custom_dimensions:false,truncatable:false}]})
  render(<ModelPicker provider={provider} revision={1} capability="embedding" taskLabel={null} models={[]} room={128} cache={new Map()} onPick={vi.fn()} onClose={vi.fn()} />)
  await screen.findByRole('textbox',{name:'Search models'})
  expect(screen.queryByRole('button',{name:'Select incompatible'})).toBeNull()
})

it('selects a known text model from the provider picker without embedding fields', async () => {
  request.mockResolvedValue({models:[{id:'text-only',name:'Text only',chat:true,embedding:false,input_per_1m:'1',output_per_1m:'2'}]})
  const pick=vi.fn()
  render(<ModelPicker provider={provider} revision={1} capability={null} taskLabel={null} models={[]} room={128} cache={new Map()} onPick={pick} onClose={vi.fn()} />)
  fireEvent.click(await screen.findByRole('button',{name:'Select text-only'}))
  expect(pick).toHaveBeenCalledWith(expect.objectContaining({name:'text-only',dimensions:'0',embedding_per_1m:'0'}))
})

it('does not reuse a previous settings session catalogue after remounting', async () => {
  mountAI()
  let select=await screen.findByLabelText('Default embedding model provider')
  fireEvent.click(within(select.parentElement!).getByRole('button',{name:'Browse models'}))
  await screen.findByRole('button',{name:'Select qwen3'})
  const before=request.mock.calls.filter(([path])=>path==='/v1/settings/llm/models').length
  cleanup()
  mountAI()
  select=await screen.findByLabelText('Default embedding model provider')
  fireEvent.click(within(select.parentElement!).getByRole('button',{name:'Browse models'}))
  await screen.findByRole('button',{name:'Select qwen3'})
  expect(request.mock.calls.filter(([path])=>path==='/v1/settings/llm/models')).toHaveLength(before+1)
})

it('keeps scoped and global model prices separate through a temporarily empty provider name', async () => {
  const config=aiFixture()
  config.models!.push({...config.models![0],provider:'',embedding_per_1m:'9'})
  mountAI(config)
  fireEvent.click(await screen.findByRole('button',{name:'Edit'}))
  fireEvent.change(screen.getByLabelText('Name'),{target:{value:''}})
  fireEvent.change(screen.getByLabelText('Name'),{target:{value:'renamed'}})
  fireEvent.change(screen.getByLabelText('API key'),{target:{value:'replacement'}})
  fireEvent.click(screen.getByRole('button',{name:'Save'}))
  await waitFor(()=>expect(request.mock.calls.some(([,options])=>options?.method==='PUT')).toBe(true))
  const body=savedBody()
  expect(body.models.filter((model:{provider?:string})=>!model.provider)).toHaveLength(1)
  expect(body.models.find((model:{provider?:string})=>model.provider==='renamed').embedding_per_1m).toBe('0.012500')
  expect(body.defaults.embedding.provider).toBe('renamed')
})

it('keeps a scoped route when removing a shadowed legacy model', async () => {
  vi.spyOn(window,'confirm').mockReturnValue(true)
  const config = aiFixture()
  config.models!.push({...config.models![0],provider:'',embedding_per_1m:'9'})
  mountAI(config)
  await screen.findAllByText('qwen3',{selector:'strong'})
  fireEvent.click(screen.getByText('Model pricing',{exact:true}))
  fireEvent.click(screen.getAllByRole('button',{name:'Remove model'})[1])
  fireEvent.click(screen.getByRole('button',{name:'Save'}))
  await waitFor(() => expect(request.mock.calls.some(([,options]) => options?.method==='PUT')).toBe(true))
  expect(savedBody().defaults.embedding).toEqual({provider:'venice',model:'qwen3'})
  expect(savedBody().tasks.find((task:{task:string}) => task.task==='embed').mode).toBe('default')
})
