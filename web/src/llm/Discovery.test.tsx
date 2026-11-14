// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request, RequestError} from '../request'
import {ModelPicker} from './Discovery'
import type {DiscoveredModel, ModelDraft, ProviderDraft} from './model'

vi.mock('../request', async importOriginal => ({...await importOriginal<typeof import('../request')>(), request: vi.fn()}))
afterEach(() => { cleanup(); vi.resetAllMocks() })

const provider: ProviderDraft = {
  name: 'local', kind: 'openai', base_url: 'https://example.test/v1', api_key: 'Aa',
  clear_api_key: false, has_api_key: false, original_name: null,
  timeout_seconds: '60', monthly_limit_usd: '0', token_field: 'max_tokens',
}

const models: DiscoveredModel[] = [
  {id: 'first', name: 'First', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '2'},
  {id: 'second', name: 'Second', chat: true, embedding: false, input_per_1m: '3', output_per_1m: '4'},
  {id: 'emb', name: 'Emb', chat: false, embedding: true},
]

function picker(overrides: Partial<Parameters<typeof ModelPicker>[0]> = {}) {
  return <ModelPicker
    provider={provider}
    revision={3}
    capability="chat"
    taskLabel="Ask my feeds"
    models={[]}
    room={128}
    cache={new Map()}
    onPick={vi.fn()}
    onClose={vi.fn()}
    {...overrides}
  />
}

function mockCatalogue(answer: {models: DiscoveredModel[]} | (() => never)) {
  vi.mocked(request).mockImplementation(async (path: string) => {
    if (path === '/v1/settings/llm/models') {
      if (typeof answer === 'function') answer()
      return answer as {models: DiscoveredModel[]}
    }
    throw new Error(`unexpected ${path}`)
  })
}

it('fetches the catalogue automatically with the draft provider and revision', async () => {
  mockCatalogue({models})
  render(picker())
  expect(await screen.findByText('First (first)')).toBeTruthy()
  const [path, options] = vi.mocked(request).mock.calls[0]!
  expect(path).toBe('/v1/settings/llm/models')
  const body = JSON.parse(String(options?.body)) as {revision: number; provider: {name: string; api_key?: string; has_api_key?: boolean}}
  expect(body.revision).toBe(3)
  expect(body.provider.name).toBe('local')
  expect(body.provider.api_key).toBe('Aa')
  expect(body.provider.has_api_key).toBeUndefined()
})

it('rejects an in-flight catalogue after a key edit, including equal-length hash collisions', async () => {
  let finish!: (value: unknown) => void
  vi.mocked(request).mockImplementation(() => new Promise(resolve => {finish = resolve}))
  const view = render(picker())
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  // Aa and BB collide under the common 31-based string fold.
  view.rerender(picker({provider: {...provider, api_key: 'BB'}}))
  finish({models})
  await waitFor(() => expect(screen.queryByText('Fetching models…')).toBeNull())
  expect(screen.queryByText('First (first)')).toBeNull()
})

it('discards catalogue results when the settings revision changes', async () => {
  mockCatalogue({models})
  const view = render(picker())
  await screen.findByText('First (first)')
  view.rerender(picker({revision: 4}))
  await waitFor(() => expect(screen.queryByText('First (first)')).toBeNull())
})

it('selects a ready model immediately', async () => {
  mockCatalogue({models})
  const onPick = vi.fn()
  render(picker({onPick}))
  await screen.findByText('First (first)')
  fireEvent.click(screen.getByRole('button', {name: 'Select first'}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({name: 'first', provider: 'local', input_per_1m: '1', output_per_1m: '2', embedding_per_1m: '0', dimensions: '0'}))
})

it('selects an already configured model without changing its prices', async () => {
  mockCatalogue({models})
  const onPick = vi.fn()
  const configured: ModelDraft = {name: 'first', provider: 'local', input_per_1m: '9', output_per_1m: '9', embedding_per_1m: '0', dimensions: '0', truncatable: false, supports_custom_dimensions: false}
  render(picker({onPick, models: [configured]}))
  await screen.findByText('First (first)')
  fireEvent.click(screen.getByRole('button', {name: 'Select first'}))
  expect(onPick).toHaveBeenCalledWith(configured)
  expect(screen.getByText(/saved prices are kept/)).toBeTruthy()
})

it('opens the inline metadata form for missing applicable fields', async () => {
  mockCatalogue({models})
  render(picker({capability: 'embedding', taskLabel: 'Embeddings'}))
  await screen.findByText('Emb (emb)')
  fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  expect(screen.getByLabelText('emb embedding price per 1m tokens')).toBeTruthy()
  expect(screen.getByLabelText('emb dimensions')).toBeTruthy()
  expect(screen.queryByLabelText('emb input price per 1m tokens')).toBeNull()
  expect(screen.getByText('Provider supports custom dimensions')).toBeTruthy()
})

it('requires rates or an explicit free mark before checking', async () => {
  mockCatalogue({models})
  render(picker({capability: 'embedding', taskLabel: 'Embeddings'}))
  await screen.findByText('Emb (emb)')
  fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  fireEvent.click(screen.getByRole('button', {name: 'Check model'}))
  expect(screen.getByText(/Provide every applicable rate or mark this model free/)).toBeTruthy()
  expect(vi.mocked(request).mock.calls.some(([path]) => path === '/v1/settings/llm/models/check')).toBe(false)

  fireEvent.click(screen.getByLabelText('This model is free'))
  expect((screen.getByLabelText('emb embedding price per 1m tokens') as HTMLInputElement).value).toBe('0')
  fireEvent.click(screen.getByRole('button', {name: 'Check model'}))
  await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path]) => path === '/v1/settings/llm/models/check')).toBe(true))
})

