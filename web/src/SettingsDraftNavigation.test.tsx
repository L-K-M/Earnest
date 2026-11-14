// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {LabelSettings} from './labels/LabelSettings'
import {OPMLTools, KeptImportTools} from './FeedSettings'
import {navigate, resetLocation} from './router'
import {request} from './request'

vi.mock('./request', () => ({request: vi.fn()}))
afterEach(() => {cleanup(); vi.restoreAllMocks(); vi.resetAllMocks()})
function prepare() {
  resetLocation('/settings#labels')
  return vi.spyOn(window, 'confirm').mockReturnValue(false)
}
function labels() {
  vi.mocked(request).mockResolvedValue([{id:1,name:'Reading',rev:1,deleted:false}])
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><LabelSettings /></QueryClientProvider>)
}
it('protects a new label until it is successfully created', async () => {
  const confirm = prepare()
  labels()
  await screen.findByText('Reading')
  fireEvent.change(screen.getByLabelText('New label'), {target:{value:'Research'}})
  expect(navigate('/settings#reading')).toBe(false)
  expect(confirm).toHaveBeenCalled()
  expect((screen.getByLabelText('New label') as HTMLInputElement).value).toBe('Research')
  fireEvent.click(screen.getByRole('button',{name:'Create label'}))
  await waitFor(() => expect((screen.getByLabelText('New label') as HTMLInputElement).value).toBe(''))
  expect(navigate('/settings#reading')).toBe(true)
})
it('protects a failed label rename until it is cancelled', async () => {
  prepare()
  labels()
  fireEvent.click(await screen.findByRole('button',{name:'Rename Reading'}))
  expect(navigate('/settings#reading')).toBe(true)
  resetLocation('/settings#labels')
  fireEvent.change(screen.getByLabelText('Rename Reading'), {target:{value:'Long reads'}})
  vi.mocked(request).mockRejectedValue(new Error('Offline'))
  fireEvent.click(screen.getByRole('button',{name:'Save'}))
  await screen.findByText('Offline')
  expect(navigate('/settings#reading')).toBe(false)
  expect((screen.getByLabelText('Rename Reading') as HTMLInputElement).value).toBe('Long reads')
  fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
  expect(navigate('/settings#reading')).toBe(true)
})
it.each(['opml','kept'])('protects a selected %s import file before and after failure', async kind => {
  prepare()
  vi.mocked(request).mockRejectedValue(new Error('Offline'))
  const act = vi.fn().mockResolvedValue(true)
  render(kind==='opml' ? <OPMLTools busy={false} act={act} /> : <KeptImportTools busy={false} act={act} />)
  const file = new File(['{}'],'saved.json')
  Object.defineProperty(file, 'text', {value:async () => '{}'})
  fireEvent.change(screen.getByLabelText(kind==='opml' ? 'Import a subscription file' : 'Import starred articles'), {target:{files:[file]}})
  expect(navigate('/settings#reading')).toBe(false)
  fireEvent.click(screen.getByRole('button',{name:kind==='opml' ? 'Import' : 'Import starred'}))
  await screen.findByRole('alert')
  expect(navigate('/settings#reading')).toBe(false)
})
it.each(['opml','kept'])('releases the %s import guard after completion and protects a newly selected file', async kind => {
  prepare()
  vi.mocked(request).mockResolvedValue(kind === 'opml' ? [] : {results: [], subscribed: []})
  const act = vi.fn().mockResolvedValue(true)
  render(kind === 'opml' ? <OPMLTools busy={false} act={act} /> : <KeptImportTools busy={false} act={act} />)
  const contents = kind === 'opml' ? '<opml><body /></opml>' : '{"items":[]}'
  const file = new File([contents], 'saved.json')
  Object.defineProperty(file, 'text', {value: async () => contents})
  const input = screen.getByLabelText(kind === 'opml' ? 'Import a subscription file' : 'Import starred articles')
  fireEvent.change(input, {target: {files: [file]}})
  fireEvent.click(screen.getByRole('button', {name: kind === 'opml' ? 'Import' : 'Import starred'}))
  await waitFor(() => expect(act).toHaveBeenCalled())
  expect(navigate('/settings#reading')).toBe(true)
  resetLocation('/settings#labels')
  fireEvent.change(input, {target: {files: [new File([contents], 'other.json')]}})
  expect(navigate('/settings#reading')).toBe(false)
})
