// @vitest-environment jsdom
import {describe, expect, it} from 'vitest'
import {entryQuery, orderEntries, readerURL, retainUnreadEntries, shortcutsAllowed, viewScope, type Entry, type Subscription, type View} from './reader'
describe('reader rules', () => {
  it.each(['input', 'textarea', 'select', 'div[contenteditable]', 'div[role=textbox]'])('protects typing in %s', selector => {
    const element = document.createElement(selector.split('[')[0])
    if (selector.includes('contenteditable')) element.setAttribute('contenteditable', 'true')
    if (selector.includes('role=')) element.setAttribute('role', 'textbox')
    expect(shortcutsAllowed(element, true)).toBe(false)
  })
  it('leaves keys pressed in an open menu to the menu', () => {
    const menu = document.createElement('div'); menu.setAttribute('role', 'menu')
    const item = menu.appendChild(document.createElement('button'))
    expect(shortcutsAllowed(item, true)).toBe(false)
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
  it.each(['today', 'edition', 'all', 'feed', 'folder', 'label', 'later', 'kept', 'done'] as const)('requests the newest %s entries first', kind => {
    const query = entryQuery({kind, id: 12, title: 'A'})
    expect(query.get('direction')).toBe('backward')
    expect(query.has('after_seq')).toBe(false)
  })
  it('requests All articles with the existing view=all newest-first query', () => {
    const query = entryQuery({kind: 'all', title: 'All articles'})
    expect(query.get('view')).toBe('all')
    expect(query.get('limit')).toBe('100')
    expect(query.get('direction')).toBe('backward')
    expect(query.has('subscription_id')).toBe(false)
    expect(query.has('folder_id')).toBe(false)
    expect(query.has('label_id')).toBe(false)
  })
  it('orders articles newest first by ingest sequence, not publisher time', () => {
    const entries = [{id: 1, seq: 1, published_at: '2030-01-01'}, {id: 3, seq: 3, published_at: '2020-01-01'}, {id: 2, seq: 2}] as Entry[]
    expect(orderEntries(entries, {kind: 'folder', id: 1, title: 'Folder'}, []).map(entry => entry.id)).toEqual([3,2,1])
  })
  it('groups Edition by stable subscription with newest entries first', () => {
    const entries = [{id: 1, subscription_id: 2, seq: 1}, {id: 2, subscription_id: 1, seq: 3}, {id: 3, subscription_id: 1, seq: 2}] as Entry[]
    expect(orderEntries(entries, {kind: 'edition', title: 'Edition'}, []).map(entry => entry.id)).toEqual([2,3,1])
    const today: View = {kind: 'today', title: 'Today'}
    expect(orderEntries(entries, today, [{id: 2, cadence: 'week'} as Subscription]).map(entry => entry.id)).toEqual([2,3])
  })
  it('retains admitted rows but honors an explicit deletion without mutating earlier snapshots', () => {
    const entry = {id: 1, state: {seen: {value: 'unseen'}, shelf: {value: 'none'}}} as Entry
    const previous = retainUnreadEntries([], [entry], value => value.state)
    const read = {...entry, state: {...entry.state, seen: {...entry.state.seen, value: 'seen'}}} as Entry
    expect(retainUnreadEntries(previous, [], () => read.state)).toBe(previous)
    const updated = retainUnreadEntries(previous, [read], value => value.state)
    expect(updated).toEqual([read])
    expect(previous).toEqual([entry])
    expect(retainUnreadEntries(updated, [{...read, deleted: true}], value => value.state)).toEqual([])
    expect(retainUnreadEntries(updated, [], value => value.state, () => true)).toEqual([])
  })
  it('keeps signed content capabilities while applying preferences', () => {
    const url = new URL(readerURL('https://content.example/content/1?sig=secret', {theme:'system',measure:64,font:'sans'}, true))
    expect(url.searchParams.get('sig')).toBe('secret')
    expect(url.searchParams.get('theme')).toBe('dark')
    expect(url.searchParams.get('measure')).toBe('64')
  })
  it('maps views to sync range scopes', () => {
    const subs = [{id:1, cadence:'today'}, {id:2, cadence:'week'}, {id:3, cadence:'now'}, {id:4, cadence:'today', deleted:true}, {id:5, cadence:'today', muted:true}] as unknown as Subscription[]
    expect(viewScope({kind:'feed', id:7, title:'F'}, subs)).toEqual({kind:'subscriptions', ids:[7]})
    expect(viewScope({kind:'folder', id:3, title:'G'}, subs)).toEqual({kind:'folder', ids:[3]})
    // Today covers what it shows: not This week's feeds, not deleted ones, not
    // muted ones, which the river hides.
    expect(viewScope({kind:'today', title:'Today'}, subs)).toEqual({kind:'subscriptions', ids:[1, 3]})
    expect(viewScope({kind:'today', title:'Today'}, [])).toBeNull()
    // A muted feed still covers itself when you ask for it by name.
    expect(viewScope({kind:'feed', id:5, title:'M'}, subs)).toEqual({kind:'subscriptions', ids:[5]})
    expect(viewScope({kind:'label', id:2, title:'L'}, subs)).toBeNull()
    expect(viewScope({kind:'later', title:'Later'}, subs)).toBeNull()
  })
})
