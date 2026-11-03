import type {components} from './api.generated'
export type Entry = components['schemas']['Entry']
export type EntryState = components['schemas']['State']
export type Subscription = components['schemas']['Subscription']
export type Register = 'seen' | 'shelf' | 'kept'
export type Value = 'seen' | 'unseen' | 'none' | 'later' | 'done' | boolean
export type View = {kind: 'today' | 'edition' | 'later' | 'kept' | 'done' | 'feed' | 'folder' | 'label'; id?: number; title: string}
export type ReaderActions = {
  state: (entry: Entry) => EntryState
  act: (entry: Entry, register: Register, value: Value, kind?: 'explicit' | 'passive') => Promise<void>
}
export function viewKey(view: View): string { return `${view.kind}${view.id === undefined ? '' : `:${view.id}`}` }
export function entryQuery(view: View): URLSearchParams {
  const query = new URLSearchParams({view: view.kind === 'today' ? 'river' : ['later', 'kept', 'done'].includes(view.kind) ? view.kind : 'all', limit: '100', direction: 'forward'})
  if (view.kind === 'edition') {query.set('view', 'river');query.set('cadence', 'week')}
  if (view.kind === 'feed') query.set('subscription_id', String(view.id))
  if (view.kind === 'folder') query.set('folder_id', String(view.id))
  if (view.kind === 'label') query.set('label_id', String(view.id))
  return query
}
export function orderEntries(entries: Entry[], view: View, subscriptions: Subscription[]): Entry[] {
  const byID = new Map(subscriptions.map(sub => [sub.id, sub]))
  const unique = [...new Map(entries.map(entry => [entry.id, entry])).values()]
  return unique.filter(entry => !entry.deleted && (view.kind !== 'today' || byID.get(entry.subscription_id)?.cadence !== 'week'))
    .sort((a, b) => view.kind === 'edition' ? a.subscription_id - b.subscription_id || a.seq - b.seq : a.seq - b.seq)
}
export function shortcutsAllowed(target: EventTarget | null, enabled: boolean): boolean {
  if (!enabled || !(target instanceof Element)) return false
  return !target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], dialog[open]')
}
export function readerURL(raw: string, settings: {theme: string; measure: number; font: string}, dark: boolean): string {
  const url = new URL(raw)
  url.searchParams.set('theme', settings.theme === 'system' ? (dark ? 'dark' : 'light') : settings.theme)
  url.searchParams.set('measure', String(settings.measure))
  url.searchParams.set('font', settings.font)
  return url.href
}
