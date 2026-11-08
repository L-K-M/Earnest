import {useState} from 'react'
import type {components} from './api.generated'
import {Brand} from './Brand'
import {explainHealth, groupByFolder} from './feeds'
import {Icon, type IconName} from './Icon'
import type {Subscription} from './reader'
import {Link} from './router'
import {routeSection, viewHref, type Route, type ViewRef} from './routes'
import type {UnreadCounts} from './counts'
import type {SavedSearch} from './search/Search'

type Folder = components['schemas']['Folder']
type Label = components['schemas']['Label']

const COLLAPSED_KEY = 'earnest.sidebar.collapsed'
export const READING_VIEWS: {kind: ViewRef['kind']; title: string; icon: IconName; shortcut: string}[] = [
  {kind:'today', title:'Today', icon:'today', shortcut:'g t'},
  {kind:'edition', title:'Edition', icon:'edition', shortcut:'g e'},
  {kind:'later', title:'Later', icon:'later', shortcut:'g l'},
  {kind:'kept', title:'Kept', icon:'keep', shortcut:'g k'},
  {kind:'done', title:'Done', icon:'done', shortcut:'g d'},
]

function readCollapsed(): Set<number> {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '[]')
    return new Set(Array.isArray(value) ? value.filter((id): id is number => Number.isSafeInteger(id)) : [])
  } catch {
    return new Set()
  }
}

// A letter mark for a feed. The hue comes from the title so a feed keeps the
// same mark everywhere; the letter carries the meaning, not the colour.
export function FeedMark({title}: {title: string}) {
  const letter = Array.from(title.trim())[0]?.toLocaleUpperCase() ?? '?'
  let hash = 0
  for (const char of title) hash = (hash * 31 + (char.codePointAt(0) ?? 0)) % 360
  return <span className="feed-mark" aria-hidden="true" style={{'--hue': hash} as React.CSSProperties}>{letter}</span>
}

function Count({value}: {value?: number}) {
  if (!value) return null
  return <span className="nav-count"><span className="sr-only">, </span>{value > 999 ? '999+' : value}<span className="sr-only"> unread</span></span>
}

type Props = {
  route: Route
  subscriptions: Subscription[]
  folders: Folder[]
  labels: Label[]
  savedSearches: SavedSearch[]
  counts?: UnreadCounts
  askEnabled: boolean
  sync: {text: string; tone: 'quiet' | 'waiting' | 'problem'}
  onNavigate?: () => void
  onPalette: () => void
  onHelp: () => void
  onSignOut: () => void
  prefetch: (screen: 'feeds' | 'settings' | 'rules') => void
}

