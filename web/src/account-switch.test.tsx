// @vitest-environment jsdom
import type {ReactNode} from 'react'
import {afterEach,expect,it,vi} from 'vitest'
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {createRoot} from 'react-dom/client'
import {request} from './request'
import {defaultSettings} from './Settings'
import type {Entry,View} from './reader'

const {entry,late} = vi.hoisted(() => ({late:{open:null as null | ((entry: Entry) => void)},entry:{id:4,title:'A private article',author:'A private author',url:'https://private.test/article',subscription_id:8,feed_id:42,state:{shelf:{value:'none'},kept:{value:false},seen:{value:'unseen'}}}}))
vi.mock('react-dom/client',() => ({createRoot:vi.fn()}))
vi.mock('./request',() => ({request:vi.fn(),setCSRF:vi.fn()}))
vi.mock('./syncBridge',() => ({useReaderSync:() => ({state:(value: Entry) => value.state,act:vi.fn(),retry:vi.fn(),status:'Synced',error:''})}))
vi.mock('./offline/Offline',() => ({OfflineStatus:() => null,useOnline:() => true,registerOffline:vi.fn().mockResolvedValue(undefined),clearOffline:vi.fn().mockResolvedValue(undefined)}))
vi.mock('./River',() => ({River:({view,onOpen}: {view: View;onOpen:(entry: Entry)=>void}) => <main><h2>{view.title} articles</h2><button onClick={() => onOpen(entry as Entry)}>Open private article</button></main>}))
vi.mock('./FrontPage',() => ({FrontPage:() => <main><h2>Front page articles</h2></main>}))
vi.mock('./ArticleReader',() => ({Reader:({entry,onOpen}:{entry: Entry;onOpen:(entry: Entry)=>void}) => {late.open=onOpen;return <article>{entry.title} {entry.author} <a href={entry.url}>Original</a></article>}}))
afterEach(() => {cleanup();localStorage.clear();vi.resetAllMocks()})

it('clears private reader metadata and the selected feed when accounts switch',async () => {
  let account = 1
  vi.mocked(createRoot).mockImplementation(() => ({render:(node: ReactNode) => {render(node)},unmount:() => undefined}))
  localStorage.setItem('earnest.csrf','token')
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id:account,principal_id:account}
    if (path === '/v1/settings') return {...defaultSettings,rev:1}
    if (path === '/v1/session/login') return {csrf_token:'new'}
    if (path === '/v1/subscriptions') return account===1 ? [{id:8,feed_id:42,title:'A private feed',url:'https://private.test',deleted:false}] : []
    return []
  })
  await import('./main')
  fireEvent.click(await screen.findByRole('link',{name:'A private feed'},{timeout:10000}))
  fireEvent.click(screen.getByRole('button',{name:'Open private article'}))
  expect(screen.getByText(/A private article/)).toBeTruthy()
  const oldOpen = late.open!
  fireEvent.click(screen.getByRole('button',{name:'Sign out'}))
  await screen.findByRole('button',{name:'Sign in'})
  account = 2
  fireEvent.change(screen.getByLabelText('Username'),{target:{value:'B'}})
  fireEvent.change(screen.getByLabelText('Password'),{target:{value:'password'}})
  fireEvent.click(screen.getByRole('button',{name:'Sign in'}))
  await screen.findByRole('button',{name:'Sign out'}, {timeout:10000})
  // A request from A can finish after B has signed in.
  act(() => oldOpen(entry as Entry))
  await waitFor(() => expect(screen.queryByText(/A private article/)).toBeNull())
  expect(screen.queryByText(/A private author/)).toBeNull()
  expect(document.querySelector('a[href="https://private.test/article"]')).toBeNull()
  expect(screen.getByRole('heading',{name:'Front page articles'})).toBeTruthy()
},15000)
