import {expect, it} from 'vitest'
import {emptyCopy, EMPTY_STATES} from './copy'

it('offers a calm empty state', () => {
  expect(EMPTY_STATES).toContain('Nothing new. A rare and beautiful thing.')
  expect(EMPTY_STATES).toContain(emptyCopy(new Date('2026-11-06T12:00:00Z')))
})

it('is stable within a day', () => {
  const morning = emptyCopy(new Date('2026-11-06T01:00:00Z'))
  const evening = emptyCopy(new Date('2026-11-06T23:00:00Z'))
  expect(morning).toBe(evening)
})

it('rotates across days and every message appears once per cycle', () => {
  const seen = new Set(Array.from({length: EMPTY_STATES.length}, (_, i) => emptyCopy(new Date(Date.UTC(2026, 10, 6 + i)))))
  expect(seen.size).toBe(EMPTY_STATES.length)
})
