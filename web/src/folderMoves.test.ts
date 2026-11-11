import {expect, it} from 'vitest'
import {canMove} from './folderMoves'
import type {Folder} from './feeds'
import type {Subscription} from './reader'

const folders = [
  {id: 1, name: 'Parent', parent_id: 0},
  {id: 2, name: 'Child', parent_id: 1},
  {id: 3, name: 'Grandchild', parent_id: 2},
  {id: 4, name: 'Other', parent_id: 0},
  {id: 5, name: 'Deleted', deleted: true, parent_id: 0, rev: 1},
] as Folder[]
const feeds = [{id: 10, folder_id: 1}, {id: 11, deleted: true, folder_id: 0}] as Subscription[]

it('allows feed moves between folders and back to root, but rejects invalid and unchanged targets', () => {
  for (const destination of [0, 2, 4]) expect(canMove({kind:'feed', id:10}, destination, feeds, folders)).toBe(true)
  for (const destination of [1, 5, 99]) expect(canMove({kind:'feed', id:10}, destination, feeds, folders)).toBe(false)
  for (const id of [11, 99]) expect(canMove({kind:'feed', id}, 1, feeds, folders)).toBe(false)
})

it('rejects folder self-drops, descendants, deleted destinations, and no-ops', () => {
  for (const destination of [0, 1, 2, 3, 5, 99]) expect(canMove({kind:'folder', id:1}, destination, feeds, folders)).toBe(false)
  expect(canMove({kind:'folder', id:1}, 4, feeds, folders)).toBe(true)
  expect(canMove({kind:'folder', id:2}, 0, feeds, folders)).toBe(true)
  expect(canMove({kind:'folder', id:5}, 1, feeds, folders)).toBe(false)
})
