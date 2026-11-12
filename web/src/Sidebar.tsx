import {useRef, useState, type DragEvent, type ReactNode, type MouseEvent as ReactMouseEvent} from 'react'
import type {components} from './api.generated'
import {canMove, SIDEBAR_DRAG_TYPE, type SidebarItem} from './folderMoves'
import {Brand} from './Brand'
import {MENU_ATTRIBUTE} from './ContextMenu'
import {FeedIcon} from './EntryMedia'
import {explainHealth, groupByFolder} from './feeds'
import {Icon, type IconName} from './Icon'
import type {Subscription, View} from './reader'
import {Link} from './router'
import {routeSection, viewHref, type Route, type ViewRef} from './routes'
import type {UnreadCounts} from './counts'
import type {SavedSearch} from './search/Search'

type Folder = components['schemas']['Folder']
type Label = components['schemas']['Label']

type Tone = 'orange' | 'amber' | 'plum' | 'purple' | 'blue' | 'green' | 'brand'

const COLLAPSED_KEY = 'earnest.sidebar.collapsed'
// Reading destinations. Today and Edition are gone as screens: their old
// meanings live on as the two front-page windows and as legacy URL aliases.
export const READING_VIEWS: {id: string; kind: ViewRef['kind']; title: string; to: string; section: string; icon: IconName; shortcut: string; tone: Tone}[] = [
  {id:'front', kind:'today', title:'Front page', to:'/front', section:'river:front', icon:'today', shortcut:'g t', tone:'orange'},
  {id:'all', kind:'all', title:'All articles', to:'/articles', section:'river:all:', icon:'stack', shortcut:'g a', tone:'blue'},
  {id:'later', kind:'later', title:'Later', to:'/later', section:'river:later:', icon:'later', shortcut:'g l', tone:'amber'},
  {id:'kept', kind:'kept', title:'Kept', to:'/kept', section:'river:kept:', icon:'keep', shortcut:'g k', tone:'plum'},
  {id:'done', kind:'done', title:'Done', to:'/done', section:'river:done:', icon:'done', shortcut:'g d', tone:'green'},
]

function readCollapsed(): Set<number> {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '[]')
    return new Set(Array.isArray(value) ? value.filter((id): id is number => Number.isSafeInteger(id)) : [])
  } catch {
    return new Set()
  }
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
  onMove?: (item: SidebarItem, parentID: number) => Promise<void>
  // Opens the actions for Today, a folder or a feed (a right click, a long
  // press, Shift+F10 or the Menu key).
  onMenu?: (event: ReactMouseEvent<HTMLElement>, view: View) => void
}

