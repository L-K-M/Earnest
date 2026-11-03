import {expect,it} from 'vitest'
import {ReaderSync,LocalLocks,type RequestFn} from '../sync/runtime'
import {MemorySyncStore} from '../sync/store'
import {emptyEntry,initialState} from '../sync/types'
import {parseJson,asArray,stringifyJson} from '../sync/json'
import {decodeWrite,encodeResult} from '../sync/wire'
import {Model} from '../sync/model'

it('keeps offline intent in the existing durable outbox across restart and flushes once online',async () => {
  const now = Date.parse('2026-11-03T12:00:00Z')
  const store = new MemorySyncStore()
  const model = new Model('g1')
  model.ingest({...emptyEntry(),id:1n,subscriptionId:1n,seq:1n,firstSeenAt:now},now)
  let online = true
  let sent = 0
  const request: RequestFn = async <T,>(path: string,options?: RequestInit): Promise<T> => {
    if (!online) throw new TypeError('Offline')
    if (path === '/v1/bootstrap') return '{"generation":"g1","rev":0}' as T
    if (path.startsWith('/v1/bootstrap/')) return '{"items":[],"has_more":false}' as T
    if (path === '/v1/state:batch') {
      sent++
      const writes = asArray(parseJson(String(options?.body)),'writes').map(decodeWrite)
      return stringifyJson(model.batch({actor:1n,token:'human',receipt:now},writes).map(encodeResult)) as T
    }
    return '{"changes":[],"next":{"generation":"g1","rev":0}}' as T
  }
  const options = {request,principal:1n,store,locks:new LocalLocks(),channel:null,openEvents:() => null,now:() => now,newOpId:() => 'offline-later'}
  const first = new ReaderSync(options)
  await first.stepOnce()
  await first.observe({...emptyEntry(),id:1n,subscriptionId:1n,seq:1n},initialState(1n,1n),'g1')
  online = false
  await first.queue(1n,'shelf','later')
  expect(first.state(1n,1n).shelf.value).toBe('later')
  await expect(first.stepOnce()).rejects.toThrow('Offline')
  expect((await store.load())?.pending).toHaveLength(1)
  const second = new ReaderSync(options)
  online = true
  // The first step restores the snapshot; the next flushes its persisted head.
  await second.stepOnce()
  await second.stepOnce()
  expect(second.status().outboxSize).toBe(0)
  expect(model.state(1n,1n).shelf.value).toBe('later')
  expect(sent).toBe(1)
})
