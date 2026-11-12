// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import type {ReactNode} from 'react'
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {request} from '../request'
import {useOnline} from '../offline/Offline'
import {Integrations, SendActions, type IntegrationConfig, type IntegrationSendResult, type IntegrationSettings} from './Integrations'

vi.mock('../request', () => ({request: vi.fn()}))
vi.mock('../offline/Offline', () => ({useOnline: vi.fn(() => true)}))

const empty: IntegrationConfig = {url: '', default_list_id: '', default_label: '', token_set: false}
const both: IntegrationSettings = {
  karakeep: {url: 'https://karakeep.test', default_list_id: 'list-1', default_label: '', token_set: true},
  hister: {url: 'https://hister.test', default_list_id: '', default_label: 'Read later', token_set: true},
}

function mount(node: ReactNode) {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  return {...render(<QueryClientProvider client={client}>{node}</QueryClientProvider>), client}
}

const getCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path === '/v1/integrations' && !options?.method)
const putCalls = (provider: string) => vi.mocked(request).mock.calls.filter(([path, options]) => path === `/v1/integrations/${provider}` && options?.method === 'PUT')
const sendCalls = () => vi.mocked(request).mock.calls.filter(([path, options]) => path.includes('/send/') && options?.method === 'POST')
const bodyOf = (call: [string, RequestInit?]) => JSON.parse(String(call[1]?.body)) as Record<string, unknown>

afterEach(() => {cleanup(); vi.clearAllMocks(); vi.mocked(useOnline).mockReturnValue(true)})

it('saves the Karakeep default list without ever sending a token', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/integrations/karakeep' && options?.method === 'PUT') return {...both.karakeep, default_list_id: 'list-2'}
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  mount(<Integrations />)
  const form = await screen.findByRole('form', {name: 'Karakeep integration'})
  const token = within(form).getByLabelText('API token') as HTMLInputElement
  // A saved token is never rendered back into the field.
  expect(token.type).toBe('password')
  expect(token.value).toBe('')
  expect(token.placeholder).toMatch(/leave blank to keep/i)
  expect(screen.getByRole('form', {name: 'Hister integration'})).toBeTruthy()
  fireEvent.change(within(form).getByLabelText('Default list ID'), {target: {value: 'list-2'}})
  fireEvent.click(within(form).getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls('karakeep')).toHaveLength(1))
  const body = bodyOf(putCalls('karakeep')[0])
  expect(body).toEqual({url: 'https://karakeep.test', default_list_id: 'list-2', default_label: '', op_id: expect.any(String)})
  expect('token' in body).toBe(false)
  expect(JSON.stringify(body)).not.toMatch(/token/i)
  expect(screen.queryByText(/token-value/)).toBeNull()
  // The field stays blank after saving, so the next save keeps the token.
  await waitFor(() => expect((within(form).getByLabelText('API token') as HTMLInputElement).value).toBe(''))
  expect((within(form).getByLabelText('API token') as HTMLInputElement).placeholder).toMatch(/leave blank to keep/i)
})

it('replaces a saved token with a new one and never echoes the old value', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/integrations/karakeep' && options?.method === 'PUT') return both.karakeep
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  mount(<Integrations />)
  const form = await screen.findByRole('form', {name: 'Karakeep integration'})
  fireEvent.change(within(form).getByLabelText('API token'), {target: {value: 'brand-new-secret'}})
  fireEvent.click(within(form).getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls('karakeep')).toHaveLength(1))
  expect(bodyOf(putCalls('karakeep')[0]).token).toBe('brand-new-secret')
  await waitFor(() => expect((within(form).getByLabelText('API token') as HTMLInputElement).value).toBe(''))
})

it('clears an optional Hister token only through the explicit action', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/integrations/hister' && options?.method === 'PUT') return {...both.hister, token_set: false}
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  mount(<Integrations />)
  const form = await screen.findByRole('form', {name: 'Hister integration'})
  // Blanking the field alone means "keep"; the button is the explicit clear.
  fireEvent.change(within(form).getByLabelText('API token'), {target: {value: ''}})
  fireEvent.click(within(form).getByRole('button', {name: 'Remove saved token'}))
  fireEvent.click(within(form).getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(putCalls('hister')).toHaveLength(1))
  expect(bodyOf(putCalls('hister')[0]).token).toBe('')
  // Karakeep has no clear action: its token is required to enable the URL.
  expect(within(screen.getByRole('form', {name: 'Karakeep integration'})).queryByRole('button', {name: /saved token/i})).toBeNull()
})

it('saves the default Hister label and keeps the confirmed result when refresh fails', async () => {
  let reads = 0
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (options?.method === 'PUT') return {...both.hister, default_label: 'RSS queue'}
    if (path === '/v1/integrations' && reads++ === 0) return both
    throw new Error('Refresh unavailable')
  })
  const {client} = mount(<Integrations />)
  const form = await screen.findByRole('form', {name: 'Hister integration'})
  fireEvent.change(within(form).getByLabelText('Default label'), {target: {value: 'RSS queue'}})
  fireEvent.click(within(form).getByRole('button', {name: 'Save'}))
  await screen.findByText('Hister saved.')
  expect(bodyOf(putCalls('hister')[0])).toEqual({url: both.hister.url, default_list_id: '', default_label: 'RSS queue', op_id: expect.any(String)})
  expect(client.getQueryData<IntegrationSettings>(['integrations'])?.hister.default_label).toBe('RSS queue')
  await waitFor(() => expect(within(form).getByRole('button', {name: 'Save'}).hasAttribute('disabled')).toBe(true))
})

