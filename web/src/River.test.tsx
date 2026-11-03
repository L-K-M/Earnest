// @vitest-environment jsdom
import {afterEach, describe, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import type {Entry, ReaderActions} from './reader'
import {request} from './request'
vi.mock('./request', () => ({request:vi.fn()}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count}: {count:number}) => ({getTotalSize:()=>count*100, getVirtualItems:()=>Array.from({length:Math.min(2,count)},(_,index)=>({index,size:100,start:index*100})), scrollToIndex:vi.fn()})}))
const entries = [1,2,3].map(id => ({id,seq:id,title:`Article ${id}`,blurb:'A cached line',subscription_id:1,deleted:false,state:{principal:1,entry_id:id,state_rev:0,seen:{value:'unseen',rev:0},shelf:{value:'none',rev:0},kept:{value:false,rev:0}}})) as Entry[]
const callbacks: IntersectionObserverCallback[] = []
const observed: Element[] = []
class Observer {
  constructor(callback: IntersectionObserverCallback) { callbacks.push(callback) }
  observe(element: Element) {observed.push(element)}
  disconnect() {}
}
afterEach(() => {cleanup();vi.clearAllMocks();vi.unstubAllGlobals();callbacks.length=0;observed.length=0})
function mount(shortcuts=true, blurbStatus="disabled") {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:blurbStatus} : path.startsWith('/v1/positions') ? {seq:0} : {entries})
  vi.stubGlobal('IntersectionObserver', Observer)
  const act = vi.fn().mockResolvedValue(undefined)
  const actions: ReaderActions = {act,state:entry=>entry.state}
  const onSelect=vi.fn()
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'today',title:'Today'}} subscriptions={[]} actions={actions} selected={entries[0]} onSelect={onSelect} onOpen={vi.fn()} shortcuts={shortcuts} counts={false} summary={{seen:0,done:0,later:0,kept:0}} onHelp={vi.fn()} /></QueryClientProvider>)
  return {act,onSelect}
}
describe('river interactions', () => {
  it('marks only intersecting rendered rows, never virtual overscan or absent rows', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    await waitFor(()=>expect(observed.length).toBeGreaterThan(0))
    const row=observed.find(element=>(element as HTMLElement).dataset.entry==='1')!
    callbacks.at(-1)!([{target:row,isIntersecting:true,intersectionRatio:1} as IntersectionObserverEntry], {} as IntersectionObserver)
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'seen','seen','passive'),{timeout:1500})
    expect(act).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Article 3')).toBeNull()
  })
  it('handles deliberate shortcuts without changing row order', async () => {
    const {act,onSelect}=mount()
    await screen.findByText('Article 1')
    fireEvent.keyDown(document.body,{key:'e'})
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'shelf','done'))
    fireEvent.keyDown(document.body,{key:'j'})
    expect(onSelect).toHaveBeenCalledWith(entries[1])
    expect(screen.getByText('Article 1')).toBeTruthy()
  })
  it('switches off single-key shortcuts', async () => {
    const {act}=mount(false)
    await screen.findByText('Article 1')
    fireEvent.keyDown(document.body,{key:'e'})
    expect(act).not.toHaveBeenCalled()
  })
})
it('retries a visible row after sync finishes restoring',async()=>{
  const {act}=mount()
  act.mockRejectedValueOnce(new Error('Reading state is being restored.'))
  await screen.findByText('Article 1')
  await waitFor(()=>expect(observed.length).toBeGreaterThan(0))
  const row=observed.find(element=>(element as HTMLElement).dataset.entry==='1')!
  callbacks.at(-1)!([{target:row,isIntersecting:true,intersectionRatio:1} as IntersectionObserverEntry],{} as IntersectionObserver)
  await waitFor(()=>expect(act).toHaveBeenCalledTimes(2),{timeout:2000})
})

it.each(['enabled','disabled'])('shows cached blurbs only when the task is %s',async status => {
  mount(true,status)
  await screen.findByText('Article 1')
  if (status === 'enabled') await waitFor(() => expect(screen.getAllByText('A cached line')).toHaveLength(2))
  else expect(screen.queryByText('A cached line')).toBeNull()
})
