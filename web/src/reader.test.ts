// @vitest-environment jsdom
import {describe, expect, it} from 'vitest'
import {entryQuery, orderEntries, readerURL, shortcutsAllowed, viewScope, type Entry, type Subscription, type View} from './reader'
describe('reader rules', () => {
  it.each(['input', 'textarea', 'select', 'div[contenteditable]', 'div[role=textbox]'])('protects typing in %s', selector => {
    const element = document.createElement(selector.split('[')[0])
    if (selector.includes('contenteditable')) element.setAttribute('contenteditable', 'true')
    if (selector.includes('role=')) element.setAttribute('role', 'textbox')
    expect(shortcutsAllowed(element, true)).toBe(false)
  })
  it('respects shortcut opt-out and nested editable targets', () => {
    const parent = document.createElement('div'); parent.contentEditable = 'true'; parent.setAttribute('contenteditable', 'true')
    const child = parent.appendChild(document.createElement('span'))
    expect(shortcutsAllowed(child, true)).toBe(false)
    expect(shortcutsAllowed(document.body, false)).toBe(false)
    expect(shortcutsAllowed(document.body, true)).toBe(true)
  })
  it.each([['feed','subscription_id'], ['folder','folder_id'], ['label','label_id']] as const)('filters %s by ID', (kind, parameter) => {
    expect(entryQuery({kind, id: 12, title: 'A'}).get(parameter)).toBe('12')
  })
  it('groups Edition by stable subscription and seq, not publisher time', () => {
    const entries = [{id: 1, subscription_id: 2, seq: 1}, {id: 2, subscription_id: 1, seq: 3}, {id: 3, subscription_id: 1, seq: 2}] as Entry[]
    expect(orderEntries(entries, {kind: 'edition', title: 'Edition'}, []).map(entry => entry.id)).toEqual([3,2,1])
    const today: View = {kind: 'today', title: 'Today'}
    expect(orderEntries(entries, today, [{id: 2, cadence: 'week'} as Subscription]).map(entry => entry.id)).toEqual([3,2])
  })
  it('keeps signed content capabilities while applying preferences', () => {
    const url = new URL(readerURL('https://content.example/content/1?sig=secret', {theme:'system',measure:64,font:'sans'}, true))
    expect(url.searchParams.get('sig')).toBe('secret')
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('measure')).toBe('64')
  })
  it('maps views to sync range scopes', () => {
    expect(viewScope({kind:'feed', id:7, title:'F'})).toEqual({kind:'subscriptions', ids:[7]})
    expect(viewScope({kind:'folder', id:3, title:'G'})).toEqual({kind:'folder', ids:[3]})
    expect(viewScope({kind:'today', title:'Today'})).toEqual({kind:'all', ids:[]})
    expect(viewScope({kind:'label', id:2, title:'L'})).toEqual({kind:'all', ids:[]})
  })
})