export function Sidebar({route, subscriptions, folders, labels, savedSearches, counts, askEnabled, sync, onNavigate, onPalette, onHelp, onSignOut, prefetch, onMenu, onMove}: Props) {
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const dragging = useRef<SidebarItem | null>(null)
  const moving = useRef(false)
  const [isDragging, setIsDragging] = useState(false)
  const [dropTarget, setDropTarget] = useState<number | null>(null)
  const [moveMessage, setMoveMessage] = useState('')
  const [pending, setPending] = useState(false)
  function endDrag() {
    dragging.current = null
    setIsDragging(false)
    setDropTarget(null)
  }
  function drag(item: SidebarItem) {
    return {
      draggable: !!onMove && !pending,
      onDragStart: (event: DragEvent<HTMLElement>) => {
        if (!onMove || moving.current) { event.preventDefault(); return }
        dragging.current = item
        setIsDragging(true)
        event.dataTransfer.effectAllowed = 'move'
        event.dataTransfer.setData(SIDEBAR_DRAG_TYPE, JSON.stringify(item))
        event.stopPropagation()
      },
      onDragEnd: endDrag,
    }
  }
  function target(parentID: number) {
    const accepts = () => !!onMove && !moving.current && !!dragging.current && canMove(dragging.current, parentID, subscriptions, folders)
    return {
      'data-drop-target': dropTarget === parentID || undefined,
      onDragOver: (event: DragEvent<HTMLElement>) => {
        event.stopPropagation()
        if (!accepts()) { event.dataTransfer.dropEffect = 'none'; setDropTarget(null); return }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        setDropTarget(parentID)
      },
      onDragLeave: (event: DragEvent<HTMLElement>) => {
        if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setDropTarget(null)
      },
      onDrop: (event: DragEvent<HTMLElement>) => {
        event.preventDefault()
        event.stopPropagation()
        const item = dragging.current
        const valid = accepts()
        endDrag()
        if (!valid || !item || !onMove) return
        moving.current = true
        setPending(true)
        setMoveMessage('Moving…')
        void onMove(item, parentID).then(() => {
          setCollapsed(previous => { const next = new Set(previous); next.delete(parentID); return next })
          setMoveMessage('Moved.')
        }).catch((error: unknown) => {
          setMoveMessage(`Could not move: ${error instanceof Error ? error.message : 'Please try again.'}`)
        }).finally(() => { moving.current = false; setPending(false) })
      },
    }
  }
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
  const nav = (props: {to: string; icon?: IconName; label: string; current?: 'page'; count?: number; intent?: () => void; feed?: Subscription; warning?: string | null; tone?: Tone; menu?: View; item?: SidebarItem}) => {
    const {menu} = props
    return <Link {...(props.item ? drag(props.item) : {})} to={props.to} className="nav-link" aria-current={props.current} data-tone={props.tone} onClick={onNavigate} onIntent={props.intent} onContextMenu={menu && onMenu ? event => onMenu(event, menu) : undefined} {...(menu && onMenu ? {[MENU_ATTRIBUTE]: ''} : {})}>
      {props.feed ? <FeedIcon subscription={props.feed} title={props.label} /> : props.icon && <Icon name={props.icon} />}
      <span className="nav-label">{props.label}</span>
      {props.warning && <span className="nav-warning" title={props.warning}><Icon name="warning" size={14} /><span className="sr-only">, failing: {props.warning}</span></span>}
      {props.count ? ' ' : null}<Count value={props.count} />
    </Link>
  }
  const feedLink = (sub: Subscription) => <li key={sub.id}>{nav({to:viewHref({kind:'feed', id:sub.id}), label:sub.title || sub.url, item:{kind:'feed', id:sub.id}, feed:sub, current:current(`river:feed:${sub.id}`), count:feedCount(sub), warning:explainHealth(sub.health), menu:{kind:'feed', id:sub.id, title:sub.title || sub.url}})}</li>
  const folderRows = (parentID: number): ReactNode => filed.filter(({folder}) => (folder?.parent_id || 0) === parentID).map(({folder, subscriptions: members}) => {
    if (!folder) return null
    const open = !collapsed.has(folder.id)
    const total = counts ? members.reduce((sum, sub) => sum + (feedCount(sub) ?? 0), 0) : undefined
    return <li key={`folder-${folder.id}`} className="nav-folder">
      <div className="nav-folder-row" {...target(folder.id)}>
        <button type="button" className="disclosure" aria-expanded={open} onClick={() => toggle(folder.id)}><Icon name="chevron" size={14} /><span className="sr-only">{open ? 'Collapse' : 'Expand'} {folder.name}</span></button>
        {nav({to:viewHref({kind:'folder', id:folder.id}), icon:'folder', label:folder.name, item:{kind:'folder', id:folder.id}, current:current(`river:folder:${folder.id}`), count:total, tone:'blue', menu:{kind:'folder', id:folder.id, title:folder.name}})}
      </div>
      {open && <ul className="nav-list nested">{folderRows(folder.id)}{members.map(feedLink)}</ul>}
    </li>
  })
  const activeLabels = labels.filter(label => !label.deleted)

  return <nav className="sidebar" aria-label="Earnest">
    <div className="sidebar-head">
      <Brand home onNavigate={onNavigate} />
      <div className="sidebar-quick">
        <Link to="/search" className="quick-search" aria-current={current('search')} onClick={onNavigate}><Icon name="search" /><span>Search</span><kbd aria-hidden="true">/</kbd></Link>
        <button type="button" className="icon-button" onClick={onPalette} title="Command palette (Ctrl+K / ⌘K)" aria-keyshortcuts="Control+K Meta+K"><Icon name="command" /><span className="sr-only">Command palette</span></button>
      </div>
    </div>
    <div className="sidebar-body">
      <div role="group" aria-labelledby="nav-reading">
        <h2 id="nav-reading" className="nav-heading sr-only">Reading</h2>
        <ul className="nav-list">{READING_VIEWS.map(item => <li key={item.id}>{nav({to:item.to, icon:item.icon, label:item.title, current:current(item.section), count:item.kind === 'later' ? counts?.later : item.kind === 'kept' ? counts?.kept : undefined, tone:item.tone, menu:item.kind === 'all' ? {kind:'all', title:item.title} : undefined})}</li>)}</ul>
      </div>
      {(subscriptions.some(sub => !sub.deleted) || filed.length > 0) && <div role="group" aria-labelledby="nav-feeds">
        <h2 id="nav-feeds" className="nav-heading" {...target(0)} title="Drop here to move to the top level">Feeds</h2>
        {moveMessage && <p className={moveMessage === 'Moved.' ? 'sr-only' : 'nav-move-status'} role="status">{moveMessage}</p>}
        <ul className="nav-list">
          {folderRows(0)}
          {unfiled.map(feedLink)}
        </ul>
        {isDragging && <div className="nav-root-drop" {...target(0)}>Drop here to move to the top level</div>}
      </div>}
      {activeLabels.length > 0 && <div role="group" aria-labelledby="nav-labels">
        <h2 id="nav-labels" className="nav-heading">Labels</h2>
        <ul className="nav-list">{activeLabels.map(label => <li key={label.id}>{nav({to:viewHref({kind:'label', id:label.id}), icon:'tag', label:label.name, current:current(`river:label:${label.id}`), tone:'purple'})}</li>)}</ul>
      </div>}
      {savedSearches.length > 0 && <div role="group" aria-labelledby="nav-saved">
        <h2 id="nav-saved" className="nav-heading">Saved searches</h2>
        <ul className="nav-list">{savedSearches.map(saved => <li key={saved.id}>{nav({to:`/search/saved/${saved.id}`, icon:'saved', label:saved.name, current:current(`saved:${saved.id}`), tone:'brand'})}</li>)}</ul>
      </div>}
      <div role="group" aria-labelledby="nav-tools">
        <h2 id="nav-tools" className="nav-heading">Tools</h2>
        <ul className="nav-list">
          {askEnabled && <li>{nav({to:'/ask', icon:'ask', label:'Ask my feeds', current:current('ask'), tone:'purple'})}</li>}
          <li>{nav({to:'/rules', icon:'rules', label:'Rules', current:current('rules'), intent:() => prefetch('rules'), tone:'blue'})}</li>
          <li>{nav({to:'/activity', icon:'activity', label:'Activity', current:current('activity'), tone:'plum'})}</li>
          <li>{nav({to:'/notifications', icon:'bell', label:'Notifications', current:current('notifications'), tone:'orange'})}</li>
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
