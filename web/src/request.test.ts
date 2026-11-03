// @vitest-environment jsdom
import {afterEach, expect, it, vi} from 'vitest'
import {request, RequestError, setCSRF} from './request'
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); sessionStorage.clear() })
it('sends the matching CSRF token on mutations and preserves XML uploads', async () => {
  setCSRF('session-secret')
  const fetcher = vi.fn().mockResolvedValue(new Response('{}', {headers:{'Content-Type':'application/json'}}))
  vi.stubGlobal('fetch', fetcher)
  await request('/v1/opml:import', {method:'POST', body:'<opml/>', headers:{'Content-Type':'application/xml'}})
  const options = fetcher.mock.calls[0][1] as RequestInit
  expect(new Headers(options.headers).get('X-CSRF-Token')).toBe('session-secret')
  expect(new Headers(options.headers).get('Content-Type')).toBe('application/xml')
  expect(options.credentials).toBe('same-origin')
})
it('keeps status codes for rebootstrap and presents plain problem details', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"detail":"Cursor expired. Bootstrap again."}', {status:410, headers:{'Content-Type':'application/problem+json'}})))
  await expect(request('/v1/changes')).rejects.toEqual(new RequestError(410, 'Cursor expired. Bootstrap again.'))
})
it('refuses rounded identifiers before an action can target another item', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"id":9007199254740993}', {headers:{'Content-Type':'application/json'}})))
  await expect(request('/v1/entries')).rejects.toThrow('integer outside')
})
