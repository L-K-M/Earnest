// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {request} from '../request'
import {MoreLikeThis,Search} from './Search'
vi.mock('../request',() => ({request:vi.fn()}))
afterEach(() => {cleanup();vi.resetAllMocks()})
const article = {kind:'entry',entry_id:4,title:'Funding news',snippet:'Publisher text'}
const note = {kind:'agent_note',entry_id:4,annotation_id:9,title:'Funding news',agent_name:'Morning Agent',snippet:'<img src=x onerror=alert(1)> https://evil.example'}
function mount(pipeline='enabled',saved=false) {
  vi.mocked(request).mockImplementation(async (path,options) => {
    if (path === '/v1/pipeline') return {embed:pipeline,classify:pipeline,semantic:pipeline,watch:pipeline}
    if (path.startsWith('/v1/search?')) return {matches:[article,note],next_cursor:'page-2'}
    if (path.startsWith('/v1/entries?')) return {entries:[{id:4,title:'Funding news'}]}
    if (path === '/v1/saved-searches' && options?.method === 'POST') return {id:12}
    if (path === '/v1/saved-searches') return []
    return {matches:[]}
  })
  const onOpen = vi.fn()
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Search saved={saved ? {id:3,name:'Funding view',query:'funding',filters:{folder_id:7,mode:'hybrid',limit:10}} : undefined} onOpen={onOpen}/></QueryClientProvider>)
  return {onOpen}
}
async function search() {
  fireEvent.change(screen.getByLabelText('Search your feeds'),{target:{value:'funding'}})
  fireEvent.click(screen.getByRole('button',{name:'Search'}))
  return screen.findByRole('region',{name:'Agent note matches'})
}
it('reports a pipeline lookup failure while leaving full-text search usable',async () => {
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/pipeline') throw new Error('Pipeline unavailable')
    return {matches:[article]}
  })
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Search onOpen={vi.fn()}/></QueryClientProvider>)
  expect(await screen.findByRole('alert')).toHaveProperty('textContent','Could not check whether AI search features are available.')
  fireEvent.change(screen.getByLabelText('Search your feeds'),{target:{value:'funding'}})
  fireEvent.click(screen.getByRole('button',{name:'Search'}))
  expect(await screen.findByRole('button',{name:'Funding news'})).toBeTruthy()
  expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=fulltext',expect.anything())
})

