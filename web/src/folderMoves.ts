import type {Folder} from './feeds'
import type {Subscription} from './reader'

export type SidebarItem = {kind: 'feed' | 'folder'; id: number}
export const SIDEBAR_DRAG_TYPE = 'application/x-earnest-sidebar-item'

export function canMove(item: SidebarItem, parentID: number, subscriptions: Subscription[], folders: Folder[]): boolean {
  const active = folders.filter(folder => !folder.deleted)
  if (parentID !== 0 && !active.some(folder => folder.id === parentID)) return false
  if (item.kind === 'feed') {
    const feed = subscriptions.find(sub => sub.id === item.id && !sub.deleted)
    return !!feed && feed.folder_id !== parentID
  }
  const folder = active.find(folder => folder.id === item.id)
  if (!folder || (folder.parent_id || 0) === parentID) return false
  // Walk the destination's ancestors, rejecting malformed cycles as well as
  // a move that would place the source inside its own subtree.
  const visited = new Set<number>([item.id])
  for (let id = parentID; id !== 0;) {
    if (visited.has(id)) return false
    visited.add(id)
    id = active.find(candidate => candidate.id === id)?.parent_id || 0
  }
  return true
}
