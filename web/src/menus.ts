import {SEPARATOR, tidy, type MenuAction, type MenuItem} from './ContextMenu'
import {viewScope, type Entry, type EntryState, type RangeScope, type Register, type Subscription, type Value, type View} from './reader'
import {viewHref} from './routes'

export type MarkRange = (scope: RangeScope, throughSeq: number) => Promise<void>

// The highest sequence in cached list pages (react-query's infinite data for
// each list). Sequences are global, so any loaded list bounds every scope.
export function newestListed(lists: unknown[]): number {
  let newest = 0
  for (const list of lists) {
    const pages = (list as {pages?: {entries?: {seq?: unknown}[]}[]} | undefined)?.pages ?? []
    for (const page of pages) {
      for (const entry of page.entries ?? []) {
        if (typeof entry.seq === 'number' && entry.seq > newest) newest = entry.seq
      }
    }
  }
  return newest
}

export type FolderMenuTarget = {id: number; title: string}

export type ListMenuServices = {
  // The newest article this device knows for the list, read when the item
  // runs: the boundary "Mark all as read" marks through.
  newest: () => number
  markRange: MarkRange
  confirm: (message: string) => boolean
  notify: (message: string) => void
  go: (to: string) => void
  openTab: (href: string) => void
  refreshFeed: (id: number) => Promise<void>
  renameFolder: (folder: FolderMenuTarget) => void
  newSubfolder: (parent: FolderMenuTarget) => void
  addFeed: (folder: FolderMenuTarget) => void
}

// Actions for All articles, a feed or a folder in the sidebar. "Mark all as
// read" covers what the list held when you chose it; articles that arrive
// later stay unread, as with the palette's "Mark the rest read".
export function listMenu(view: View, subscriptions: Subscription[], services: ListMenuServices): MenuItem[] {
  const scope = viewScope(view, subscriptions)
  const feedID = view.kind === 'feed' ? view.id : undefined
  const folder = view.kind === 'folder' && view.id !== undefined ? {id: view.id, title: view.title} : undefined
  return tidy([
    scope && {id: 'mark-all-read', label: 'Mark all as read', icon: 'check', disabled: services.newest() <= 0, run: async () => {
      if (!services.confirm(`Mark every article in ${view.title} read? Articles that arrive later stay unread.`)) return
      await services.markRange(scope, services.newest())
      services.notify(`Marked everything in ${view.title} read.`)
    }},
    SEPARATOR,
    folder !== undefined && {id: 'rename-folder', label: 'Rename folder', icon: 'folder', run: () => services.renameFolder({id: folder.id, title: folder.title})},
    folder !== undefined && {id: 'add-feed-in-folder', label: 'Add feed inside this folder', icon: 'feeds', run: () => services.addFeed({id: folder.id, title: folder.title})},
    folder !== undefined && {id: 'new-subfolder', label: 'New subfolder', icon: 'folder', run: () => services.newSubfolder({id: folder.id, title: folder.title})},
    feedID !== undefined && {id: 'refresh-feed', label: 'Refresh feed', icon: 'refresh', run: async () => {
      await services.refreshFeed(feedID)
      services.notify('Refresh requested.')
    }},
    feedID !== undefined && {id: 'feed-settings', label: 'Feed settings', icon: 'settings', run: () => services.go(`/feeds#subscription-${feedID}`)},
    {id: 'open-tab', label: 'Open in new tab', icon: 'external', run: () => services.openTab(viewHref({kind: view.kind, id: view.id}))},
  ])
}

export type ArticleMenuServices = {
  open: () => void
  openTab: () => void
  openOriginal: () => void
  act: (register: Register, value: Value) => Promise<void>
}

// Actions for one article row, matching its keyboard shortcuts. Each toggle
// names what it will do to this article. "Mark unread" emits only the seen
// write, like its "m" shortcut: the shared sync model then clears Done
// atomically from that deliberate unseen (see the "native-unread-done"
// vector), so a Done article becomes genuinely unread and the label flips
// with it. The label follows the row's display, where Done reads as read.
export function articleMenu(state: EntryState, services: ArticleMenuServices): MenuItem[] {
  const done = state.shelf.value === 'done'
  const later = state.shelf.value === 'later'
  const kept = state.kept.value === true
  const unread = state.seen.value !== 'seen' && !done
  return [
    {id: 'open', label: 'Open', hint: 'o', run: services.open},
    {id: 'open-tab', label: 'Open in new tab', run: services.openTab},
    {id: 'open-original', label: 'Open original', icon: 'external', hint: 'v', run: services.openOriginal},
    SEPARATOR,
    {id: 'done', label: done ? 'Remove from Done' : 'Done', icon: 'done', hint: done ? undefined : 'e', run: () => services.act('shelf', done ? 'none' : 'done')},
    {id: 'later', label: later ? 'Remove from Later' : 'Later', icon: 'later', hint: 's', run: () => services.act('shelf', later ? 'none' : 'later')},
    {id: 'keep', label: kept ? 'Stop keeping' : 'Keep', icon: 'keep', hint: 'f', run: () => services.act('kept', !kept)},
    {id: 'read', label: unread ? 'Mark read' : 'Mark unread', icon: 'unread', hint: unread ? undefined : 'm', run: () => services.act('seen', unread ? 'seen' : 'unseen')},
  ]
}

// Marks a list read from its start through one article, for clearing what
// you have skimmed past. Only lists with a range scope offer it.
export function markUpToHere(entry: Entry, view: View, subscriptions: Subscription[], services: {markRange: MarkRange; confirm: (message: string) => boolean; notify: (message: string) => void}): MenuAction | null {
  const scope = viewScope(view, subscriptions)
  if (!scope) return null
  return {id: 'mark-up-to-here', label: 'Mark read up to here', icon: 'check', run: async () => {
    if (!services.confirm(`Mark this article and everything before it in ${view.title} read?`)) return
    await services.markRange(scope, entry.seq)
    services.notify(`Marked ${view.title} read up to this article.`)
  }}
}
