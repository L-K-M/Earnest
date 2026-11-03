// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {request} from '../request'
import type {Entry,Subscription} from '../reader'
import {Rules} from './Rules'
vi.mock('../request',() => ({request:vi.fn()}))
afterEach(() => {cleanup();vi.resetAllMocks()})
const rule = {id:1,name:'Noise',predicate:{type:'feed_in',ids:[42]},actions:[{type:'notify',budget:3}],enabled:true,match_count:14,last_match_at:'2026-11-01T12:00:00Z'}
function mount(pipeline='enabled', watch=false) {
  vi.mocked(request).mockImplementation(async (path,options) => {
    if (path === '/v1/pipeline') return {embed:pipeline,classify:pipeline,semantic:pipeline,watch:pipeline,compile_rule:pipeline}
    if (path === '/v1/rules' && !options?.method) return watch ? [rule,{...rule,id:2,name:'Security watch',predicate:{type:'semantic',value:'advisories'}}] : [rule]
    if (path === '/v1/watch-rules') return watch ? [{...rule,id:2,name:'Security watch',predicate:{type:'semantic',value:'advisories'},check_llm:false,check_budget:3,status:pipeline}] : []
    if (path === '/v1/webhooks') return []
    if (path === '/v1/rules:dry-run') return {entries:[{id:9,title:'Matching story'} as Entry]}
    return {}
  })
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Rules feeds={[{id:8,feed_id:42,title:'Publisher'} as Subscription]} folders={[{id:7,name:'Work',rev:1,deleted:false}]} labels={[{ID:3,Name:'Reading',Rev:1,Deleted:false}]}/></QueryClientProvider>)
}
async function edit() {fireEvent.click(await screen.findByRole('button',{name:'Edit Noise'}));return screen.getByRole('region',{name:'Rule editor'})}
it('lists statistics and disables and deletes through their API operations',async () => {
  mount()
  expect(await screen.findByText(/14 matches/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Disable Noise'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/rules/1:disable',{method:'POST'}))
  await waitFor(() => expect((screen.getByRole('button',{name:'Delete Noise'}) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole('button',{name:'Delete Noise'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/rules/1',{method:'DELETE'}))
}, 15000)
it('requires a fresh dry run, lists matches, and saves only on a click',async () => {
  mount()
  const editor = await edit()
  expect(within(editor).queryByRole('button',{name:'Save'})).toBeNull()
  fireEvent.click(within(editor).getByRole('button',{name:/Dry run/}))
  expect(await screen.findByText('Would have matched 1 items.')).toBeTruthy()
  expect(vi.mocked(request).mock.calls.filter(([path,options]) => path === '/v1/rules/1' && options?.method === 'PATCH')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button',{name:'Review them'}))
  expect(screen.getByText('Matching story')).toBeTruthy()
  expect(screen.getByRole('region',{name:'Rule editor'})).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Renamed'}})
  expect(screen.queryByRole('button',{name:'Save'})).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:/Dry run/}))
  fireEvent.click(await screen.findByRole('button',{name:'Save'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/rules/1',expect.objectContaining({method:'PATCH',body:JSON.stringify({...rule,name:'Renamed',id:undefined,match_count:undefined,last_match_at:undefined})})))
})
it('uses feed IDs, explains restrictions inline and hides model conditions and watches when disabled',async () => {
  mount('disabled')
  await edit()
  expect(screen.getByRole('option',{name:'Publisher'}).getAttribute('value')).toBe('42')
  expect(screen.queryByRole('option',{name:'Semantic match'})).toBeNull()
  expect(screen.queryByRole('option',{name:'Classifier label'})).toBeNull()
  expect(screen.queryByRole('region',{name:'Watch rules'})).toBeNull()
  fireEvent.change(screen.getByLabelText('Condition type'),{target:{value:'folder_in'}})
  fireEvent.change(screen.getByLabelText('Action 1 type'),{target:{value:'set_shelf'}})
  expect(screen.getAllByText(/Shelf changes need feeds you chose/).length).toBeGreaterThan(0)
  expect((screen.getByRole('button',{name:/Dry run/}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByLabelText('Action 1 type'),{target:{value:'webhook'}})
  expect(screen.getByText(/Webhooks allow only feeds/)).toBeTruthy()
})
it('shows server validation errors and never offers Save after failed dry run',async () => {
  mount()
  await edit()
  vi.mocked(request).mockRejectedValueOnce(new Error('rule reference: feed no longer belongs to you'))
  fireEvent.click(screen.getByRole('button',{name:/Dry run/}))
  expect(await screen.findByRole('alert')).toHaveProperty('textContent','rule reference: feed no longer belongs to you')
  expect(screen.queryByRole('button',{name:'Save'})).toBeNull()
})
it('creates a notify-only watch with explicit budgets',async () => {
  mount()
  const region = await screen.findByRole('region',{name:'Watch rules'})
  fireEvent.change(within(region).getByLabelText('Watch name'),{target:{value:'Security'}})
  fireEvent.change(within(region).getByLabelText('Semantic description'),{target:{value:'Rust security advisories'}})
  fireEvent.change(within(region).getByLabelText('Daily watch notification budget'),{target:{value:'2'}})
  fireEvent.click(within(region).getByRole('button',{name:'Create watch'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/watch-rules',expect.objectContaining({method:'POST',body:JSON.stringify({name:'Security',description:'Rust security advisories',budget:2,enabled:true,check_llm:false,check_budget:3})})))
})

it.each(['enabled','disabled'])('keeps watches out of the generic editor and hides them when pipeline is %s',async pipeline => {
  mount(pipeline,true)
  await screen.findByRole('button',{name:'Edit Noise'})
  expect(screen.queryByRole('button',{name:'Edit Security watch'})).toBeNull()
  if (pipeline === 'disabled') {
    expect(screen.queryByText('Security watch')).toBeNull()
    return
  }
  fireEvent.click(screen.getByRole('button',{name:'Disable watch Security watch'}))
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/rules/2:disable',expect.objectContaining({method:'POST'})))
})

it('compiles into the editor, dry runs, and requires confirmation before saving',async () => {
  mount()
  await edit()
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/rules:compile') return {predicate:{type:'feed_in',ids:[42]},actions:[{type:'notify',budget:3}],feeds_to_confirm:[{id:42,name:'Publisher'}]}
    if (path === '/v1/rules:dry-run') return {entries:[{id:9,title:'Matching story'}]}
    return []
  })
  fireEvent.change(screen.getByLabelText('Describe a rule'),{target:{value:'Notify for Publisher'}})
  fireEvent.click(screen.getByRole('button',{name:'Compile rule'}))
  expect(await screen.findByText('Publisher (ID 42)')).toBeTruthy()
  expect(await screen.findByText('Would have matched 1 items.')).toBeTruthy()
  expect((screen.getByRole('button',{name:'Save'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByLabelText('Confirm these feeds'))
  expect((screen.getByRole('button',{name:'Save'}) as HTMLButtonElement).disabled).toBe(false)
  expect(vi.mocked(request).mock.calls.some(([path,options]) => path === '/v1/rules/1' && options?.method === 'PATCH')).toBe(false)
})

it('retains the operation ID when rule creation is retried', async () => {
  mount()
  fireEvent.click(await screen.findByRole('button',{name:'New rule'}))
  fireEvent.change(screen.getByLabelText('Name'),{target:{value:'Retry'}})
  const selection = screen.getByLabelText('Condition selection') as HTMLSelectElement
  selection.options[0].selected = true
  fireEvent.change(selection)
  fireEvent.click(screen.getByRole('button',{name:/Dry run/}))
  await screen.findByText('Would have matched 1 items.')
  const original = vi.mocked(request).getMockImplementation()!
  let attempts = 0
  const bodies: string[] = []
  vi.mocked(request).mockImplementation(async (path,options) => {
    if (path === '/v1/rules' && options?.method === 'POST') {
      bodies.push(String(options.body)); attempts++
      if (attempts === 1) throw new Error('Lost response')
      return {}
    }
    return original(path,options)
  })
  fireEvent.click(screen.getByRole('button',{name:'Save'}))
  await screen.findByText('Lost response')
  fireEvent.click(screen.getByRole('button',{name:'Save'}))
  await waitFor(() => expect(bodies).toHaveLength(2))
  expect(JSON.parse(bodies[0]).op_id).toBeTruthy()
  expect(bodies[1]).toBe(bodies[0])
}, 15000)
