// @vitest-environment jsdom
import {afterEach, describe, expect, it, vi} from 'vitest'
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {River} from './River'
import type {Entry, ReaderActions, Subscription} from './reader'
import {request} from './request'
vi.mock('./request', () => ({request:vi.fn()}))
const virtualItems = vi.hoisted(() => ({items: [0, 1]}))
const {measureElementMock} = vi.hoisted(() => ({measureElementMock: vi.fn()}))
vi.mock('@tanstack/react-virtual', () => ({useVirtualizer: ({count, scrollMargin = 0}: {count:number; scrollMargin?:number}) => ({getTotalSize:()=>count*100, getVirtualItems:()=>virtualItems.items.filter(index=>index<count).map(index=>({index,size:100,start:index*100 + scrollMargin})), scrollToIndex:vi.fn(), options:{scrollMargin}, measureElement:measureElementMock})}))
const observers: IntersectionObserverCallback[] = []
const observed: Element[] = []
class Observer {
  constructor(callback: IntersectionObserverCallback) { observers.push(callback) }
  observe(element: Element) { observed.push(element) }
  disconnect() {}
}
const entries = [1,2,3].map(id => ({id,seq:4-id,title:`Article ${id}`,blurb:'A cached line',subscription_id:1,deleted:false,state:{principal:1,entry_id:id,state_rev:0,seen:{value:'unseen',rev:0},shelf:{value:'none',rev:0},kept:{value:false,rev:0}}})) as Entry[]
function withShelf(entry: Entry, value: 'none' | 'later' | 'done'): Entry {
  return {...entry, state: {...entry.state, shelf: {...entry.state.shelf, value}}}
}
afterEach(() => {cleanup();vi.clearAllMocks();vi.restoreAllMocks();vi.unstubAllGlobals();virtualItems.items=[0,1];observers.length=0;observed.length=0})
function mount(shortcuts=true, blurbStatus="disabled", selected=entries[0], rows=entries, subscriptions: Subscription[]=[], reading=false) {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:blurbStatus} : path.startsWith('/v1/positions') ? {seq:0} : {entries:rows})
  const act = vi.fn().mockResolvedValue(undefined)
  const actions: ReaderActions = {act,state:entry=>entry.state}
  const onSelect=vi.fn()
  const onOpen=vi.fn()
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  const view=render(<QueryClientProvider client={client}><River view={{kind:'today',title:'Today'}} subscriptions={subscriptions} actions={actions} selected={selected} onSelect={onSelect} onOpen={onOpen} reading={reading} shortcuts={shortcuts} counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
  return {act,actions,onSelect,onOpen,client,container:view.container}
}
function scroller() { return screen.getByLabelText('Article list') }
function scrollPast(id: number) {
  scroller().scrollTop = id * 100 + 50
  fireEvent.scroll(scroller())
}
function positionWrites(): number[] {
  return vi.mocked(request).mock.calls
    .filter(([path, options]) => typeof path === 'string' && path.startsWith('/v1/positions') && (options as RequestInit | undefined)?.method === 'PUT')
    .map(([, options]) => JSON.parse(String((options as RequestInit).body)).seq as number)
}
describe('river interactions', () => {
  it.each([false,true])('does not mark rows seen merely because they render (reading: %s)', async reading => {
    vi.stubGlobal('IntersectionObserver', Observer)
    const {act}=mount(true,'disabled',entries[0],entries,[],reading)
    await screen.findByText('Article 1')
    // The replaced mechanism observed intersections; one must not mark a row now.
    const row=observed.find(element=>(element as HTMLElement).dataset.entry==='1')
    const callback=observers.at(-1)
    if (row && callback) callback([{target:row,isIntersecting:true,intersectionRatio:1} as IntersectionObserverEntry],{} as IntersectionObserver)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
  })
  it('does not mark the displayed article read during a background refresh', async () => {
    const {act: write,client,onOpen}=mount(true,'disabled',entries[0],entries,[],true)
    await screen.findByText('Article 1')
    await act(async () => {await client.invalidateQueries({queryKey:['river']})})
    const listRequests = vi.mocked(request).mock.calls.filter(([path]) => path.startsWith('/v1/entries?'))
    expect(listRequests).toHaveLength(2)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(write).not.toHaveBeenCalled()
    expect(onOpen).not.toHaveBeenCalled()
    expect(positionWrites()).toEqual([])
  })
  it('keeps scrolled-past rows unread without advancing the resume position', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    scrollPast(2)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
    expect(positionWrites()).toEqual([])
    expect(screen.queryByText('Article 3')).toBeNull()
  })
  it('keeps rows unread before and after scrolling past the Today panel', async () => {
    // The agent panel pushes the list down, so the list starts 40px into the
    // scroller. The virtualizer must report rows in scroller coordinates.
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return {top: this.classList.contains('virtual-list') ? 40 : 0} as DOMRect
    })
    const {act}=mount()
    await screen.findByText('Article 1')
    // Row 0 ends at 100px in list coordinates but 140px in the scroller.
    scroller().scrollTop = 100
    fireEvent.scroll(scroller())
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
    scrollPast(2)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
  })
  it('drops a queued open read when the reader marks the row unread', async () => {
    const {act}=mount()
    fireEvent.click(await screen.findByText('Article 1'))
    fireEvent.keyDown(document.body,{key:'m'})
    expect(act).toHaveBeenCalledWith(entries[0],'seen','unseen')
    await new Promise(resolve => window.setTimeout(resolve, 1300))
    expect(act).not.toHaveBeenCalledWith(entries[0],'seen','seen','passive')
  })
  it('offers a row\'s actions in its context menu, dropping a queued passive seen as m does', async () => {
    const act = vi.fn().mockResolvedValue(undefined)
    const onMenu = vi.fn()
    const read = {...entries[0], state:{...entries[0].state, seen:{value:'seen',rev:1}}} as Entry
    const rows = [read, entries[1], entries[2]]
    vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries:rows})
    render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'feed',id:1,title:'Rust'}} subscriptions={[]} actions={{act,state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} onMenu={onMenu} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
    const row = (await screen.findByText('Article 1')).closest('a')!
    fireEvent.click(row)
    fireEvent.contextMenu(row)
    expect(onMenu).toHaveBeenCalledWith(expect.anything(), read, expect.any(Array))
    const items = onMenu.mock.calls[0][2] as {id: string; label: string; run: () => Promise<void>}[]
    const unread = items.find(item => item.id === 'read')!
    expect(unread.label).toBe('Mark unread')
    await unread.run()
    expect(act).toHaveBeenCalledWith(read,'seen','unseen')
    await new Promise(resolve => window.setTimeout(resolve, 1300))
    expect(act).not.toHaveBeenCalledWith(read,'seen','seen','passive')
  })
  it('marks a row seen when it is opened', async () => {
    const {act}=mount()
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[0],'seen','seen','passive'),{timeout:1500})
  })
  it.each(['keyboard', 'reader action', 'remote'])('allows a later open after a settled passive read and %s unread', async source => {
    const {act: write, actions} = mount()
    let current = entries[0].state
    const setSeen = (value: 'seen' | 'unseen') => {
      current = {...current, state_rev: current.state_rev + 1, seen: {...current.seen, value, rev: current.seen.rev + 1}}
    }
    actions.state = () => current
    write.mockImplementation(async (_entry, register, value) => {
      if (register === 'seen' && (value === 'seen' || value === 'unseen')) setSeen(value)
    })
    const passive = () => write.mock.calls.filter(call => call[3] === 'passive')
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(passive()).toHaveLength(1), {timeout: 1500})
    if (source === 'keyboard') fireEvent.keyDown(document.body, {key: 'm'})
    else if (source === 'reader action') await actions.act(entries[0], 'seen', 'unseen')
    else setSeen('unseen')
    expect(current.seen.value).toBe('unseen')
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(passive()).toHaveLength(1)
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(passive()).toHaveLength(2), {timeout: 1500})
    expect(current.seen.value).toBe('seen')
  })
  it('does not retain an already-seen row against a later unread and open', async () => {
    const {act: write, actions} = mount()
    let current: Entry['state'] = {...entries[0].state, seen: {...entries[0].state.seen, value: 'seen'}}
    actions.state = () => current
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(positionWrites()).toEqual([3]), {timeout: 1500})
    expect(write).not.toHaveBeenCalled()
    current = {...current, seen: {...current.seen, value: 'unseen'}}
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(write).toHaveBeenCalledWith(entries[0], 'seen', 'seen', 'passive'), {timeout: 1500})
  })
  it('keeps a later open queued while its earlier passive write is outstanding', async () => {
    const {act: write} = mount()
    let finish: (() => void) | undefined
    write.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(write).toHaveBeenCalledTimes(1), {timeout: 1500})
    fireEvent.click(screen.getByText('Article 1'))
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(write).toHaveBeenCalledTimes(1)
    await act(async () => { finish!() })
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2), {timeout: 1500})
  })
  it.each(['remote', 'keyboard'])('does not retry older failed reading after a newer %s unread', async source => {
    const {act: write, actions} = mount()
    let current = entries[0].state
    actions.state = () => current
    let reject: ((error: Error) => void) | undefined
    write.mockImplementationOnce(() => new Promise<void>((_resolve, fail) => { reject = fail }))
    const passive = () => write.mock.calls.filter(call => call[3] === 'passive')
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(passive()).toHaveLength(1), {timeout: 1500})
    if (source === 'keyboard') fireEvent.keyDown(document.body, {key: 'm'})
    else current = {...current, seen: {...current.seen, value: 'unseen', rev: current.seen.rev + 1}}
    await act(async () => { reject!(new Error('Could not persist reading progress.')) })
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(passive()).toHaveLength(1)
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(() => expect(passive()).toHaveLength(2), {timeout: 1500})
  })
  const navigation = [
    {key:'j', from:0, to:1}, {key:'k', from:1, to:0},
    {key:'ArrowDown', from:0, to:1}, {key:'ArrowUp', from:1, to:0},
    {key:'Home', from:1, to:0}, {key:'End', from:0, to:2},
  ]
  it.each(navigation)('keeps both rows unread when $key only changes the selection', async ({key, from, to}) => {
    const {act,onSelect,onOpen}=mount(true,'disabled',entries[from])
    await screen.findByText('Article 1')
    fireEvent.keyDown(scroller(),{key})
    expect(onSelect).toHaveBeenCalledWith(entries[to])
    expect(onOpen).not.toHaveBeenCalled()
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).not.toHaveBeenCalled()
    expect(positionWrites()).toEqual([])
  })
  it.each(navigation)('preserves an explicit unread when $key opens another article', async ({key, from, to}) => {
    const {act: write,actions,onOpen}=mount(true,'disabled',entries[from],entries,[],true)
    const states = new Map(entries.map(entry => [entry.id, entry.state]))
    actions.state = entry => states.get(entry.id)!
    write.mockImplementation(async (entry: Entry, register, value) => {
      if (register !== 'seen') return
      const state = actions.state(entry)
      states.set(entry.id, {...state, seen: {...state.seen, value, rev: state.seen.rev + 1}})
    })
    const previous = entries[from]
    const next = entries[to]
    fireEvent.click(await screen.findByText(previous.title))
    await waitFor(() => expect(actions.state(previous).seen.value).toBe('seen'),{timeout:1500})
    // The reader toolbar writes through the same actions without cancelling
    // the list's queue directly. Leaving must not create a new read intent.
    await actions.act(previous,'seen','unseen')
    onOpen.mockClear()
    fireEvent.keyDown(scroller(),{key})
    expect(onOpen).toHaveBeenCalledWith(next,'replace')
    await waitFor(() => expect(actions.state(next).seen.value).toBe('seen'),{timeout:1500})
    expect(actions.state(previous).seen.value).toBe('unseen')
    expect(write.mock.calls.filter(call => call[0].id === previous.id && call[3] === 'passive')).toHaveLength(1)
    scrollPast(2)
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(actions.state(previous).seen.value).toBe('unseen')
  })
  it.each([{key:'j', index:2}, {key:'k', index:0}, {key:'Home', index:0}, {key:'End', index:2}])('does not mark read when $key stays at a list boundary', async ({key, index}) => {
    const {act,onOpen}=mount(true,'disabled',entries[index],entries,[],true)
    await screen.findByText('Article 1')
    fireEvent.keyDown(scroller(),{key:'m'})
    fireEvent.keyDown(scroller(),{key})
    expect(onOpen).not.toHaveBeenCalled()
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(act).toHaveBeenCalledExactlyOnceWith(entries[index],'seen','unseen')
  })
  it('never regresses the resume position when an earlier row becomes due later', async () => {
    const {act}=mount(true,'disabled',entries[1])
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(()=>expect(positionWrites()).toEqual([3]),{timeout:1500})
    // Opening an older article must not move the shared high-water position
    // backwards when that row becomes due.
    fireEvent.click(screen.getByText('Article 2'))
    await waitFor(()=>expect(act).toHaveBeenCalledWith(entries[1],'seen','seen','passive'),{timeout:1500})
    expect(positionWrites()).toEqual([3])
  })
  it('backs off before retrying a failed position write', async () => {
    mount()
    await screen.findByText('Article 2')
    let failed=false
    vi.mocked(request).mockImplementation(async (path, options) => {
      if (typeof path === 'string' && path.startsWith('/v1/positions') && (options as RequestInit | undefined)?.method === 'PUT' && !failed) {
        failed=true
        throw new Error('offline')
      }
      return path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries}
    })
    fireEvent.click(screen.getByText('Article 1'))
    await waitFor(()=>expect(positionWrites()).toEqual([3]),{timeout:1500})
    await new Promise(resolve => window.setTimeout(resolve, 700))
    expect(positionWrites()).toEqual([3])
    await waitFor(()=>expect(positionWrites()).toEqual([3,3]),{timeout:3000})
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
    expect(within(header).getByText('One per story')).toBeTruthy()
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
  it('exposes each row to the virtualizer measurement hook', async () => {
    measureElementMock.mockClear()
    mount()
    await screen.findByText('Article 1')
    const rows = document.querySelectorAll('.virtual-row')
    expect(rows.length).toBeGreaterThan(0)
    expect(measureElementMock).toHaveBeenCalledTimes(rows.length)
    for (const row of rows) expect(row.getAttribute('data-index')).toMatch(/^\d+$/)
  })
  it('shows one representative per story with its also-in count', async () => {
    vi.mocked(request).mockImplementation(async path => {
      if (path === '/v1/pipeline') return {blurb:'disabled'}
      if (path.startsWith('/v1/positions')) return {seq:0}
      return path.includes('group=cluster') ? {entries, groups:[{entry:entries[0],also_in:2}]} : {entries}
    })
    vi.stubGlobal('IntersectionObserver', Observer)
    const actions: ReaderActions = {act:vi.fn().mockResolvedValue(undefined),state:entry=>entry.state}
    render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'today',title:'Today'}} subscriptions={[]} actions={actions} selected={entries[0]} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts={false} counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
    await screen.findByText('Article 1')
    fireEvent.click(screen.getByText('One per story'))
    await waitFor(()=>expect(screen.getByText(/also in 2/)).toBeTruthy())
    expect(vi.mocked(request).mock.calls.some(([path]) => path.includes('group=cluster'))).toBe(true)
  })
})
it('retries an opened row after sync finishes restoring',async()=>{
  const {act}=mount()
  act.mockRejectedValueOnce(new Error('Reading state is being restored.'))
  fireEvent.click(await screen.findByText('Article 1'))
  await waitFor(()=>expect(act).toHaveBeenCalledTimes(2),{timeout:2000})
})

