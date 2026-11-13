// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {request} from '../request'
import {DiscoveryPanel} from './Discovery'
import type {DiscoveredModel, ProviderDraft} from './model'

vi.mock('../request', () => ({request: vi.fn(), RequestError: class extends Error {status = 503}}))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const provider: ProviderDraft = {name: 'local', kind: 'openai', base_url: 'https://example.test/v1', api_key: 'Aa', clear_api_key: false, has_api_key: false, original_name: null, timeout_seconds: '60', monthly_limit_usd: '0', token_field: 'max_tokens'}
const models: DiscoveredModel[] = [
  {id: 'first', name: 'First', chat: true, embedding: false, input_per_1m: '1', output_per_1m: '2'},
  {id: 'second', name: 'Second', chat: true, embedding: false, input_per_1m: '3', output_per_1m: '4'},
]
function panel(p = provider, revision = 3) {
  return <DiscoveryPanel provider={p} revision={revision} capability="chat" taskLabel="ask" configured={new Set()} room={128} onPick={vi.fn()} onImport={vi.fn()} onClose={vi.fn()} />
}

it('rejects an in-flight catalogue after a key edit, including equal-length hash collisions', async () => {
  let finish!: (value: unknown) => void
  vi.mocked(request).mockImplementation(() => new Promise(resolve => {finish = resolve}))
  const view = render(panel())
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1))
  // Aa and BB collide under the common 31-based string fold.
  view.rerender(panel({...provider, api_key: 'BB'}))
  finish({models})
  await waitFor(() => expect(screen.queryByText('Fetching models…')).toBeNull())
  expect(screen.queryByText('First (first)')).toBeNull()
})

it('discards catalogue results when the settings revision changes', async () => {
  vi.mocked(request).mockResolvedValue({models})
  const view = render(panel())
  await screen.findByText('First (first)')
  view.rerender(panel(provider, 4))
  await waitFor(() => expect(screen.queryByText('First (first)')).toBeNull())
})

it('starts a fresh price review when another model is selected', async () => {
  vi.mocked(request).mockResolvedValue({models})
  render(panel())
  await screen.findByText('First (first)')
  fireEvent.click(screen.getAllByRole('button', {name: 'Use for ask'})[0])
  fireEvent.change(screen.getByLabelText('first input price'), {target: {value: '99'}})
  fireEvent.click(screen.getAllByRole('button', {name: 'Use for ask'})[1])
  expect((screen.getByLabelText('second configured name') as HTMLInputElement).value).toBe('second')
  expect((screen.getByLabelText('second input price') as HTMLInputElement).value).toBe('3')
})

it('imports a selected model without routing unless Use for task was chosen', async () => {
  vi.mocked(request).mockResolvedValue({models})
  const onPick = vi.fn(), onImport = vi.fn()
  render(<DiscoveryPanel provider={provider} revision={3} capability="chat" taskLabel="ask" configured={new Set()} room={128} onPick={onPick} onImport={onImport} onClose={vi.fn()} />)
  await screen.findByText('First (first)')
  fireEvent.click(screen.getByLabelText('Select first'))
  fireEvent.click(screen.getByRole('button', {name: /Import selected/}))
  fireEvent.click(screen.getByRole('button', {name: 'Import model to draft'}))
  expect(onImport).toHaveBeenCalledTimes(1)
  expect(onPick).not.toHaveBeenCalled()
})
