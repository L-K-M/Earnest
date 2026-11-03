// @vitest-environment jsdom
import type {ReactNode} from 'react'
import {afterEach,expect,it,vi} from 'vitest'
import {act as reactAct,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {createRoot} from 'react-dom/client'
import {request} from '../request'
import {defaultSettings} from '../Settings'
import {clearOffline} from '../offline/Offline'
import type {Entry,View} from '../reader'

const {changes,act,entry} = vi.hoisted(() => ({changes:{current:null as null | ((reason?: 'generation') => void)},act:vi.fn().mockResolvedValue(undefined),entry:{id:4,title:'Selected article',subscription_id:8,feed_id:42,state:{shelf:{value:'none'},kept:{value:false},seen:{value:'unseen'}}}}))
vi.mock('react-dom/client',() => ({createRoot:vi.fn()}))
vi.mock('../request',() => ({request:vi.fn(),setCSRF:vi.fn()}))
vi.mock('../syncBridge',() => ({useReaderSync:(_identity: unknown,onChanges: (reason?: 'generation') => void) => {changes.current=onChanges;return {state:(value: Entry) => value.state,act,retry:vi.fn(),status:'Reading actions synced.',error:''}}}))
vi.mock('../offline/Offline',() => ({OfflineStatus:() => null,registerOffline:vi.fn().mockResolvedValue(undefined),clearOffline:vi.fn().mockResolvedValue(undefined)}))
vi.mock('../River',() => ({River:({view,onSelect}: {view: View; onSelect: (entry: Entry) => void}) => <main><h2>{view.title} articles</h2><button onClick={() => onSelect(entry as Entry)}>Select article</button></main>}))
afterEach(() => {cleanup();localStorage.clear();vi.resetAllMocks()})

it('connects sidebar searches, feed and label commands, and reading actions to the existing shell',async () => {
  HTMLDialogElement.prototype.showModal = function() {this.setAttribute('open','')}
  HTMLDialogElement.prototype.close = function() {this.removeAttribute('open')}
  vi.mocked(createRoot).mockImplementation(() => ({render:(node: ReactNode) => {render(node)},unmount:() => undefined}))
  localStorage.setItem('earnest.csrf','token')
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id:1,principal_id:1}
    if (path === '/v1/settings') return {...defaultSettings,rev:1}
    if (path === '/v1/subscriptions') return [{id:8,feed_id:42,title:'Rust',url:'https://feed.test',deleted:false}]
    if (path === '/v1/labels') return [{ID:3,Name:'Research',Deleted:false}]
    if (path === '/v1/saved-searches') return [{id:2,name:'Rust searches',query:'rust',filters:{}}]
    if (path === '/v1/pipeline') return {embed:'disabled',classify:'disabled',semantic:'disabled',watch:'disabled',ask:'enabled'}
    if (path.startsWith('/v1/search')) return {matches:[]}
    return []
  })
  await import('../main')
  fireEvent.click(await screen.findByRole('button',{name:'Ask my feeds'},{timeout:10000}))
  expect(screen.getByRole('heading',{name:'Ask my feeds'})).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Today'}))
  fireEvent.click(await screen.findByRole('button',{name:'Select article'}))
  function palette(query: string) {
    fireEvent.keyDown(document.body,{key:'k',ctrlKey:true})
    fireEvent.change(screen.getByRole('combobox'),{target:{value:query}})
    fireEvent.keyDown(screen.getByRole('combobox'),{key:'Enter'})
  }
  palette('Mark Done')
  await waitFor(() => expect(act).toHaveBeenCalledWith(entry,'shelf','done','explicit'))
  await waitFor(() => expect(screen.queryByRole('dialog',{name:'Command palette'})).toBeNull())
  reactAct(() => {changes.current?.('generation')})
  await waitFor(() => expect(clearOffline).toHaveBeenCalledOnce())
  await screen.findByRole('heading',{name:'Today articles'})
  palette('Ask my feeds')
  expect(await screen.findByRole('heading',{name:'Ask my feeds'})).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('dialog',{name:'Command palette'})).toBeNull())
  palette('Feed: Rust')
  expect(await screen.findByRole('heading',{name:'Rust articles'})).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('dialog',{name:'Command palette'})).toBeNull())
  palette('Label: Research')
  expect(await screen.findByRole('heading',{name:'Research articles'})).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Rust searches'}))
  expect(await screen.findByRole('heading',{name:'Rust searches'})).toBeTruthy()
  await waitFor(() => expect(request).toHaveBeenCalledWith('/v1/search?q=rust&mode=fulltext',expect.anything()))
}, 15000)
