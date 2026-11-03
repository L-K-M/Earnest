import {expect, it} from 'vitest'
import {SyncClient} from './client'
import {type Write} from './types'
const write = (opId: string, value: 'seen' | 'unseen'): Write => ({opId,entryId:1n,principal:1n,register:'seen',value,base:{generation:'g1',rev:0n},occurredAt:Date.parse('2026-11-02T12:00:00Z'),kind:'explicit'})
it('preserves an uncertain submitted payload when a newer intent is queued before retry', () => {
  const client = new SyncClient({generation:'g1',rev:0n})
  client.queue(write('first','seen'))
  const submitted=client.beginFlush()
  client.flushFailed()
  client.queue(write('replacement','unseen'))
  expect(client.outbox()).toHaveLength(2)
  expect(client.beginFlush()).toEqual(submitted)
})
it('preserves submitted payloads through same-generation cursor expiry', () => {
  const client = new SyncClient({generation:'g1',rev:0n})
  client.queue(write('first','seen'))
  const submitted=client.beginFlush()
  client.beginBootstrap({generation:'g1',rev:4n})
  client.queue(write('replacement','unseen'))
  expect(client.outbox()).toHaveLength(2)
  expect(client.beginFlush()).toEqual(submitted)
})
it('reserves the generated Done-clearing suffix for causal operations', () => {
  const client = new SyncClient({generation:'g1',rev:0n})
  expect(()=>client.queue(write('spoof/clear-done','unseen'))).toThrow()
})