it('shows stored feed icons and thumbnails, with a letter mark when a feed has no icon',async()=>{
  const subscription = {id:1,title:'404 Media',icon_url:'/v1/subscriptions/1/icon?v=abc'} as Subscription
  const rows = [{...entries[0],thumbnail_url:'/v1/entries/1/thumbnail?v=def'},{...entries[1],subscription_id:2}]
  const {container}=mount(true,'disabled',entries[0],rows,[subscription,{id:2,title:'Plain Feed'} as Subscription])
  await screen.findByText('Article 1')
  const [first, second] = Array.from(container.querySelectorAll<HTMLElement>('.entry'))
  expect(first.querySelector('.feed-icon')?.getAttribute('src')).toBe('/v1/subscriptions/1/icon?v=abc')
  expect(first.querySelector('.entry-thumb')?.getAttribute('src')).toBe('/v1/entries/1/thumbnail?v=def')
  expect(first.querySelector('.entry-thumb')?.getAttribute('alt')).toBe('')
  expect(second.querySelector('.feed-icon')).toBeNull()
  expect(second.querySelector('.feed-mark')?.textContent).toBe('P')
  expect(second.querySelector('.entry-thumb')).toBeNull()
  // A copy that fails to load gives way to the letter mark and to no thumbnail.
  fireEvent.error(first.querySelector('.feed-icon')!)
  fireEvent.error(first.querySelector('.entry-thumb')!)
  expect(first.querySelector('.feed-mark')?.textContent).toBe('4')
  expect(first.querySelector('.entry-thumb')).toBeNull()
  expect(within(first).getByText('404 Media')).toBeTruthy()
})

