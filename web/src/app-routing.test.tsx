// @vitest-environment jsdom
import {useState, type ReactNode} from 'react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {act as reactAct, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {createRoot} from 'react-dom/client'
import {request, RequestError} from './request'
import {defaultSettings} from './Settings'
import type {Entry, View} from './reader'

const {act, entries, listMounted} = vi.hoisted(() => {
  const state = (shelf = 'none') => ({shelf:{value:shelf}, kept:{value:false}, seen:{value:'unseen'}})
  return {listMounted: vi.fn(), act:vi.fn().mockResolvedValue(undefined), entries:[4,5].map(id => ({id, title:`Article ${id}`, subscription_id:8, feed_id:42, url:`https://example.test/${id}`, state:state()}))}
})
vi.mock('react-dom/client',() => ({createRoot:vi.fn()}))
vi.mock('./request',async original => ({...await original<typeof import('./request')>(), request:vi.fn(), setCSRF:vi.fn()}))
vi.mock('./syncBridge',() => ({useReaderSync:() => ({state:(value: Entry) => value.state,act,actRange:vi.fn(),retry:vi.fn(),status:'Reading actions synced.',error:''})}))
vi.mock('./offline/Offline',() => ({OfflineStatus:() => null,useOnline:() => true,registerOffline:vi.fn().mockResolvedValue(undefined),clearOffline:vi.fn().mockResolvedValue(undefined)}))
vi.mock('./River',() => ({River: function River({view,onOpen,unreadOnly}: {view: View; onOpen: (entry: Entry) => void; unreadOnly?: boolean}) {
  useState(() => {listMounted(view); return null})
  return <section data-unread-only={unreadOnly}><h2>{view.title} list</h2>{entries.map(entry => <button key={entry.id} onClick={() => onOpen(entry as Entry)}>Open {entry.title}</button>)}</section>
}}))
vi.mock('./ArticleReader',() => ({Reader:({entry,onClose}: {entry: Entry; onClose: () => void}) => <article><h2>{entry.title}</h2><button onClick={onClose}>Back</button></article>}))

beforeEach(async () => {
  HTMLDialogElement.prototype.showModal = function() {this.setAttribute('open','')}
  HTMLDialogElement.prototype.close = function() {this.removeAttribute('open');this.dispatchEvent(new Event('close'))}
  localStorage.setItem('earnest.csrf','token')
  vi.mocked(request).mockImplementation(async path => {
    if (path === '/v1/me') return {user_id:1,principal_id:1}
    if (path === '/v1/settings') return {...defaultSettings,rev:1}
    if (path === '/v1/subscriptions') return [{id:8,feed_id:42,title:'Rust',url:'https://feed.test',deleted:false,folder_id:0,health:{consecutive_failures:0}}]
    if (path === '/v1/entries/4') return entries[0]
    if (path === '/v1/entries/77') throw new RequestError(404, 'Not found')
    if (path === '/v1/entries/5') return new Promise(() => undefined)
    if (path === '/v1/pipeline') return {ask:'disabled'}
    return []
  })
  vi.mocked(createRoot).mockImplementation(() => ({render:(node: ReactNode) => {render(node)},unmount:() => undefined}))
})
afterEach(() => {cleanup();localStorage.clear();vi.clearAllMocks()})

async function start(path: string) {
  history.replaceState(null, '', path)
  vi.resetModules()
  await import('./main')
}

it('opens the article named in the URL and closes it with Escape', async () => {
  await start('/feeds/8/articles/4')
  expect(await screen.findByRole('heading',{name:'Article 4'},{timeout:5000})).toBeTruthy()
  await waitFor(() => expect(document.title).toBe('Article 4 · Rust · Earnest'))
  fireEvent.keyDown(document.body,{key:'Escape'})
  await waitFor(() => expect(location.pathname).toBe('/feeds/8'))
  expect(screen.queryByRole('heading',{name:'Article 4'})).toBeNull()
  expect(screen.getByRole('heading',{name:'Rust list'})).toBeTruthy()
})

