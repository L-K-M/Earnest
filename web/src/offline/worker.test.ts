import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {readingCache,shellCache} from './policy'

const origin = 'https://reader.test'
type Key = Request | string
const href = (key: Key) => new URL(typeof key === 'string' ? key : key.url,origin).href
class TestCache {
  rows = new Map<string,Response>()
  async match(key: Key) {return this.rows.get(href(key))?.clone()}
  async put(key: Key, response: Response) {this.rows.set(href(key),response.clone())}
  async delete(key: Key) {return this.rows.delete(href(key))}
  async keys() {return [...this.rows.keys()].map(url => new Request(url))}
  async addAll(keys: string[]) {for(const key of keys) await this.put(key,await fetch(key))}
}
let cachesByName: Map<string,TestCache>
let handlers: Map<string,(event: unknown) => void>
const json = (value: unknown, status=200) => new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
async function cache(name=readingCache) {
  if(!cachesByName.has(name)) cachesByName.set(name,new TestCache())
  return cachesByName.get(name)!
}
function dispatch(path: string, method='GET') {
  let response: Promise<Response> | undefined
  const waits: Promise<unknown>[] = []
  handlers.get('fetch')!({request:new Request(new URL(path,origin),{method}),respondWith:(value: Promise<Response>) => {response=value},waitUntil:(value: Promise<unknown>) => waits.push(value)})
  return {response,done:() => Promise.all(waits)}
}
beforeEach(async () => {
  vi.resetModules()
  cachesByName = new Map();handlers = new Map()
  vi.stubGlobal('self',{location:{origin},clients:{claim:vi.fn()},addEventListener:(kind: string,handler: (event: unknown) => void) => handlers.set(kind,handler)})
  vi.stubGlobal('caches',{open:cache,delete:async(name: string) => cachesByName.delete(name)})
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(json({})))
  await import('./worker')
})
afterEach(() => {vi.unstubAllGlobals();vi.resetAllMocks()})

it('preloads the production shell and its fingerprinted assets',async () => {
  vi.mocked(fetch).mockImplementation(async input => String(input) === '/' ? new Response('<script src="/assets/app-1.js"></script><link href="/assets/app-1.css">',{headers:{'Content-Type':'text/html'}}) : new Response('asset'))
  let done: Promise<unknown> = Promise.resolve()
  handlers.get('install')!({waitUntil:(value: Promise<unknown>) => {done=value}})
  await done
  const shell = await cache(shellCache)
  expect([...shell.rows.keys()]).toEqual([`${origin}/`,`${origin}/assets/app-1.js`,`${origin}/assets/app-1.css`])
})
it('warms visited stored content and falls back to it only when the network fails',async () => {
  vi.mocked(fetch).mockImplementation(async input => href(input as Key).includes('/content') ? json({entry_id:7,text:'Cached article'}) : json({entries:[{id:7},{id:8}]}))
  const visit = dispatch('/v1/entries?view=river')
  expect((await visit.response)?.ok).toBe(true)
  await visit.done()
  expect((await cache()).rows.has(`${origin}/v1/entries/7/content`)).toBe(true)
  vi.mocked(fetch).mockRejectedValue(new TypeError('Offline'))
  const offline = dispatch('/v1/entries/7/content')
  expect(await (await offline.response)!.json()).toEqual({entry_id:7,text:'Cached article'})
  await offline.done()
  expect(dispatch('/v1/state:batch','POST').response).toBeUndefined()
  expect(dispatch('/web/content-url/7').response).toBeUndefined()
  expect(dispatch('https://publisher.test/article').response).toBeUndefined()
})
it('does not turn authentication errors into stale successful reads',async () => {
  await (await cache()).put('/v1/me',json({user_id:1,principal_id:1}))
  vi.mocked(fetch).mockResolvedValue(json({detail:'Sign in again'},401))
  const expired = dispatch('/v1/me')
  expect((await expired.response)?.status).toBe(401)
  await expired.done()
  expect(cachesByName.has(readingCache)).toBe(false)
})
it('preserves server failures and successful reads even when cache storage is full',async () => {
  await (await cache()).put('/v1/settings',json({rev:1}))
  vi.mocked(fetch).mockResolvedValue(json({detail:'Unavailable'},503))
  expect((await dispatch('/v1/settings').response)?.status).toBe(503)
  const privateCache = await cache()
  vi.spyOn(privateCache,'put').mockRejectedValue(new Error('Quota exceeded'))
  vi.mocked(fetch).mockResolvedValue(json({rev:2}))
  expect(await (await dispatch('/v1/settings').response)!.json()).toEqual({rev:2})
})
it('clears old identity data and fences content fetches still in flight during logout',async () => {
  await (await cache()).put('/v1/me',json({user_id:1,principal_id:1}))
  await (await cache()).put('/v1/entries/1/content',json({text:'Private'}))
  vi.mocked(fetch).mockResolvedValue(json({user_id:2,principal_id:2}))
  const identity = dispatch('/v1/me')
  await identity.response;await identity.done()
  expect((await cache()).rows.has(`${origin}/v1/entries/1/content`)).toBe(false)
  let release: (response: Response) => void = () => undefined
  const pending = new Promise<Response>(resolve => {release=resolve})
  vi.mocked(fetch).mockImplementation(async input => href(input as Key).endsWith('/content') ? pending : json({entries:[{id:9}]}))
  const visit = dispatch('/v1/entries?view=river')
  await visit.response
  await vi.waitFor(() => expect(fetch).toHaveBeenCalledWith(expect.objectContaining({url:`${origin}/v1/entries/9/content`})))
  const logout = dispatch('/v1/session/logout','POST')
  await logout.response
  release(json({text:'Old session'}))
  await visit.done()
  expect((await cache()).rows.size).toBe(0)
})
