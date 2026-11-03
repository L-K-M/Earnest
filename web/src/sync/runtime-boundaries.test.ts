import {expect,it} from 'vitest'
import {ReaderSync, LocalLocks, type RequestFn} from './runtime'
import {MemorySyncStore} from './store'
it('restarts an interrupted bootstrap instead of treating its captured cursor as a complete cache',async()=>{
  let fail=true
  let bootstraps=0
  const request:RequestFn=async<T,>(path:string):Promise<T>=>{
    if(path==='/v1/bootstrap'){bootstraps++;return '{"generation":"g1","rev":0}' as T}
    if(path.startsWith('/v1/bootstrap/entries') && fail) throw new Error('offline during paging')
    if(path.startsWith('/v1/bootstrap/')) return '{"items":[],"has_more":false}' as T
    return '{"changes":[],"next":{"generation":"g1","rev":0}}' as T
  }
  const store=new MemorySyncStore()
  const runtime=new ReaderSync({request,principal:1n,store,locks:new LocalLocks(),channel:null,openEvents:()=>null})
  await expect(runtime.stepOnce()).rejects.toThrow('offline')
  fail=false
  await runtime.stepOnce()
  expect(bootstraps).toBe(2)
  expect(runtime.status().bootstrapped).toBe(true)
})
it('starts its first changes read without needing an SSE event',async()=>{
  const store=new MemorySyncStore()
  const calls:string[]=[]
  const request:RequestFn=async<T,>(path:string):Promise<T>=>{
    calls.push(path)
    return (path==='/v1/bootstrap' ? '{"generation":"g1","rev":0}' : path.startsWith('/v1/bootstrap/') ? '{"items":[],"has_more":false}' : '{"changes":[],"next":{"generation":"g1","rev":0}}') as T
  }
  const runtime=new ReaderSync({request,principal:1n,store,locks:new LocalLocks(),channel:null,openEvents:()=>null})
  await runtime.start()
  for(let i=0;i<100;i++) await Promise.resolve()
  expect(calls).toContain('/v1/bootstrap')
  await runtime.stop()
})
it('rolls back the visible overlay if durable storage rejects a queued action',async()=>{
  const memory=new MemorySyncStore()
  let full=false
  const store={load:()=>memory.load(),save:async(snapshot:import('./client').ClientSnapshot)=>{
    if(full) throw new Error('Storage is full')
    await memory.save(snapshot)
  }}
  const request:RequestFn=async<T,>(path:string):Promise<T> => (path==='/v1/bootstrap' ? '{"generation":"g1","rev":0}' : '{"items":[],"has_more":false}') as T
  const runtime=new ReaderSync({request,principal:1n,store,locks:new LocalLocks(),channel:null,openEvents:()=>null})
  await runtime.stepOnce()
  const {initialState,emptyEntry}=await import('./types')
  await runtime.observe({...emptyEntry(),id:1n,seq:1n,subscriptionId:1n},initialState(1n,1n))
  full=true
  await expect(runtime.queue(1n,'shelf','done')).rejects.toThrow('Storage is full')
  expect(runtime.state(1n,1n).shelf.value).toBe('none')
})
it('rejects entry metadata captured under an older generation before merging its revisions',async()=>{
  const request:RequestFn=async<T,>(path:string):Promise<T> => (path==='/v1/bootstrap' ? '{"generation":"g2","rev":0}' : '{"items":[],"has_more":false}') as T
  const runtime=new ReaderSync({request,principal:1n,store:new MemorySyncStore(),locks:new LocalLocks(),channel:null,openEvents:()=>null})
  await runtime.stepOnce()
  const {initialState,emptyEntry}=await import('./types')
  await expect(runtime.observe({...emptyEntry(),id:1n,rev:99n},initialState(1n,1n),'g1')).rejects.toThrow('Reading state changed')
  expect(runtime.hasEntry(1n)).toBe(false)
})
