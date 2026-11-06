// @vitest-environment jsdom
import {afterEach, describe, expect, it, vi} from 'vitest'
import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import type {Entry, ReaderActions} from './reader'
import {request} from './request'
vi.mock('./request', () => ({request:vi.fn()}))
const virtualItems = vi.hoisted(() => ({items: [0, 1]}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count}: {count:number}) => ({getTotalSize:()=>count*100, getVirtualItems:()=>virtualItems.items.filter(index=>index<count).map(index=>({index,size:100,start:index*100})), scrollToIndex:vi.fn()})}))
const observers: IntersectionObserverCallback[] = []
const observed: Element[] = []
class Observer {
  constructor(callback: IntersectionObserverCallback) { observers.push(callback) }
  observe(element: Element) { observed.push(element) }
  disconnect() {}
}
const entries = [1,2,3].map(id => ({id,seq:id,title:`Article ${id}`,blurb:'A cached line',subscription_id:1,deleted:false,state:{principal:1,entry_id:id,state_rev:0,seen:{value:'unseen',rev:0},shelf:{value:'none',rev:0},kept:{value:false,rev:0}}})) as Entry[]
function withShelf(entry: Entry, value: 'none' | 'later' | 'done'): Entry {
  return {...entry, state: {...entry.state, shelf: {...entry.state.shelf, value}}}
}
afterEach(() => {cleanup();vi.clearAllMocks();vi.unstubAllGlobals();virtualItems.items=[0,1];observers.length=0;observed.length=0})
function mount(shortcuts=true, blurbStatus="disabled", selected=entries[0], rows=entries) {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:blurbStatus} : path.startsWith('/v1/positions') ? {seq:0} : {entries:rows})
  const act = vi.fn().mockResolvedValue(undefined)
  const markAll = vi.fn().mockResolvedValue(undefined)
  const actions: ReaderActions = {act,markAll,state:entry=>entry.state}
  const onSelect=vi.fn()
  const view=render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'today',title:'Today'}} subscriptions={[]} actions={actions} selected={selected} onSelect={onSelect} onOpen={vi.fn()} shortcuts={shortcuts} counts={false} summary={{seen:0,done:0,later:0,kept:0}} onHelp={vi.fn()} /></QueryClientProvider>)
  return {act,markAll,onSelect,container:view.container}
}
function scroller() { return screen.getByLabelText('Article list') }
function scrollPast(id: number) {
  scroller().scrollTop = id * 100 + 50
  fireEvent.scroll(scroller())
}
describe('river interactions', () => {
  it('does not mark rows seen merely because they render', async () => {
    vi.stubGlobal('IntersectionObserver', Observer)
    const {act}=mount()
    await screen.findByText('Article 1')
    // The replaced mechanism observed intersections; one must not mark a row now.
    const row=observed.find(element=>(element as HTMLElement).dataset.entry==='1')
    const callback=observers.at(-1)
    if (row && callback) callback([{target:row,isIntersecting:true,intersectionRatio:1} as IntersectionObserverEntry],{} as IntersectionObserver)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
  })
  it('marks a rendered row once it leaves through the top edge', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    scrollPast(1)
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'seen','seen','passive'),{timeout:1500})
    expect(act).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Article 3')).toBeNull()
  })
  it('drops a queued passive seen when the reader marks the row unread', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    scrollPast(1)
    fireEvent.keyDown(document.body,{key:'m'})
    expect(act).toHaveBeenCalledWith(entries[0],'seen','unseen')
    await new Promise(resolve => window.setTimeout(resolve, 1300))
    expect(act).not.toHaveBeenCalledWith(entries[0],'seen','seen','passive')
  })
  it('marks a row seen when it is opened', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'seen','seen','passive'),{timeout:1500})
  })
  it('marks the row the selection leaves when j moves down', async () => {
    const {act,onSelect}=mount()
    await screen.findByText('Article 1')
    fireEvent.keyDown(document.body,{key:'j'})
    expect(onSelect).toHaveBeenCalledWith(entries[1])
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'seen','seen','passive'),{timeout:1500})
  })
  it('marks the row the selection leaves when k moves up', async () => {
    const {act,onSelect}=mount(true,'disabled',entries[1])
    await screen.findByText('Article 1')
    fireEvent.keyDown(document.body,{key:'k'})
    expect(onSelect).toHaveBeenCalledWith(entries[0])
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[1],'seen','seen','passive'),{timeout:1500})
  })
  it('keeps rows skipped by a jump unseen', async () => {
    virtualItems.items=[1,2]
    const {act}=mount()
    await screen.findByText('Article 2')
    scrollPast(1)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
  })
  it('keeps both river header actions fully visible', async () => {
    const {container}=mount()
    await screen.findByText('Article 1')
    const header=container.querySelector('.river header') as HTMLElement
    expect(within(header).getByText('Refresh articles')).toBeTruthy()
    expect(within(header).getByText('Earlier articles')).toBeTruthy()
  })
  it('labels shelf states in title case', async () => {
    const done=withShelf(entries[0],'done')
    const later=withShelf(entries[1],'later')
    mount(true,'disabled',done,[done,later,entries[2]])
    await screen.findByText('Article 1')
    expect(screen.getByText('Feed · Done')).toBeTruthy()
    expect(screen.getByText('Feed · Later')).toBeTruthy()
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
  it('marks the whole view read after confirmation', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {markAll}=mount()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Mark all as read'))
    await waitFor(()=>expect(markAll).toHaveBeenCalledWith({kind:'today',title:'Today'}))
    confirm.mockRestore()
  })
  it('leaves the river alone when mark-all is cancelled', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {markAll}=mount()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Mark all as read'))
    expect(markAll).not.toHaveBeenCalled()
    confirm.mockRestore()
  })
})
it('retries a scrolled-past row after sync finishes restoring',async()=>{
  const {act}=mount()
  act.mockRejectedValueOnce(new Error('Reading state is being restored.'))
  await screen.findByText('Article 1')
  scrollPast(1)
  await waitFor(()=>expect(act).toHaveBeenCalledTimes(2),{timeout:2000})
})

it.each(['enabled','disabled'])('shows cached blurbs only when the task is %s',async status => {
  mount(true,status)
  await screen.findByText('Article 1')
  if (status === 'enabled') await waitFor(() => expect(screen.getAllByText('A cached line')).toHaveLength(2))
  else expect(screen.queryByText('A cached line')).toBeNull()
})