it.each(['enabled','disabled'])('shows cached blurbs only when the task is %s',async status => {
  mount(true,status)
  await screen.findByText('Article 1')
  if (status === 'enabled') await waitFor(() => expect(screen.getAllByText('A cached line')).toHaveLength(2))
  else expect(screen.queryByText('A cached line')).toBeNull()
})

it('reports the newest loaded entry for the mark-rest command', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries})
  const onNewestLoaded = vi.fn()
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'today',title:'Today'}} subscriptions={[]} actions={{act:vi.fn(),state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} onNewestLoaded={onNewestLoaded} /></QueryClientProvider>)
  await waitFor(() => expect(onNewestLoaded).toHaveBeenLastCalledWith(Math.max(...entries.map(entry => entry.seq))))
})

it('ignores saved positions when loading and refetching articles', async () => {
  virtualItems.items = [0,1,2]
  // Sync and a tab refocus may refetch positions. Neither the initial saved
  // position nor a later one should bound the newest-first article query.
  let seq = 1
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq} : {entries})
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  render(<QueryClientProvider client={client}><River view={{kind:'today',title:'Today'}} subscriptions={[]} actions={{act:vi.fn(),state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
  await screen.findByText('Article 1')
  const listRequests = () => vi.mocked(request).mock.calls.map(([path]) => String(path)).filter(path => path.startsWith('/v1/entries?'))
  expect(listRequests()).toEqual([expect.stringContaining('direction=backward')])
  expect(listRequests()[0]).not.toContain('after_seq')
  seq = 2
  await act(async () => { await client.invalidateQueries() })
  // Query results reach components a tick later; give a restart its chance.
  await act(() => new Promise(resolve => setTimeout(resolve, 50)))
  expect(screen.getByText('Article 1')).toBeTruthy()
  expect(screen.queryByText('Loading articles…')).toBeNull()
  // The list refetched in place: no third request from the new position.
  expect(listRequests()).toEqual([expect.stringContaining('direction=backward'), expect.stringContaining('direction=backward')])
  expect(listRequests().every(path => !path.includes('after_seq'))).toBe(true)
})

it('loads and marks articles seen while waiting for the shared position', async () => {
  let finishPosition: ((value: {seq: number}) => void) | undefined
  const position = new Promise<{seq: number}>(resolve => { finishPosition = resolve })
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? position : {entries})
  const write = vi.fn().mockResolvedValue(undefined)
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'feed',id:9,title:'Rust'}} subscriptions={[]} actions={{act:write,state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
  fireEvent.click(await screen.findByText('Article 1'))
  await waitFor(() => expect(write).toHaveBeenCalledWith(entries[0], 'seen', 'seen', 'passive'), {timeout:1500})
  expect(positionWrites()).toEqual([])
  await act(async () => { finishPosition!({seq:10}) })
  await new Promise(resolve => window.setTimeout(resolve, 700))
  // The delayed position must not be overwritten with this older article.
  expect(positionWrites()).toEqual([])
})