it('adopts refreshed settings only while the form has no edits', async () => {
  vi.mocked(request).mockResolvedValue(both)
  const {client} = mount(<Integrations />)
  const form = await screen.findByRole('form', {name: 'Hister integration'})
  const field = within(form).getByLabelText('Default label') as HTMLInputElement
  act(() => {client.setQueryData(['integrations'], {...both, hister: {...both.hister, default_label: 'Updated elsewhere'}})})
  await waitFor(() => expect(field.value).toBe('Updated elsewhere'))
  fireEvent.change(field, {target: {value: 'My unsaved label'}})
  act(() => {client.setQueryData(['integrations'], {...both, hister: {...both.hister, default_label: 'Another update'}})})
  await waitFor(() => expect(field.value).toBe('My unsaved label'))
})

it('does not notify another article or session after the sender unmounts', async () => {
  let finish: (result: IntegrationSendResult) => void = () => undefined
  vi.mocked(request).mockImplementation(async path => path === '/v1/integrations' ? both : new Promise<IntegrationSendResult>(resolve => {finish = resolve}))
  const notify = vi.fn()
  const {unmount} = mount(<SendActions entryID={7} onNotify={notify} />)
  fireEvent.click(await screen.findByRole('button', {name: 'Send to Hister'}))
  unmount()
  await act(async () => {finish({message: 'Sent to Hister.'})})
  expect(notify).not.toHaveBeenCalled()
})

it('sends the entry with an op id and reports the server message', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/entries/7/send/karakeep' && options?.method === 'POST') return {message: 'Sent to Karakeep.'}
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  const onNotify = vi.fn()
  mount(<SendActions entryID={7} onNotify={onNotify} />)
  fireEvent.click(await screen.findByRole('button', {name: 'Send to Karakeep'}))
  await waitFor(() => expect(onNotify).toHaveBeenCalledWith('Sent to Karakeep.'))
  expect(sendCalls()).toHaveLength(1)
  expect(sendCalls()[0][0]).toBe('/v1/entries/7/send/karakeep')
  expect(bodyOf(sendCalls()[0])).toEqual({op_id: expect.any(String)})
  expect(screen.queryByRole('alert')).toBeNull()
})

it('shows a failed send inline and never claims success', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/entries/7/send/hister' && options?.method === 'POST') throw new Error('integration returned HTTP 500; check its URL, API token and default destination')
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  const onNotify = vi.fn()
  mount(<SendActions entryID={7} onNotify={onNotify} />)
  fireEvent.click(await screen.findByRole('button', {name: 'Send to Hister'}))
  expect((await screen.findByRole('alert')).textContent).toMatch(/HTTP 500/)
  expect(onNotify).not.toHaveBeenCalled()
})

it('treats a partially completed send as a failure, not a success', async () => {
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/entries/7/send/karakeep' && options?.method === 'POST') throw new Error('saved to Karakeep, but adding it to the default list could not be confirmed')
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  const onNotify = vi.fn()
  mount(<SendActions entryID={7} onNotify={onNotify} />)
  fireEvent.click(await screen.findByRole('button', {name: 'Send to Karakeep'}))
  expect((await screen.findByRole('alert')).textContent).toMatch(/could not be confirmed/)
  expect(onNotify).not.toHaveBeenCalled()
})

it('disables both send buttons while a send is pending', async () => {
  let release: ((value: IntegrationSendResult) => void) | undefined
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    if (path === '/v1/entries/7/send/karakeep' && options?.method === 'POST') return new Promise<IntegrationSendResult>(resolve => {release = resolve})
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  mount(<SendActions entryID={7} onNotify={vi.fn()} />)
  const karakeep = await screen.findByRole('button', {name: 'Send to Karakeep'})
  const hister = screen.getByRole('button', {name: 'Send to Hister'})
  fireEvent.click(karakeep)
  await waitFor(() => expect(karakeep.hasAttribute('disabled')).toBe(true))
  expect(hister.hasAttribute('disabled')).toBe(true)
  expect(sendCalls()).toHaveLength(1)
  await act(async () => {release?.({message: 'Sent to Karakeep.'})})
  await waitFor(() => expect(karakeep.hasAttribute('disabled')).toBe(false))
  expect(hister.hasAttribute('disabled')).toBe(false)
})

it('disables sending while offline and issues no request', async () => {
  vi.mocked(useOnline).mockReturnValue(false)
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/integrations' && !options?.method) return both
    throw new Error(`unexpected ${path} ${options?.method ?? 'GET'}`)
  })
  mount(<SendActions entryID={7} onNotify={vi.fn()} />)
  const karakeep = await screen.findByRole('button', {name: 'Send to Karakeep'})
  expect(karakeep.hasAttribute('disabled')).toBe(true)
  fireEvent.click(karakeep)
  expect(sendCalls()).toHaveLength(0)
})

it('renders no send buttons when neither provider is configured', async () => {
  let release: ((value: IntegrationSettings) => void) | undefined
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/integrations') return new Promise<IntegrationSettings>(resolve => {release = resolve})
    throw new Error(`unexpected ${path}`)
  })
  mount(<SendActions entryID={7} onNotify={vi.fn()} />)
  await waitFor(() => expect(getCalls()).toHaveLength(1))
  await act(async () => {release?.({karakeep: empty, hister: empty})})
  expect(screen.queryByRole('button', {name: /send to/i})).toBeNull()
})
