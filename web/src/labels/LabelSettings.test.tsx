// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import type {components} from '../api.generated'
import {request} from '../request'
import {LabelSettings} from './LabelSettings'

vi.mock('../request', () => ({request: vi.fn()}))

type Label = components['schemas']['Label']
const fixture: Label[] = [
  {id: 1, name: 'Reading', rev: 2, deleted: false},
  {id: 2, name: 'Archived', rev: 1, deleted: true},
]

function mount(labels: Label[] = fixture) {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}})
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (path === '/v1/labels' && !options?.method) return labels
    return {id: 3, name: 'Created', rev: 1, deleted: false} as Label
  })
  render(<QueryClientProvider client={client}><LabelSettings /></QueryClientProvider>)
  return client
}

const calls = (path: string, method?: string) => vi.mocked(request).mock.calls.filter(([candidate, options]) => candidate === path && (method === undefined || options?.method === method))
const bodyOf = (call: [string, RequestInit?]) => JSON.parse(String(call[1]?.body)) as Record<string, unknown>
const deletes = () => vi.mocked(request).mock.calls.filter(([path, options]) => path.startsWith('/v1/labels/1?') && options?.method === 'DELETE')

afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.resetAllMocks()})

it('lists only non-deleted labels and issues no mutations on load', async () => {
  mount()
  expect(await screen.findByText('Reading')).toBeTruthy()
  expect(screen.queryByText('Archived')).toBeNull()
  expect(calls('/v1/labels')).toHaveLength(1)
  expect(vi.mocked(request).mock.calls.filter(([, options]) => options?.method)).toHaveLength(0)
})

it('creates a label with the trimmed name and a unique nonempty op id, then invalidates labels', async () => {
  const client = mount()
  await screen.findByText('Reading')
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  fireEvent.change(screen.getByLabelText('New label'), {target: {value: '  Security  '}})
  fireEvent.click(screen.getByRole('button', {name: 'Create label'}))
  await waitFor(() => expect(calls('/v1/labels', 'POST')).toHaveLength(1))
  const first = bodyOf(calls('/v1/labels', 'POST')[0])
  expect(first).toEqual({name: 'Security', op_id: expect.any(String)})
  expect(String(first.op_id)).not.toBe('')
  await waitFor(() => expect(invalidate).toHaveBeenCalledWith({queryKey: ['labels']}))
  expect((await screen.findByRole('status')).textContent).toBe('Label created.')
  expect((screen.getByLabelText('New label') as HTMLInputElement).value).toBe('')
  fireEvent.change(screen.getByLabelText('New label'), {target: {value: 'Ops'}})
  fireEvent.click(screen.getByRole('button', {name: 'Create label'}))
  await waitFor(() => expect(calls('/v1/labels', 'POST')).toHaveLength(2))
  const second = bodyOf(calls('/v1/labels', 'POST')[1])
  expect(second.name).toBe('Ops')
  expect(second.op_id).not.toBe(first.op_id)
})

it('renames a label inline with the exact body and a nonempty op id', async () => {
  const client = mount()
  await screen.findByText('Reading')
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  fireEvent.click(screen.getByRole('button', {name: 'Rename Reading'}))
  fireEvent.change(screen.getByLabelText('Rename Reading'), {target: {value: 'Long reads'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  await waitFor(() => expect(calls('/v1/labels/1', 'PATCH')).toHaveLength(1))
  const body = bodyOf(calls('/v1/labels/1', 'PATCH')[0])
  expect(body).toEqual({name: 'Long reads', op_id: expect.any(String)})
  expect(String(body.op_id)).not.toBe('')
  await waitFor(() => expect(invalidate).toHaveBeenCalledWith({queryKey: ['labels']}))
  expect((await screen.findByRole('status')).textContent).toBe('Label renamed.')
})

it('deletes only after an explicit confirmation explaining that memberships go but articles stay', async () => {
  mount()
  await screen.findByText('Reading')
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  fireEvent.click(screen.getByRole('button', {name: 'Delete Reading'}))
  expect(confirm).toHaveBeenCalledTimes(1)
  const prompt = String(confirm.mock.calls[0][0])
  expect(prompt).toMatch(/memberships? (are|is) removed/i)
  expect(prompt).toMatch(/articles are kept/i)
  expect(deletes()).toHaveLength(0)
  confirm.mockReturnValue(true)
  fireEvent.click(screen.getByRole('button', {name: 'Delete Reading'}))
  await waitFor(() => expect(deletes()).toHaveLength(1))
  const opID = new URL(deletes()[0][0], 'http://localhost').searchParams.get('op_id')
  expect(opID).toBeTruthy()
  expect((await screen.findByRole('status')).textContent).toBe('Label deleted.')
})

it('shows the server error and no false success when a mutation fails', async () => {
  const client = mount()
  await screen.findByText('Reading')
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  vi.mocked(request).mockImplementation(async (path, options) => {
    if (options?.method === 'POST') throw new Error('label name is already taken')
    return fixture
  })
  fireEvent.change(screen.getByLabelText('New label'), {target: {value: 'Duplicate'}})
  fireEvent.click(screen.getByRole('button', {name: 'Create label'}))
  expect((await screen.findByRole('alert')).textContent).toBe('label name is already taken')
  expect((screen.getByLabelText('New label') as HTMLInputElement).getAttribute('aria-invalid')).toBeNull()
  expect(screen.queryByRole('status')).toBeNull()
  expect(invalidate).not.toHaveBeenCalled()
  expect(calls('/v1/labels', 'POST')).toHaveLength(1)
})

it('rejects blank names without calling the API', async () => {
  mount()
  await screen.findByText('Reading')
  fireEvent.change(screen.getByLabelText('New label'), {target: {value: '   '}})
  fireEvent.click(screen.getByRole('button', {name: 'Create label'}))
  expect((await screen.findByRole('alert')).textContent).toBe('Give the label a name.')
  expect((screen.getByLabelText('New label') as HTMLInputElement).getAttribute('aria-invalid')).toBe('true')
  expect((screen.getByLabelText('New label') as HTMLInputElement).getAttribute('aria-describedby')).toBe('label-error')
  expect(vi.mocked(request).mock.calls.filter(([, options]) => options?.method)).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', {name: 'Rename Reading'}))
  fireEvent.change(screen.getByLabelText('Rename Reading'), {target: {value: '   '}})
  fireEvent.click(screen.getByRole('button', {name: 'Save'}))
  expect((await screen.findByRole('alert')).textContent).toBe('Give the label a name.')
  expect((screen.getByLabelText('Rename Reading') as HTMLInputElement).getAttribute('aria-invalid')).toBe('true')
  expect((screen.getByLabelText('Rename Reading') as HTMLInputElement).getAttribute('aria-describedby')).toBe('label-error')
  expect(vi.mocked(request).mock.calls.filter(([, options]) => options?.method)).toHaveLength(0)
})
