// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Reader} from '../Reader'
import {defaultSettings} from '../Settings'
import type {Entry} from '../reader'
import {request} from '../request'
vi.mock('../request',() => ({request:vi.fn()}))
afterEach(() => {cleanup();vi.restoreAllMocks();vi.unstubAllGlobals()})
it('opens a text copy and keeps reading actions on the existing outbox path offline',async () => {
  vi.spyOn(navigator,'onLine','get').mockReturnValue(false)
  vi.stubGlobal('matchMedia',() => ({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn()}))
  vi.mocked(request).mockResolvedValue({text:'Saved publisher body',html:'<p>Saved publisher body</p>'})
  const act = vi.fn().mockResolvedValue(undefined)
  const entry = {id:7,feed_id:42,title:'Offline story',url:'https://publisher.test',state:{shelf:{value:'none'},kept:{value:false}}} as Entry
  const renderBody = vi.fn()
  render(<QueryClientProvider client={new QueryClient()}><Reader entry={entry} settings={defaultSettings} actions={{state:entry => entry.state,act}} onClose={vi.fn()} onSettings={vi.fn()} renderBody={renderBody}/></QueryClientProvider>)
  expect(await screen.findByText('Saved publisher body')).toBeTruthy()
  expect(renderBody).not.toHaveBeenCalled()
  expect(request).not.toHaveBeenCalledWith('/web/content-url/7',expect.anything())
  fireEvent.click(screen.getByRole('button',{name:'Later (s)'}))
  await waitFor(() => expect(act).toHaveBeenCalledWith(entry,'shelf','later'))
})

it('keeps an already loaded article mounted when connectivity drops',async () => {
  const online = vi.spyOn(navigator,'onLine','get').mockReturnValue(true)
  vi.stubGlobal('matchMedia',() => ({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn()}))
  vi.mocked(request).mockImplementation(async path => path === '/web/content-url/7' ? {url:'https://content.test/article'} : {embed:'disabled'})
  const entry = {id:7,feed_id:42,title:'Loaded story',url:'https://publisher.test',state:{shelf:{value:'none'},kept:{value:false}}} as Entry
  render(<QueryClientProvider client={new QueryClient()}><Reader entry={entry} settings={defaultSettings} actions={{state:entry => entry.state,act:vi.fn()}} onClose={vi.fn()} onSettings={vi.fn()} renderBody={url => <iframe title="Loaded article" src={url}/>}/></QueryClientProvider>)
  const frame = await screen.findByTitle('Loaded article')
  online.mockReturnValue(false)
  fireEvent(window,new Event('offline'))
  expect(screen.queryByTitle('Loaded article')).toBe(frame)
  expect(screen.queryByRole('region',{name:'Offline article'})).toBeNull()
})