it('separates inert agent notes and labels their author, opens publisher entries',async () => {
  const {onOpen} = mount('disabled')
  const notes = await search()
  expect(within(notes).getByText('Agent note · Morning Agent')).toBeTruthy()
  expect(within(notes).getByText(note.snippet)).toBeTruthy()
  expect(notes.querySelector('img, a')).toBeNull()
  const articles = screen.getByRole('region',{name:'Article matches'})
  fireEvent.click(within(articles).getByRole('button',{name:'Funding news'}))
  await waitFor(() => expect(onOpen).toHaveBeenCalledWith({id:4,title:'Funding news'}))
  expect(screen.queryByLabelText('Search mode')).toBeNull()
})
it('switches to hybrid mode, pages using opaque cursors, and saves its exact query as a sidebar view',async () => {
  mount()
  await screen.findByLabelText('Search mode')
  await search()
  fireEvent.change(screen.getByLabelText('Search mode'),{target:{value:'hybrid'}})
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=hybrid',expect.anything()))
  fireEvent.click(await screen.findByRole('button',{name:'More search results'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=hybrid&cursor=page-2',expect.anything()))
  fireEvent.change(screen.getByLabelText('Saved search name'),{target:{value:'Funding'}})
  fireEvent.click(screen.getByRole('button',{name:'Save search as a view'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/saved-searches',{method:'POST',body:JSON.stringify({name:'Funding',query:'funding',filters:{mode:'hybrid'}})}))
  expect(await screen.findByText('Search saved in the sidebar.')).toBeTruthy()
})
it('deletes a saved search from the search panel',async () => {
  vi.spyOn(window,'confirm').mockReturnValue(true)
  mount('enabled',true)
  fireEvent.click(await screen.findByRole('button',{name:'Delete “Funding view”'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/saved-searches/3',{method:'DELETE'}))
  expect(await screen.findByText('Saved search deleted. It is gone from the sidebar.')).toBeTruthy()
})
it('preserves saved filters and offers full text when hybrid is paused',async () => {
  mount('enabled',true)
  await screen.findByLabelText('Search mode')
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {embed:'enabled'} : {matches:[],status:'paused'})
  fireEvent.click(screen.getByRole('button',{name:'Search'}))
  // Change the mode twice to force a distinct query after the initial load.
  fireEvent.change(screen.getByLabelText('Search mode'),{target:{value:'fulltext'}})
  await screen.findByText(/Hybrid search is paused/)
  expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('folder_id=7') && path.includes('limit=10'))).toBe(true)
  fireEvent.click(screen.getByRole('button',{name:'Use full-text search'}))
  expect((screen.getByLabelText('Search mode') as HTMLSelectElement).value).toBe('fulltext')
})
it('shows saved subscription, folder and limit filters as selectable controls',async () => {
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/pipeline') return {embed:'disabled',classify:'disabled',semantic:'disabled',watch:'disabled'}
    return {matches:[]}
  })
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>
    <Search saved={{id:3,name:'Funding view',query:'funding',filters:{folder_id:7,subscription_id:5,limit:10,mode:'fulltext'}}} onOpen={vi.fn()} subscriptions={[{id:5,feed_id:1,title:'Publisher',url:'https://a.example',deleted:false} as never]} folders={[{id:7,name:'Work',rev:1,deleted:false}]}/>
  </QueryClientProvider>)
  expect((screen.getByLabelText('Feed') as HTMLSelectElement).value).toBe('5')
  expect((screen.getByLabelText('Folder') as HTMLSelectElement).value).toBe('7')
  expect((screen.getByLabelText('Results per page') as HTMLSelectElement).value).toBe('10')
})
it('clears a saved filter when its control is reset to all',async () => {
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/pipeline') return {embed:'disabled',classify:'disabled',semantic:'disabled',watch:'disabled'}
    if (path.startsWith('/v1/search?')) return {matches:[]}
    return {}
  })
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>
    <Search saved={{id:3,name:'Funding view',query:'funding',filters:{folder_id:7,subscription_id:5,limit:10,mode:'fulltext'}}} onOpen={vi.fn()} subscriptions={[{id:5,feed_id:1,title:'Publisher',url:'https://a.example',deleted:false} as never]} folders={[{id:7,name:'Work',rev:1,deleted:false}]}/>
  </QueryClientProvider>)
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=fulltext&subscription_id=5&folder_id=7&limit=10',expect.anything()))
  fireEvent.change(screen.getByLabelText('Feed'),{target:{value:'0'}})
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=fulltext&folder_id=7&limit=10',expect.anything()))
})
it('only requests similarity on a click and hides it when embeddings is disabled',async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {embed:'enabled'} : {matches:[],status:'paused'})
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  const {unmount} = render(<QueryClientProvider client={client}><MoreLikeThis entryID={4} onOpen={vi.fn()}/></QueryClientProvider>)
  const button = await screen.findByRole('button',{name:'More like this'})
  expect(request).not.toHaveBeenCalledWith('/v1/entries/4/similar',expect.anything())
  fireEvent.click(button)
  expect(await screen.findByText('Similar articles are paused.')).toBeTruthy()
  unmount()
  vi.mocked(request).mockResolvedValue({embed:'disabled'})
  render(<QueryClientProvider client={new QueryClient()}><MoreLikeThis entryID={4} onOpen={vi.fn()}/></QueryClientProvider>)
  await waitFor(() => expect(screen.queryByRole('button',{name:'More like this'})).toBeNull())
})

it('explains a paused embedding budget and keeps full-text search available',async () => {
  mount('paused')
  await screen.findByText('Hybrid search is paused. Full-text search is available.')
  await search()
  expect(request).toHaveBeenCalledWith('/v1/search?q=funding&mode=fulltext',expect.anything())
})
it('allows stored-vector similarity while the embedding budget is paused',async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {embed:'paused'} : {matches:[],status:'paused'})
  render(<QueryClientProvider client={new QueryClient()}><MoreLikeThis entryID={4} onOpen={vi.fn()}/></QueryClientProvider>)
  fireEvent.click(await screen.findByRole('button',{name:'More like this'}))
  expect(await screen.findByText('Similar articles are paused.')).toBeTruthy()
})