it('fetches newly arrived articles when returning to a cached list', async () => {
  let rows = entries
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:99} : {entries:rows})
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  const tree = <QueryClientProvider client={client}><River view={{kind:'feed',id:9,title:'Rust'}} subscriptions={[]} actions={{act:vi.fn(),state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>
  const first = render(tree)
  await screen.findByText('Article 1')
  first.unmount()
  rows = [{...entries[0], id:4, seq:4, title:'Newest arrival'}, ...entries]
  render(tree)
  await screen.findByText('Newest arrival')
  const queries = vi.mocked(request).mock.calls.map(([path]) => String(path)).filter(path => path.startsWith('/v1/entries?'))
  expect(queries).toHaveLength(2)
  expect(queries.every(path => path.includes('direction=backward') && !path.includes('after_seq') && !path.includes('cursor'))).toBe(true)
})

describe('list navigation', () => {
  function mountWith(props: {reading?: boolean; selected?: Entry; shortcuts?: boolean}) {
    vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries})
    const onSelect = vi.fn()
    const onOpen = vi.fn()
    render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'feed',id:9,title:'Rust'}} subscriptions={[]} actions={{act:vi.fn().mockResolvedValue(undefined),state:entry=>entry.state}} selected={props.selected ?? entries[0]} onSelect={onSelect} onOpen={onOpen} reading={props.reading} shortcuts={props.shortcuts ?? true} counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
    return {onSelect, onOpen}
  }
  it('renders rows as links to the article in this view with one tab stop', async () => {
    mountWith({})
    const link = (await screen.findByText('Article 1')).closest('a')!
    expect(link.getAttribute('href')).toBe('/feeds/9/articles/1')
    expect(document.querySelectorAll('.entry[tabindex="0"]')).toHaveLength(1)
  })
  it('leaves modified clicks to the browser so articles open in a new tab', async () => {
    const {onOpen} = mountWith({})
    const link = (await screen.findByText('Article 2')).closest('a')!
    const event = new MouseEvent('click', {bubbles:true, cancelable:true, metaKey:true})
    link.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    expect(onOpen).not.toHaveBeenCalled()
  })
  it('moves the selection with arrow keys even with single-key shortcuts off', async () => {
    const {onSelect} = mountWith({shortcuts:false})
    const link = (await screen.findByText('Article 1')).closest('a')!
    link.focus()
    fireEvent.keyDown(link, {key:'ArrowDown'})
    expect(onSelect).toHaveBeenLastCalledWith(entries[1])
    fireEvent.keyDown(link, {key:'End'})
    expect(onSelect).toHaveBeenLastCalledWith(entries[2])
  })
  it('opens the next article in place while one is being read', async () => {
    const {onOpen} = mountWith({reading:true})
    await screen.findByText('Article 1')
    fireEvent.keyDown(document.body, {key:'j'})
    expect(onOpen).toHaveBeenCalledWith(entries[1], 'replace')
  })
  it('names each list’s emptiness instead of reusing the river line', async () => {
    vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries:[]})
    render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'later',title:'Later'}} subscriptions={[]} actions={{act:vi.fn(),state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
    expect(await screen.findByText(/Nothing waiting for later/)).toBeTruthy()
  })
})

