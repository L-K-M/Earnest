import {describe, expect, it, vi} from 'vitest'
import {SEPARATOR, type MenuAction, type MenuItem} from './ContextMenu'
import {articleMenu, listMenu, markUpToHere, type ListMenuServices} from './menus'
import type {Entry, EntryState, Subscription} from './reader'

const subscriptions = [
  {id: 8, cadence: 'today', deleted: false},
  {id: 9, cadence: 'week', deleted: false},
  {id: 10, cadence: 'now', deleted: false},
] as Subscription[]

function services(overrides: Partial<ListMenuServices> = {}): ListMenuServices {
  return {newest: () => 120, markRange: vi.fn().mockResolvedValue(undefined), confirm: vi.fn(() => true), notify: vi.fn(), go: vi.fn(), openTab: vi.fn(), refreshFeed: vi.fn().mockResolvedValue(undefined), ...overrides}
}
const ids = (items: MenuItem[]) => items.map(item => item === SEPARATOR ? '-' : item.id)
const item = (items: MenuItem[], id: string) => items.find((value): value is MenuAction => value !== SEPARATOR && value.id === id)!

describe('listMenu', () => {
  it('offers a feed its bulk, refresh and settings actions', () => {
    expect(ids(listMenu({kind: 'feed', id: 8, title: 'Rust'}, subscriptions, services()))).toEqual(['mark-all-read', '-', 'refresh-feed', 'feed-settings', 'open-tab'])
  })

  it('marks a feed read through the newest article known when it runs', async () => {
    let newest = 120
    const deps = services({newest: () => newest})
    const items = listMenu({kind: 'feed', id: 8, title: 'Rust'}, subscriptions, deps)
    newest = 130
    await item(items, 'mark-all-read').run()
    expect(deps.confirm).toHaveBeenCalledWith('Mark every article in Rust read? Articles that arrive later stay unread.')
    expect(deps.markRange).toHaveBeenCalledWith({kind: 'subscriptions', ids: [8]}, 130)
    expect(deps.notify).toHaveBeenCalledWith('Marked everything in Rust read.')
  })

  it('does nothing when the confirmation is cancelled', async () => {
    const deps = services({confirm: vi.fn(() => false)})
    await item(listMenu({kind: 'folder', id: 3, title: 'News'}, subscriptions, deps), 'mark-all-read').run()
    expect(deps.markRange).not.toHaveBeenCalled()
    expect(deps.notify).not.toHaveBeenCalled()
  })

  it('scopes Today to the feeds it shows and a folder to itself', async () => {
    const deps = services()
    await item(listMenu({kind: 'today', title: 'Today'}, subscriptions, deps), 'mark-all-read').run()
    await item(listMenu({kind: 'folder', id: 3, title: 'News'}, subscriptions, deps), 'mark-all-read').run()
    expect(vi.mocked(deps.markRange).mock.calls).toEqual([[{kind: 'subscriptions', ids: [8, 10]}, 120], [{kind: 'folder', ids: [3]}, 120]])
    expect(ids(listMenu({kind: 'folder', id: 3, title: 'News'}, subscriptions, deps))).toEqual(['mark-all-read', '-', 'open-tab'])
  })

  it('cannot mark before this device knows any article', () => {
    expect(item(listMenu({kind: 'feed', id: 8, title: 'Rust'}, subscriptions, services({newest: () => 0})), 'mark-all-read').disabled).toBe(true)
  })

  it('offers no bulk action where a list has no range', () => {
    expect(ids(listMenu({kind: 'later', title: 'Later'}, subscriptions, services()))).toEqual(['open-tab'])
  })

  it('refreshes the feed, opens its settings and opens a new tab', async () => {
    const deps = services()
    const items = listMenu({kind: 'feed', id: 8, title: 'Rust'}, subscriptions, deps)
    await item(items, 'refresh-feed').run()
    await item(items, 'feed-settings').run()
    await item(items, 'open-tab').run()
    expect(deps.refreshFeed).toHaveBeenCalledWith(8)
    expect(deps.notify).toHaveBeenCalledWith('Refresh requested.')
    expect(deps.go).toHaveBeenCalledWith('/feeds#subscription-8')
    expect(deps.openTab).toHaveBeenCalledWith('/feeds/8')
  })
})

function state(seen: string, shelf: string, kept: boolean): EntryState {
  return {seen: {value: seen}, shelf: {value: shelf}, kept: {value: kept}} as EntryState
}

describe('articleMenu', () => {
  const deps = () => ({open: vi.fn(), openTab: vi.fn(), openOriginal: vi.fn(), act: vi.fn().mockResolvedValue(undefined)})

  it('names what each toggle will do to an unread article', async () => {
    const d = deps()
    const items = articleMenu(state('unseen', 'none', false), d)
    expect(items.map(value => value === SEPARATOR ? '-' : value.label)).toEqual(['Open', 'Open in new tab', 'Open original', '-', 'Done', 'Later', 'Keep', 'Mark read'])
    for (const id of ['done', 'later', 'keep', 'read']) await item(items, id).run()
    expect(d.act.mock.calls).toEqual([['shelf', 'done'], ['shelf', 'later'], ['kept', true], ['seen', 'seen']])
  })

  it('offers to undo each state an article already has', async () => {
    const d = deps()
    const later = articleMenu(state('seen', 'later', true), d)
    expect(['later', 'keep', 'read'].map(id => item(later, id).label)).toEqual(['Remove from Later', 'Stop keeping', 'Mark unread'])
    const done = articleMenu(state('seen', 'done', false), d)
    expect(item(done, 'done').label).toBe('Remove from Done')
    for (const id of ['later', 'keep', 'read']) await item(later, id).run()
    await item(done, 'done').run()
    expect(d.act.mock.calls).toEqual([['shelf', 'none'], ['kept', false], ['seen', 'unseen'], ['shelf', 'none']])
  })

  it('treats a Done article as read', () => {
    expect(item(articleMenu(state('unseen', 'done', false), deps()), 'read').label).toBe('Mark unread')
  })
})

describe('markUpToHere', () => {
  const entry = {id: 4, seq: 77} as Entry

  it('marks a list read through the chosen article', async () => {
    const deps = {markRange: vi.fn().mockResolvedValue(undefined), confirm: vi.fn(() => true), notify: vi.fn()}
    await markUpToHere(entry, {kind: 'feed', id: 8, title: 'Rust'}, subscriptions, deps)!.run()
    expect(deps.confirm).toHaveBeenCalledWith('Mark this article and everything before it in Rust read?')
    expect(deps.markRange).toHaveBeenCalledWith({kind: 'subscriptions', ids: [8]}, 77)
  })

  it('is not offered where a list has no range', () => {
    const deps = {markRange: vi.fn(), confirm: vi.fn(), notify: vi.fn()}
    expect(markUpToHere(entry, {kind: 'kept', title: 'Kept'}, subscriptions, deps)).toBeNull()
    expect(markUpToHere(entry, {kind: 'edition', title: 'Edition'}, subscriptions, deps)).toBeNull()
  })
})
