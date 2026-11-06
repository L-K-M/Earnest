// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {request} from '../request'
import {Ask, Edition, Summary} from './Pipeline'
vi.mock('../request', () => ({request:vi.fn(), RequestError:class extends Error {status = 0}}))
afterEach(() => {cleanup();vi.resetAllMocks()})
const hostile = '<img src=x> [link](javascript:alert(1)) https://evil.example <a href="https://evil.example">link</a>'
const status = {summary:'enabled',edition:'enabled',ask:'enabled',blurb:'enabled'}
function mount(ui: React.ReactNode) {return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{ui}</QueryClientProvider>)}
it('hides disabled summary and edition without requesting output', async () => {
  vi.mocked(request).mockResolvedValue({summary:'disabled',edition:'disabled'})
  mount(<><Summary entryID={1}/><Edition onOpen={vi.fn()}/></>)
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/pipeline',expect.anything()))
  expect(screen.queryByText('Summary')).toBeNull()
  expect(screen.queryByText("Make this week's edition")).toBeNull()
  expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('/summary') || path.includes('/editions'))).toBe(false)
})
it('requests summary on demand, renders hostile output literally, and exposes unavailable highlighting', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? status : {model:'model',generated_at:'2026-01-01T00:00:00Z',excerpt_notice:'The feed gave an excerpt only. This summary covers the excerpt.',claims:[{text:hostile,passages:['Source passage'],verified:false}]})
  const {container} = mount(<Summary entryID={1}/>)
  fireEvent.click(await screen.findByRole('button',{name:'Summary'}))
  expect(await screen.findByText(hostile)).toBeTruthy()
  expect(screen.getByText('Citation not found in the article')).toBeTruthy()
  expect(screen.getByText(/The feed gave an excerpt only/)).toBeTruthy()
  expect(container.querySelector('a, img')).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Source passage'}))
  expect(screen.getByText(/Highlighting is unavailable/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Summary'}))
  expect(screen.queryByText(hostile)).toBeNull()
})
it('asks on submission and opens verified passages while flagging failures', async () => {
  const open = vi.fn()
  vi.mocked(request).mockImplementation(async path => path === '/v1/ask' ? {remaining_today:19,daily_limit:20,claims:[{text:hostile,verified:false,citations:[{entry_id:4,verified:true,passages:['Quoted text']}]}]} : {id:4})
  const {container} = mount(<Ask onOpen={open}/>)
  fireEvent.change(screen.getByLabelText('Question'),{target:{value:'What happened?'}})
  fireEvent.click(screen.getByRole('button',{name:'Ask'}))
  expect(await screen.findByText(hostile)).toBeTruthy()
  expect(screen.getByText('19 of 20 questions remaining today.')).toBeTruthy()
  expect(screen.getByText('Citation not found in the article')).toBeTruthy()
  expect(container.querySelector('a, img')).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Quoted text'}))
  await waitFor(() => expect(open).toHaveBeenCalledWith({id:4}))
})
it('uses answer wording for the excerpt notice', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/ask' ? {remaining_today:19,daily_limit:20,excerpt_notice:'The feed gave an excerpt only. This summary covers the excerpt.',claims:[]} : {})
  mount(<Ask onOpen={vi.fn()}/>)
  fireEvent.change(screen.getByLabelText('Question'),{target:{value:'What happened?'}})
  fireEvent.click(screen.getByRole('button',{name:'Ask'}))
  expect(await screen.findByText('Some sources gave excerpts only.')).toBeTruthy()
  expect(screen.queryByText(/The feed gave an excerpt only/)).toBeNull()
})
it('shows edition prose and triggers a weekly edition', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? status : path === '/v1/editions/latest' ? {sections:[{name:'News',claims:[{text:'A story.',verified:true,citations:[{entry_id:4,verified:true,passages:['Evidence']}]}]}]} : {})
  mount(<Edition onOpen={vi.fn()}/>)
  expect(await screen.findByText('A story.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:"Make this week's edition"}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/editions',expect.objectContaining({method:'POST',body:expect.stringContaining('weekly')})))
})

it('never hijacks Tab while a summary is visible', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? status : {model:'m',generated_at:'2026-01-01T00:00:00Z',claims:[]})
  mount(<><h2 tabIndex={-1}>Article</h2><input aria-label="Other input"/><Summary entryID={1}/></>)
  await screen.findByRole('button',{name:'Summary'})
  const heading = screen.getByText('Article')
  fireEvent.click(screen.getByRole('button',{name:'Summary'}))
  expect(await screen.findByText(/Summary · m/)).toBeTruthy()
  // Keyboard focus must keep moving normally with a summary open.
  expect(fireEvent.keyDown(heading,{key:'Tab'})).toBe(true)
  expect(fireEvent.keyDown(heading,{key:'Tab',shiftKey:true})).toBe(true)
  expect(fireEvent.keyDown(screen.getByLabelText('Other input'),{key:'Tab'})).toBe(true)
  expect(screen.getByText(/Summary · m/)).toBeTruthy()
})

it('explains paused tasks and preserves the latest edition without triggering generation',async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {summary:'paused',edition:'paused'} : {sections:[],status:'ready'})
  mount(<><Summary entryID={1}/><Edition onOpen={vi.fn()}/></>)
  await screen.findByText('Summaries are paused because the provider budget is unavailable.')
  await screen.findByText('Edition generation is paused because the provider budget is unavailable.')
  expect(screen.getByRole('button',{name:"Make this week's edition"})).toHaveProperty('disabled',true)
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/editions/latest',expect.anything()))
  expect(vi.mocked(request).mock.calls.some(([path,options]) => path.includes('/summary') || options?.method === 'POST')).toBe(false)
})
