// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MessageChannel} from 'node:worker_threads'
import {OfflineArticle,OfflineStatus,registerOffline,clearOffline} from './Offline'
import {request} from '../request'
vi.mock('../request',() => ({request:vi.fn()}))
afterEach(() => {cleanup();vi.restoreAllMocks();vi.resetAllMocks();vi.unstubAllGlobals();vi.useRealTimers();delete (navigator as unknown as {serviceWorker?: unknown}).serviceWorker})

it.each([null,{scriptURL:'https://reader.test/push-worker.js'}])('fences an active offline worker before deleting cache with controller %j',async controller => {
  vi.stubGlobal('MessageChannel',MessageChannel)
  let acknowledge = () => undefined as void
  const postMessage = vi.fn((_message,ports) => {acknowledge=() => {ports[0].postMessage('cleared');ports[0].close()}})
  Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{controller,getRegistration:vi.fn().mockResolvedValue({active:{scriptURL:'https://reader.test/offline-worker.js',postMessage}})}})
  const remove = vi.fn().mockResolvedValue(true)
  vi.stubGlobal('caches',{delete:remove})
  const clearing = clearOffline()
  await Promise.resolve()
  expect(postMessage).toHaveBeenCalledWith({type:'earnest:clear-offline'},expect.any(Array))
  expect(remove).not.toHaveBeenCalled()
  acknowledge()
  await clearing
  expect(remove).toHaveBeenCalledWith('earnest-reading-v1')
})

it('refuses to finish clearing if the active worker does not acknowledge',async () => {
  vi.useFakeTimers()
  const close = vi.fn()
  vi.stubGlobal('MessageChannel',class {port1={close,onmessage:null};port2={}})
  Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{controller:null,getRegistration:vi.fn().mockResolvedValue({active:{scriptURL:'https://reader.test/offline-worker.js',postMessage:vi.fn()}})}})
  const remove = vi.fn()
  vi.stubGlobal('caches',{delete:remove})
  const clearing = clearOffline().then(() => null,error => error as Error)
  await vi.advanceTimersByTimeAsync(5000)
  expect(await clearing).toBeInstanceOf(Error)
  expect((await clearing)?.message).toContain('Retry sign out')
  expect(close).toHaveBeenCalledOnce()
  expect(remove).not.toHaveBeenCalled()
})

it('quietly reports connectivity changes',() => {
  const online = vi.spyOn(navigator,'onLine','get')
  online.mockReturnValue(true)
  render(<OfflineStatus/> )
  expect(screen.queryByRole('status')).toBeNull()
  online.mockReturnValue(false)
  fireEvent(window,new Event('offline'))
  expect(screen.getByRole('status').textContent).toBe('Offline. Changes will sync.')
  online.mockReturnValue(true)
  fireEvent(window,new Event('online'))
  expect(screen.queryByRole('status')).toBeNull()
})
it('renders the cached text as inert text while the browser reports offline',async () => {
  vi.spyOn(navigator,'onLine','get').mockReturnValue(false)
  vi.mocked(request).mockResolvedValue({entry_id:42,text:'<script>steal()</script> https://publisher.test',html:'<img src="https://publisher.test/tracking">'})
  render(<QueryClientProvider client={new QueryClient()}><OfflineArticle entryID={42}/></QueryClientProvider>)
  expect(await screen.findByText('<script>steal()</script> https://publisher.test')).toBeTruthy()
  expect(document.querySelector('.offline-article img, .offline-article script, .offline-article a')).toBeNull()
  expect(request).toHaveBeenCalledWith('/v1/entries/42/content',expect.anything())
})
it('registers a root module worker without replacing an existing push worker',async () => {
  const register = vi.fn()
  const getRegistration = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator,'serviceWorker',{configurable:true,value:{getRegistration,register}})
  await registerOffline()
  expect(register).toHaveBeenCalledWith('/offline-worker.js',{scope:'/',type:'module'})
  getRegistration.mockResolvedValue({active:{scriptURL:'https://reader.test/push-worker.js'}})
  await expect(registerOffline()).rejects.toThrow('push registration was preserved')
  expect(register).toHaveBeenCalledTimes(1)
  delete (navigator as unknown as {serviceWorker?: unknown}).serviceWorker
})