it('applies observed geometry from a check to the selection only', async () => {
  vi.mocked(request).mockImplementation(async (path: string) => {
    if (path === '/v1/settings/llm/models') return {models}
    if (path === '/v1/settings/llm/models/check') return {compatible: true, dimensions: 2048, supports_custom_dimensions: true, message: 'compatible'}
    throw new Error(`unexpected ${path}`)
  })
  const onGeometry = vi.fn()
  render(picker({capability: 'embedding', taskLabel: 'Embeddings', onGeometry}))
  await screen.findByText('Emb (emb)')
  fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  fireEvent.change(screen.getByLabelText('emb embedding price per 1m tokens'), {target: {value: '0.2'}})
  fireEvent.change(screen.getByLabelText('emb dimensions'), {target: {value: '4096'}})
  fireEvent.click(screen.getByRole('button', {name: 'Check model'}))
  await screen.findByText('compatible')
  expect((screen.getByLabelText('emb dimensions') as HTMLInputElement).value).toBe('2048')
  expect((screen.getByLabelText('emb embedding price per 1m tokens') as HTMLInputElement).value).toBe('0.2')
  expect((screen.getByLabelText('emb supports custom dimensions') as HTMLInputElement).checked).toBe(true)
  expect(onGeometry).toHaveBeenCalledWith({provider: 'local', name: 'emb', dimensions: 2048, supports_custom_dimensions: true})
})

