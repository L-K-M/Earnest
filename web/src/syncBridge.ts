import {useEffect, useRef, useState} from 'react'
import {requestText} from './request'
import {ReaderSync, type RequestFn} from './sync/runtime'
import {type Entry, type EntryState, type Register, type Value} from './reader'
import {parseJson} from './sync/json'
import {decodeEntry, decodeState} from './sync/wire'
import {formatTime} from './sync/time'
import type {Register as SyncRegister, State} from './sync/types'

function safeNumber(value: bigint): number {
  const number = Number(value)
  if (!Number.isSafeInteger(number)) throw new Error('This identifier exceeds the reader’s supported range.')
  return number
}
export function nativeState(state: State): EntryState {
  const register = (value: SyncRegister) => ({value: value.value === 'true' ? true : value.value === 'false' ? false : value.value, rev:safeNumber(value.rev), occurred_at:formatTime(value.occurredAt), actor:safeNumber(value.actor), source:value.source})
  return {principal:safeNumber(state.principal), entry_id:safeNumber(state.entryId), state_rev:safeNumber(state.stateRev), seen:register(state.seen), shelf:register(state.shelf), kept:register(state.kept)}
}
const syncRequest: RequestFn = async <T,>(path: string, options?: RequestInit): Promise<T> => await requestText(path, options) as T

export function useReaderSync(identity: {user_id: number; principal_id: number} | null, onChanges: (reason?: 'generation') => void) {
  const runtime = useRef<ReaderSync | null>(null)
  const [status, setStatus] = useState('Opening sync…')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [, setRevision] = useState(0)
  const changes = useRef(onChanges)
  useEffect(() => {changes.current=onChanges},[onChanges])
  const userID=identity?.user_id
  const principal=identity?.principal_id
  useEffect(() => {
    if (principal === undefined || userID === undefined) return
    let alive=true
    let cleanup: (()=>void)|undefined
    const start = async () => {
      // A shared outbox requires a cross-tab lock, including in HTTP tailnets.
      if (!navigator.locks) throw new Error('Shared reading actions need Web Locks. Open Earnest over HTTPS or localhost in a current browser.')
      if (!window.indexedDB) throw new Error('IndexedDB is unavailable. Allow browser storage to save reading actions.')
      const sync=new ReaderSync({request:syncRequest, principal:BigInt(principal), device:`user:${userID}:principal:${principal}`, onDropped:dropped => {
        if (alive) setError(`${dropped.length} reading operation(s) were not saved: ${dropped.map(item=>`${item.opId} (${item.status})`).join(', ')}.`)
      }, onError:error=>{if(alive)setError(error instanceof Error ? error.message : 'Sync failed.')}})
      runtime.current=sync
      let lastCursor=''
      let lastGeneration=''
      const unsubscribe=sync.subscribe(value=>{
        if (!alive) return
        setStatus(value.bootstrapped ? (value.outboxSize ? `${value.outboxSize} reading action(s) waiting to sync.` : 'Reading actions synced.') : 'Restoring reading state…')
        const cursor=`${value.cursor.generation}:${value.cursor.rev}`
        if (cursor !== lastCursor) {
          const rotated=lastGeneration !== '' && value.cursor.generation !== lastGeneration
          lastCursor=cursor;lastGeneration=value.cursor.generation
          changes.current(rotated ? 'generation' : undefined)
        }
        setRevision(old=>old+1)
      })
      cleanup=()=>{unsubscribe();void sync.stop()}
      await sync.start()
      return cleanup
    }
    void start().then(stop=>{if(!alive)stop?.()}).catch(error=>{cleanup?.();if(alive){runtime.current=null;setError(error instanceof Error ? error.message : 'Could not start sync.')}})
    return () => {alive=false;cleanup?.();runtime.current=null}
  },[principal,userID,attempt])
  return {
    status,error,
    deleted:(entry:Entry):boolean => entry.deleted || runtime.current?.isDeleted(BigInt(entry.id)) === true,
    state:(entry:Entry):EntryState => {
      const sync=runtime.current
      if (!sync || !sync.hasEntry(BigInt(entry.id))) return entry.state
      return nativeState(sync.state(BigInt(principal ?? entry.state.principal),BigInt(entry.id)))
    },
    act:async (entry:Entry, register:Register, value:Value, kind:'explicit'|'passive'='explicit'):Promise<void> => {
      const sync=runtime.current
      if (!sync) throw new Error('Reading state is not ready. Try again after sync opens.')
      if (!sync.status().bootstrapped) throw new Error('Reading state is being restored. Try again when sync finishes.')
      const generation=sync.cursor().generation
      await sync.observe(decodeEntry(parseJson(JSON.stringify(entry))), decodeState(parseJson(JSON.stringify(entry.state))), generation)
      await sync.queue(BigInt(entry.id),register,value,kind)
      sync.kick()
    },
    actRange:async (scope:{kind:'all'|'subscriptions'|'folder'; ids:number[]}, throughSeq:number):Promise<void> => {
      const sync=runtime.current
      if (!sync) throw new Error('Reading state is not ready. Try again after sync opens.')
      if (!sync.status().bootstrapped) throw new Error('Reading state is being restored. Try again when sync finishes.')
      await sync.queueRange({kind:scope.kind, ids:scope.ids.map(id => BigInt(id))}, {throughSeq:BigInt(throughSeq)})
      sync.kick()
    },
    // The newest article this device has synced, or 0 while sync is closed.
    newestSeq:():number => {
      const sync=runtime.current
      return sync ? safeNumber(sync.newestSeq()) : 0
    },
    retry:() => {setError('');if(runtime.current)runtime.current.kick();else setAttempt(old=>old+1)},
  }
}