it('opens from the list with a new history entry and goes back to it', async () => {
  await start('/today')
  fireEvent.click(await screen.findByRole('button',{name:'Open Article 5'},{timeout:5000}))
  await waitFor(() => expect(location.pathname).toBe('/today/articles/5'))
  // The list's copy renders the article at once; the refresh behind it
  // (which never answers here) is not waited for.
  expect(await screen.findByRole('heading',{name:'Article 5'})).toBeTruthy()
  await waitFor(() => expect(vi.mocked(request).mock.calls.some(([path]) => path === '/v1/entries/5')).toBe(true))
  fireEvent.click(screen.getByRole('button',{name:'Back'}))
  await waitFor(() => expect(location.pathname).toBe('/today'))
})

it('keeps a list visit while opening and closing articles, but renews it on intentional parent navigation', async () => {
  const original = vi.mocked(request).getMockImplementation()!
  vi.mocked(request).mockImplementation(async (path, options) => path === '/v1/settings' ? {...defaultSettings, show_only_unread: true, rev: 1} : original(path, options))
  await start('/feeds/8')
  fireEvent.click(await screen.findByRole('button', {name: 'Open Article 4'}, {timeout: 5000}))
  await screen.findByRole('heading', {name: 'Article 4'})
  expect(listMounted).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', {name: 'Back'}))
  await waitFor(() => expect(location.pathname).toBe('/feeds/8'))
  expect(listMounted).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', {name: 'Open Article 4'}))
  await screen.findByRole('heading', {name: 'Article 4'})
  fireEvent.click(screen.getByRole('link', {name: 'Rust'}))
  await waitFor(() => expect(listMounted).toHaveBeenCalledTimes(2))
})

it('explains an article that no longer exists instead of showing a blank pane', async () => {
  await start('/today/articles/77')
  const problem = await screen.findByRole('heading',{name:'This article is no longer available'},{timeout:5000})
  await waitFor(() => expect(document.activeElement).toBe(problem))
  await waitFor(() => expect(document.title).toBe('Article unavailable · Today · Earnest'))
})

it('reports an unknown address and a deleted feed in words', async () => {
  await start('/nowhere')
  const heading = await screen.findByRole('heading',{name:'There is nothing at this address'},{timeout:5000})
  await waitFor(() => expect(document.activeElement).toBe(heading))
  cleanup()
  await start('/feeds/99')
  const gone = await screen.findByRole('heading',{name:'This feed is gone'},{timeout:5000})
  await waitFor(() => expect(document.activeElement).toBe(gone))
})

it('offers an undo for a deliberate action and restores the previous value with z', async () => {
  await start('/feeds/8/articles/4')
  await screen.findByRole('heading',{name:'Article 4'},{timeout:5000})
  fireEvent.keyDown(document.body,{key:'k',ctrlKey:true})
  fireEvent.change(screen.getByRole('combobox'),{target:{value:'Mark Done'}})
  fireEvent.keyDown(screen.getByRole('combobox'),{key:'Enter'})
  await waitFor(() => expect(act).toHaveBeenCalledWith(entries[0],'shelf','done','explicit'))
  expect(await screen.findByText('Marked Done.')).toBeTruthy()
  reactAct(() => {fireEvent.keyDown(document.body,{key:'z'})})
  await waitFor(() => expect(act).toHaveBeenLastCalledWith(entries[0],'shelf','none'))
  expect(await screen.findByText('Undone.')).toBeTruthy()
})

it('follows go-to shortcuts without letting the second key act on the list', async () => {
  await start('/today')
  await screen.findByRole('heading',{name:'Today list'},{timeout:5000})
  // Single-key shortcuts wait for the account's setting to load.
  await reactAct(async () => {await new Promise(resolve => setTimeout(resolve, 50))})
  fireEvent.keyDown(document.body,{key:'g'})
  fireEvent.keyDown(document.body,{key:'e'})
  await waitFor(() => expect(location.pathname).toBe('/edition'))
  expect(act).not.toHaveBeenCalled()
})
