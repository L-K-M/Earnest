import {readFileSync} from 'node:fs'
import {runInNewContext} from 'node:vm'
import {expect, it, vi} from 'vitest'

it.each([null, false, 42, 'text', []])('ignores a non-object push payload: %j', payload => {
  let push: (event: unknown) => void = () => undefined
  const showNotification = vi.fn()
  const self = {addEventListener:(name:string, handler:(event: unknown)=>void) => {if (name === 'push') push=handler},registration:{showNotification}}
  runInNewContext(readFileSync('public/push-worker.js','utf8'), {self})
  expect(() => push({data:{json:() => payload},waitUntil:vi.fn()})).not.toThrow()
  expect(showNotification).not.toHaveBeenCalled()
})

it('shows only server title and body and opens an internal notification link', async () => {
  type Event = {data?:{json:()=>unknown}; notification?:{close:()=>void;data:{id:string}}; waitUntil:(promise:Promise<unknown>)=>void}
  const listeners:Record<string,(event:Event)=>void> = {}
  const showNotification = vi.fn().mockResolvedValue(undefined)
  const openWindow = vi.fn().mockResolvedValue(undefined)
  const self = {addEventListener:(name:string, handler:(event:Event)=>void) => {listeners[name]=handler}, registration:{showNotification}, clients:{openWindow}}
  runInNewContext(readFileSync('public/push-worker.js','utf8'), {self})
  let pending:Promise<unknown> = Promise.resolve()
  listeners.push({data:{json:()=>({title:'Earnest',body:'1 alert',text:'private',notification_id:'42',url:'https://evil.test'})},waitUntil:(p:Promise<unknown>)=>{pending=p}})
  await pending
  expect(showNotification).toHaveBeenCalledWith('Earnest',{body:'1 alert',data:{id:'42'}})
  const close = vi.fn()
  listeners.notificationclick({notification:{close,data:{id:'42'}},waitUntil:(p:Promise<unknown>)=>{pending=p}})
  await pending
  expect(close).toHaveBeenCalled();expect(openWindow).toHaveBeenCalledWith('/?notification=42')
  listeners.notificationclick({notification:{close,data:{id:'https://evil.test'}},waitUntil:(p:Promise<unknown>)=>{pending=p}})
  await pending;expect(openWindow).toHaveBeenLastCalledWith('/')
})