export function Sidebar({route, subscriptions, folders, labels, savedSearches, counts, askEnabled, sync, onNavigate, onPalette, onHelp, onSignOut, prefetch}: Props) {
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const section = routeSection(route)
  const current = (key: string) => section === key ? 'page' as const : undefined
  function toggle(id: number) {
    const next = new Set(collapsed)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setCollapsed(next)
    try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next])) } catch { /* Collapsing still works for this visit. */ }
  }
  const feedCount = (sub: Subscription) => counts?.bySubscription[String(sub.id)]
  const groups = groupByFolder(subscriptions, folders)
  const filed = groups.filter(group => group.folder !== null)
  const unfiled = groups.find(group => group.folder === null)?.subscriptions ?? []
  const nav = (props: {to: string; icon?: IconName; label: string; current?: 'page'; count?: number; intent?: () => void; mark?: string; warning?: string | null}) =>
    <Link to={props.to} className="nav-link" aria-current={props.current} onClick={onNavigate} onIntent={props.intent}>
      {props.mark !== undefined ? <FeedMark title={props.mark} /> : props.icon && <Icon name={props.icon} />}
      <span className="nav-label">{props.label}</span>
      {props.warning && <span className="nav-warning" title={props.warning}><Icon name="warning" size={14} /><span className="sr-only">, failing: {props.warning}</span></span>}
      {props.count ? ' ' : null}<Count value={props.count} />
    </Link>
  const feedLink = (sub: Subscription) => <li key={sub.id}>{nav({to:viewHref({kind:'feed', id:sub.id}), label:sub.title || sub.url, mark:sub.title || sub.url, current:current(`river:feed:${sub.id}`), count:feedCount(sub), warning:explainHealth(sub.health)})}</li>
  const activeLabels = labels.filter(label => !label.deleted)

  return <nav className="sidebar" aria-label="Earnest">
    <div className="sidebar-head">
      <Brand />
      <div className="sidebar-quick">
        <Link to="/search" className="quick-search" aria-current={current('search')} onClick={onNavigate}><Icon name="search" /><span>Search</span><kbd aria-hidden="true">/</kbd></Link>
        <button type="button" className="icon-button" onClick={onPalette} title="Command palette (Ctrl+K / ⌘K)" aria-keyshortcuts="Control+K Meta+K"><Icon name="command" /><span className="sr-only">Command palette</span></button>
      </div>
    </div>
    <div className="sidebar-body">
      <div role="group" aria-labelledby="nav-reading">
        <h2 id="nav-reading" className="nav-heading sr-only">Reading</h2>
        <ul className="nav-list">{READING_VIEWS.map(item => <li key={item.kind}>{nav({to:viewHref({kind:item.kind}), icon:item.icon, label:item.title, current:current(`river:${item.kind}:`), count:item.kind === 'later' ? counts?.later : item.kind === 'kept' ? counts?.kept : undefined})}</li>)}</ul>
      </div>
      {subscriptions.some(sub => !sub.deleted) && <div role="group" aria-labelledby="nav-feeds">
        <h2 id="nav-feeds" className="nav-heading">Feeds</h2>
        <ul className="nav-list">
          {filed.map(({folder, subscriptions: members}) => {
            if (!folder) return null
            const open = !collapsed.has(folder.id)
            const total = counts ? members.reduce((sum, sub) => sum + (feedCount(sub) ?? 0), 0) : undefined
            return <li key={`folder-${folder.id}`} className="nav-folder">
              <div className="nav-folder-row">
                <button type="button" className="disclosure" aria-expanded={open} onClick={() => toggle(folder.id)}><Icon name="chevron" size={14} /><span className="sr-only">{open ? 'Collapse' : 'Expand'} {folder.name}</span></button>
                {nav({to:viewHref({kind:'folder', id:folder.id}), icon:'folder', label:folder.name, current:current(`river:folder:${folder.id}`), count:total})}
              </div>
              {open && members.length > 0 && <ul className="nav-list nested">{members.map(feedLink)}</ul>}
            </li>
          })}
          {unfiled.map(feedLink)}
        </ul>
      </div>}
      {activeLabels.length > 0 && <div role="group" aria-labelledby="nav-labels">
        <h2 id="nav-labels" className="nav-heading">Labels</h2>
        <ul className="nav-list">{activeLabels.map(label => <li key={label.id}>{nav({to:viewHref({kind:'label', id:label.id}), icon:'tag', label:label.name, current:current(`river:label:${label.id}`)})}</li>)}</ul>
      </div>}
      {savedSearches.length > 0 && <div role="group" aria-labelledby="nav-saved">
        <h2 id="nav-saved" className="nav-heading">Saved searches</h2>
        <ul className="nav-list">{savedSearches.map(saved => <li key={saved.id}>{nav({to:`/search/saved/${saved.id}`, icon:'saved', label:saved.name, current:current(`saved:${saved.id}`)})}</li>)}</ul>
      </div>}
      <div role="group" aria-labelledby="nav-tools">
        <h2 id="nav-tools" className="nav-heading">Tools</h2>
        <ul className="nav-list">
          {askEnabled && <li>{nav({to:'/ask', icon:'ask', label:'Ask my feeds', current:current('ask')})}</li>}
          <li>{nav({to:'/rules', icon:'rules', label:'Rules', current:current('rules'), intent:() => prefetch('rules')})}</li>
          <li>{nav({to:'/activity', icon:'activity', label:'Activity', current:current('activity')})}</li>
          <li>{nav({to:'/notifications', icon:'bell', label:'Notifications', current:current('notifications')})}</li>
        </ul>
      </div>
    </div>
    <div className="sidebar-foot">
      <ul className="nav-list">
        <li>{nav({to:'/feeds', icon:'feeds', label:'Manage feeds', current:current('feeds'), intent:() => prefetch('feeds')})}</li>
        <li>{nav({to:'/settings', icon:'settings', label:'Settings', current:current('settings'), intent:() => prefetch('settings')})}</li>
        <li><button type="button" className="nav-link" onClick={onHelp} aria-keyshortcuts="Shift+?"><Icon name="keyboard" /><span className="nav-label">Keyboard shortcuts</span></button></li>
        <li><button type="button" className="nav-link" onClick={onSignOut}><Icon name="exit" /><span className="nav-label">Sign out</span></button></li>
      </ul>
      <p className={`sync-indicator ${sync.tone}`} title={sync.text}><Icon name={sync.tone === 'problem' ? 'warning' : sync.tone === 'waiting' ? 'sync' : 'check'} size={14} /><span>{sync.text}</span></p>
    </div>
  </nav>
}