it('checks unknown embedding width with the API zero sentinel and applies the observed width', async () => {
  vi.mocked(request).mockImplementation(async (path: string, options?: RequestInit) => {
    if (path === '/v1/settings/llm/models') return {models: []}
    if (path === '/v1/settings/llm/models/check') {
      const body = JSON.parse(String(options?.body))
      expect(body.model.dimensions).toBe(0)
      expect(body.capability).toBe('embedding')
      return {compatible: true, dimensions: 4096, supports_custom_dimensions: true, message: 'Provider can return 1024 dimensions.'}
    }
    throw new Error(`unexpected ${path}`)
  })
  const onPick = vi.fn()
  render(picker({onPick, capability: 'embedding'}))
  fireEvent.click(await screen.findByRole('button', {name: 'Enter model ID manually'}))
  fireEvent.change(screen.getByLabelText('Manual model ID'), {target: {value: 'unknown-width'}})
  fireEvent.click(screen.getByRole('button', {name: 'Add model ID'}))
  fireEvent.click(screen.getByLabelText('This model is free'))
  fireEvent.click(screen.getByRole('button', {name: 'Check model'}))
  await screen.findByText('Provider can return 1024 dimensions.')
  expect((screen.getByLabelText('unknown-width dimensions') as HTMLInputElement).value).toBe('4096')
  expect((screen.getByLabelText('unknown-width truncatable') as HTMLInputElement).checked).toBe(false)
  fireEvent.click(screen.getByRole('button', {name: /Add to draft/}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({dimensions: '4096', supports_custom_dimensions: true, truncatable: false}))
})

it('starts a fresh metadata form when another model is selected', async () => {
  mockCatalogue({models: [...models, {id: 'emb2', name: 'Emb Two', embedding: true}]})
  render(picker({capability: 'embedding', taskLabel: 'Embeddings'}))
  await screen.findByText('Emb (emb)')
  fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  fireEvent.change(screen.getByLabelText('emb embedding price per 1m tokens'), {target: {value: '9'}})
  fireEvent.click(screen.getByRole('button', {name: 'Select emb2, missing embedding price, dimensions'}))
  expect((screen.getByLabelText('emb2 embedding price per 1m tokens') as HTMLInputElement).value).toBe('')
  expect(screen.queryByLabelText('emb embedding price per 1m tokens')).toBeNull()
})

it('falls back to a manual model ID', async () => {
  mockCatalogue({models})
  const onPick = vi.fn()
  render(picker({onPick}))
  await screen.findByText('First (first)')
  fireEvent.click(screen.getByRole('button', {name: 'Enter model ID manually'}))
  fireEvent.change(screen.getByLabelText('Manual model ID'), {target: {value: 'manual-model'}})
  fireEvent.click(screen.getByRole('button', {name: 'Add model ID'}))
  expect(await screen.findByLabelText('manual-model input price per 1m tokens')).toBeTruthy()
  fireEvent.click(screen.getByLabelText('This model is free'))
  fireEvent.click(screen.getByRole('button', {name: 'Add to draft and use for Ask my feeds'}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({name: 'manual-model', provider: 'local', input_per_1m: '0', output_per_1m: '0'}))
})

it.each([404, 503])('keeps manual selection usable after catalogue error %s', async status => {
  vi.mocked(request).mockRejectedValue(new RequestError(status, 'Catalogue temporarily unavailable'))
  const onPick = vi.fn()
  render(picker({onPick}))
  expect((await screen.findByRole('alert')).textContent).toContain(status === 404
    ? 'This server does not support model discovery'
    : 'Catalogue temporarily unavailable')
  fireEvent.click(screen.getByRole('button', {name: 'Enter model ID manually'}))
  fireEvent.change(screen.getByLabelText('Manual model ID'), {target: {value: 'manual-fallback'}})
  fireEvent.click(screen.getByRole('button', {name: 'Add model ID'}))
  fireEvent.click(screen.getByLabelText('This model is free'))
  fireEvent.click(screen.getByRole('button', {name: /Add to draft/}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({name: 'manual-fallback', provider: 'local', dimensions: '0'}))
})

it('reports a catalogue revision conflict to the settings draft', async () => {
  vi.mocked(request).mockRejectedValue(new RequestError(409, 'Stale revision'))
  const onConflict = vi.fn()
  render(picker({onConflict}))
  expect((await screen.findByRole('alert')).textContent).toContain('Reload, then fetch again')
  expect(onConflict).toHaveBeenCalledOnce()
})

it.each(['catalogue', 'manual'])('does not create unscoped %s metadata while a provider name is blank', async source => {
  mockCatalogue({models})
  const onPick = vi.fn()
  render(picker({onPick, provider: {...provider, name: ''}}))
  await screen.findByText('First (first)')
  if (source === 'catalogue') fireEvent.click(screen.getByRole('button', {name: 'Select first'}))
  else {
    fireEvent.click(screen.getByRole('button', {name: 'Enter model ID manually'}))
    fireEvent.change(screen.getByLabelText('Manual model ID'), {target: {value: 'manual'}})
    fireEvent.click(screen.getByRole('button', {name: 'Add model ID'}))
  }
  expect(screen.getByText(/Give this provider a name/)).toBeTruthy()
  expect(onPick).not.toHaveBeenCalled()
  expect(screen.queryByRole('button', {name: /Add to draft/})).toBeNull()
})

it('refuses to add when the configured-model table is full', async () => {
  mockCatalogue({models})
  const onPick = vi.fn()
  render(picker({onPick, room: 0}))
  await screen.findByText('First (first)')
  fireEvent.click(screen.getByRole('button', {name: 'Select first'}))
  expect(screen.getByText(/table is full/)).toBeTruthy()
  expect(onPick).not.toHaveBeenCalled()
})

it.each(['catalogue', 'manual'])('reports a full table before completing %s metadata', async source => {
  mockCatalogue({models})
  const onPick = vi.fn()
  render(picker({onPick, room: 0, capability: 'embedding'}))
  await screen.findByText('Emb (emb)')
  if (source === 'catalogue') {
    fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  } else {
    fireEvent.click(screen.getByRole('button', {name: 'Enter model ID manually'}))
    fireEvent.change(screen.getByLabelText('Manual model ID'), {target: {value: 'manual'}})
    fireEvent.click(screen.getByRole('button', {name: 'Add model ID'}))
  }
  expect(screen.getByText(/table is full/)).toBeTruthy()
  expect(screen.queryByRole('button', {name: /Add to draft/})).toBeNull()
  expect(onPick).not.toHaveBeenCalled()
})

it('rechecks available room when a metadata form is submitted', async () => {
  mockCatalogue({models})
  const onPick = vi.fn()
  const view = render(picker({onPick, capability: 'embedding'}))
  await screen.findByText('Emb (emb)')
  fireEvent.click(screen.getByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  fireEvent.click(screen.getByLabelText('This model is free'))
  fireEvent.change(screen.getByLabelText('emb dimensions'), {target: {value: '1024'}})
  view.rerender(picker({onPick, room: 0, capability: 'embedding'}))
  fireEvent.click(screen.getByRole('button', {name: /Add to draft/}))
  expect(screen.getByText(/table is full/)).toBeTruthy()
  expect(onPick).not.toHaveBeenCalled()
  expect((screen.getByLabelText('emb dimensions') as HTMLInputElement).value).toBe('1024')
})

it('restores published prices when the free choice is undone', async () => {
  mockCatalogue({models: [{id: 'paid', name: 'Paid', embedding: true, embedding_per_1m: '0.7'}]})
  const onPick = vi.fn()
  render(picker({onPick, capability: 'embedding'}))
  fireEvent.click(await screen.findByRole('button', {name: 'Select paid, missing dimensions'}))
  const rate = screen.getByLabelText('paid embedding price per 1m tokens') as HTMLInputElement
  fireEvent.click(screen.getByLabelText('This model is free'))
  expect(rate.value).toBe('0')
  expect(rate.disabled).toBe(true)
  fireEvent.click(screen.getByLabelText('This model is free'))
  expect(rate.disabled).toBe(false)
  expect(rate.value).toBe('0.7')
  fireEvent.change(screen.getByLabelText('paid dimensions'), {target: {value: '1024'}})
  fireEvent.click(screen.getByRole('button', {name: /Add to draft/}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({embedding_per_1m: '0.7'}))
})

it('focuses missing dimensions after an otherwise complete embedding selection', async () => {
  mockCatalogue({models})
  render(picker({capability: 'embedding'}))
  fireEvent.click(await screen.findByRole('button', {name: 'Select emb, missing embedding price, dimensions'}))
  fireEvent.click(screen.getByLabelText('This model is free'))
  fireEvent.click(screen.getByRole('button', {name: /Add to draft/}))
  await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('emb dimensions')))
  expect(screen.getByLabelText('emb embedding price per 1m tokens').getAttribute('aria-invalid')).toBe('false')
})

it('filters known-incompatible models for a task and labels unknown capabilities', async () => {
  mockCatalogue({models: [...models, {id: 'mystery', name: 'Mystery'}]})
  render(picker({capability: 'embedding', taskLabel: 'Embeddings'}))
  expect(await screen.findByText('Emb (emb)')).toBeTruthy()
  expect(screen.queryByText('First (first)')).toBeNull()
  expect(screen.getByText(/cannot do embedding work/)).toBeTruthy()
  expect(screen.getByText('Mystery (mystery)')).toBeTruthy()
  expect(screen.getByText('capabilities unknown')).toBeTruthy()
})

it('keeps unknown resizing support available for an explicit compatibility check', async () => {
  const onPick = vi.fn()
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/settings/llm/models') return {models: [
      {id: 'unknown-width', name: 'Unknown resize', embedding: true, dimensions: 4096, embedding_per_1m: '0.1'},
      {id: 'unsupported-width', name: 'Cannot resize', embedding: true, dimensions: 4096, supports_custom_dimensions: false},
    ]}
    if (path === '/v1/settings/llm/models/check') return {
      compatible: true, dimensions: 4096, supports_custom_dimensions: true, message: 'Supports 1024 dimensions.',
    }
    throw new Error(`unexpected ${path}`)
  })
  render(picker({capability: 'embedding', taskLabel: 'Embeddings', onPick}))
  const choose = await screen.findByRole('button', {name: 'Select unknown-width'})
  expect(screen.queryByText('Cannot resize (unsupported-width)')).toBeNull()
  expect(screen.getByText('resizing support unknown')).toBeTruthy()
  fireEvent.click(choose)
  expect(onPick).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', {name: 'Check model'}))
  await screen.findByText('Supports 1024 dimensions.')
  fireEvent.click(screen.getByRole('button', {name: 'Add to draft and use for Embeddings'}))
  expect(onPick).toHaveBeenCalledWith(expect.objectContaining({
    provider: 'local', name: 'unknown-width', dimensions: '4096',
    supports_custom_dimensions: true, truncatable: false, embedding_per_1m: '0.1',
  }))
})

it('does not apply a model check after its picker closes', async () => {
  let finish!: (value: unknown) => void
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/settings/llm/models') return {models: [{id:'emb',name:'Emb',embedding:true,chat:false,embedding_per_1m:'1'}]}
    return new Promise(resolve => { finish = resolve })
  })
  const onGeometry = vi.fn()
  const view = render(picker({capability:'embedding',onGeometry}))
  fireEvent.click(await screen.findByRole('button',{name:/Select emb/}))
  fireEvent.click(screen.getByRole('button',{name:'Check model'}))
  await waitFor(() => expect(finish).toBeTruthy())
  view.unmount()
  finish({compatible:true,dimensions:4096,supports_custom_dimensions:true})
  await new Promise(resolve => setTimeout(resolve, 0))
  expect(onGeometry).not.toHaveBeenCalled()
})
