import type {components} from './api.generated'
export type Entry = components['schemas']['Entry']
export type EntryState = components['schemas']['State']
export type Subscription = components['schemas']['Subscription']
export type Register = 'seen' | 'shelf' | 'kept'
export type Value = 'seen' | 'unseen' | 'none' | 'later' | 'done' | boolean
export type View = {kind: 'today' | 'edition' | 'later' | 'kept' | 'done' | 'feed' | 'folder' | 'label'; id?: number; title: string}
export type ReaderActions = {
  deleted?: (entry: Entry) => boolean
  state: (entry: Entry) => EntryState
  act: (entry: Entry, register: Register, value: Value, kind?: 'explicit' | 'passive') => Promise<void>
}
export function viewKey(view: View): string { return `${view.kind}${view.id === undefined ? '' : `:${view.id}`}` }
export type RangeScope = {kind: 'subscriptions' | 'folder'; ids: number[]}

// The sync range "Mark the rest read" covers for a view. Today covers the
// subscriptions it shows: every cadence except This week, which belongs to the
// Edition, and no muted feed, since the river hides those. A feed or folder
// covers itself, muted or not. Other views offer no such command.
export function viewScope(view: View, subscriptions: Subscription[]): RangeScope | null {
  if (view.kind === 'feed' && view.id !== undefined) return {kind: 'subscriptions', ids: [view.id]}
  if (view.kind === 'folder' && view.id !== undefined) return {kind: 'folder', ids: [view.id]}
  if (view.kind !== 'today') return null
  const ids = subscriptions.filter(sub => !sub.deleted && !sub.muted && sub.cadence !== 'week').map(sub => sub.id)
  return ids.length ? {kind: 'subscriptions', ids} : null
}
export function entryQuery(view: View, group?: 'cluster'): URLSearchParams {
  const query = new URLSearchParams({view: view.kind === 'today' ? 'river' : ['later', 'kept', 'done'].includes(view.kind) ? view.kind : 'all', limit: '100', direction: 'backward'})
  if (view.kind === 'edition') {query.set('view', 'river');query.set('cadence', 'week')}
  if (view.kind === 'feed') query.set('subscription_id', String(view.id))
  if (view.kind === 'folder') query.set('folder_id', String(view.id))
  if (view.kind === 'label') query.set('label_id', String(view.id))
  if (group) query.set('group', group)
  return query
}
export function orderEntries(entries: Entry[], view: View, subscriptions: Subscription[]): Entry[] {
  const byID = new Map(subscriptions.map(sub => [sub.id, sub]))
  const unique = [...new Map(entries.map(entry => [entry.id, entry])).values()]
  return unique.filter(entry => !entry.deleted && (view.kind !== 'today' || byID.get(entry.subscription_id)?.cadence !== 'week'))
    .sort((a, b) => view.kind === 'edition' ? a.subscription_id - b.subscription_id || b.seq - a.seq : b.seq - a.seq)
}
// A visit admits unread rows once. Keep their metadata even if a background
// river response omits them after a read, so sync cannot move the list under
// the reader. Deletions still remove rows immediately; only an intentional
// parent-list reload clears the read-admission snapshot.
export function retainUnreadEntries(previous: Entry[], fresh: Entry[], state: ReaderActions['state'], deleted: (entry: Entry) => boolean = entry => entry.deleted): Entry[] {
  const admitted = new Map(previous.filter(entry => !deleted(entry)).map(entry => [entry.id, entry]))
  for (const entry of fresh) {
    if (deleted(entry)) {
      admitted.delete(entry.id)
      continue
    }
    const current = state(entry)
    if (admitted.has(entry.id) || (current.seen.value === 'unseen' && current.shelf.value !== 'done')) admitted.set(entry.id, entry)
  }
  const next = [...admitted.values()]
  return next.length === previous.length && next.every((entry, index) => entry === previous[index]) ? previous : next
}
export function shortcutsAllowed(target: EventTarget | null, enabled: boolean): boolean {
  if (!enabled || !(target instanceof Element)) return false
  // Keys pressed in an open menu belong to the menu.
  return !target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="menu"], dialog[open]')
}
export function readerURL(raw: string, settings: {theme: string; measure: number; font: string}, dark: boolean): string {
  const url = new URL(raw)
  url.searchParams.set('theme', settings.theme === 'system' ? (dark ? 'dark' : 'light') : settings.theme)
  url.searchParams.set('measure', String(settings.measure))
  url.searchParams.set('font', settings.font)
  return url.href
}
