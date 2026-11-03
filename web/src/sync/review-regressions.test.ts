import {expect,it} from 'vitest'
import {decodeEntry} from './wire'
import {ZERO_TIME} from './time'
import {SyncClient} from './client'
import {initialState,type Entry,type Write} from './types'

it('bootstraps an undated entry returned by the API',() => {
  expect(decodeEntry({id:1n,subscription_id:2n,seq:3n,rev:4n,published_at:null}).publishedAt).toBe(ZERO_TIME)
})

it('accepts resubscription backfill after a gone acknowledgement, including after restart',() => {
  const cursor = {generation:'g',rev:1n}
  const client = new SyncClient(cursor)
  const entry: Entry = {id:1n,subscriptionId:2n,seq:3n,rev:1n,deleted:false,firstSeenAt:ZERO_TIME,publishedAt:ZERO_TIME}
  client.mergeEntry(entry)
  const write: Write = {opId:'old',principal:1n,entryId:1n,register:'seen',value:'seen',base:cursor,occurredAt:0,kind:'explicit'}
  client.queue(write)
  client.beginFlush()
  client.acknowledge('g',[{opId:'old',status:'gone',state:initialState(1n,1n),replay:false}])
  expect(client.entry(1n)?.deleted).toBe(true)
  const restarted = SyncClient.fromSnapshot(client.toSnapshot())
  restarted.mergeEntry({...entry,subscriptionId:5n,rev:2n})
  expect(restarted.entry(1n)).toMatchObject({subscriptionId:5n,deleted:false,rev:2n})
  restarted.queue({...write,opId:'new'})
  expect(restarted.state(1n,1n).seen.value).toBe('seen')
})

it('keeps a revisioned deletion permanent after a gone acknowledgement',() => {
  const client = new SyncClient({generation:'g',rev:0n})
  const entry: Entry = {id:1n,subscriptionId:2n,seq:3n,rev:1n,deleted:false,firstSeenAt:ZERO_TIME,publishedAt:ZERO_TIME}
  client.mergeEntry(entry)
  client.acknowledge('g',[{opId:'old',status:'gone',state:initialState(1n,1n),replay:false}])
  client.mergeEntry(entry)
  expect(client.entry(1n)?.deleted).toBe(true)
  client.mergeEntry({...entry,rev:2n,deleted:true})
  client.mergeEntry({...entry,rev:3n})
  expect(client.entry(1n)).toMatchObject({rev:2n,deleted:true})
})

it('rebootstraps legacy tombstones without losing pending intent',async () => {
  const {ReaderSync,LocalLocks} = await import('./runtime')
  const {MemorySyncStore} = await import('./store')
  const cursor = {generation:'g',rev:1n}
  const client = new SyncClient(cursor)
  const entry: Entry = {id:1n,subscriptionId:2n,seq:3n,rev:1n,deleted:true,firstSeenAt:ZERO_TIME,publishedAt:ZERO_TIME}
  client.mergeEntry(entry)
  client.queue({opId:'pending',principal:1n,entryId:1n,register:'seen',value:'seen',base:cursor,occurredAt:0,kind:'explicit'})
  const legacy = client.toSnapshot()
  delete legacy.goneEntries
  legacy.bootstrapped = true
  const store = new MemorySyncStore()
  await store.save(legacy)
  const runtime = new ReaderSync({principal:1n,store,locks:new LocalLocks(),channel:null,openEvents:()=>null,request:async () => {throw new Error('No request expected')}})
  await runtime.observe(entry,initialState(1n,1n))
  expect(runtime.status().bootstrapped).toBe(false)
  expect(runtime.status().outboxSize).toBe(1)
})
