// @vitest-environment jsdom
import {afterEach, describe, expect, it} from 'vitest'
import type {FrontPage} from './front'
import {clearFrontCache, clearFrontCacheForUser, frontCacheKey, loadFrontPage, saveFrontPage} from './front-store'

const page: FrontPage = {
  period: 'daily',
  window_start: '2026-11-11T10:00:00Z',
  window_end: '2026-11-12T10:00:00Z',
  selected_at: '2026-11-12T10:00:00Z',
  revision: 'rev-1',
  sections: [],
}
afterEach(() => localStorage.clear())

describe('front page cache', () => {
  it('keys the last successful page by account and period', () => {
    saveFrontPage(1, 'daily', page, '2026-11-12T10:00:00Z')
    expect(frontCacheKey(1, 'daily')).toBe('earnest.front.1.daily')
    expect(loadFrontPage(1, 'daily')).toEqual({page, updated_at: '2026-11-12T10:00:00Z'})
    expect(loadFrontPage(1, 'weekly')).toBeUndefined()
    expect(loadFrontPage(2, 'daily')).toBeUndefined()
  })

  it('ignores a malformed or partial cache entry', () => {
    localStorage.setItem(frontCacheKey(1, 'daily'), '{not json')
    expect(loadFrontPage(1, 'daily')).toBeUndefined()
    localStorage.setItem(frontCacheKey(1, 'daily'), JSON.stringify({page: {sections: null}, updated_at: 'x'}))
    expect(loadFrontPage(1, 'daily')).toBeUndefined()
  })

  it('clears one account or the whole private namespace on sign-out', () => {
    saveFrontPage(1, 'daily', page)
    saveFrontPage(1, 'weekly', {...page, period: 'weekly'})
    saveFrontPage(2, 'daily', page)
    clearFrontCacheForUser(1)
    expect(loadFrontPage(1, 'daily')).toBeUndefined()
    expect(loadFrontPage(1, 'weekly')).toBeUndefined()
    expect(loadFrontPage(2, 'daily')).toEqual({page, updated_at: expect.any(String)})
    clearFrontCache()
    expect(loadFrontPage(2, 'daily')).toBeUndefined()
  })
})