it('says when the feed being read is failing and links to its settings', async () => {
  vi.mocked(request).mockImplementation(async path => path === '/v1/pipeline' ? {blurb:'disabled'} : path.startsWith('/v1/positions') ? {seq:0} : {entries})
  const failing = {id:9, title:'Rust', health:{consecutive_failures:3, error_class:''}} as unknown as Subscription
  render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><River view={{kind:'feed',id:9,title:'Rust'}} subscriptions={[failing]} actions={{act:vi.fn(),state:entry=>entry.state}} selected={null} onSelect={vi.fn()} onOpen={vi.fn()} shortcuts counts={false} summary={{seen:0,done:0,later:0,kept:0}} /></QueryClientProvider>)
  expect(await screen.findByText('Fetch failed.')).toBeTruthy()
  expect(screen.getByRole('link',{name:'Fix it in feed settings'}).getAttribute('href')).toBe('/feeds#subscription-9')
  expect(screen.getByRole('link',{name:'Feed settings'}).getAttribute('href')).toBe('/feeds#subscription-9')
})

it('leaves Enter on a brief summary to the summary instead of opening the selection', async () => {
  const {container} = mount()
  await screen.findByText('Article 1')
  const details = document.createElement('details')
  const summary = document.createElement('summary')
  summary.textContent = 'While you were out'
  details.append(summary)
  container.append(details)
  const onOpenCalls = vi.mocked(request).mock.calls.length
  const event = new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true})
  summary.dispatchEvent(event)
  expect(event.defaultPrevented).toBe(false)
  expect(vi.mocked(request).mock.calls.length).toBe(onOpenCalls)
})
