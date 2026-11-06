// Calm empty-state lines, rotated by day so the river does not repeat itself
// verbatim on every visit. The first line is the original.
export const EMPTY_STATES = [
  'Nothing new. A rare and beautiful thing.',
  'The river is calm. Enjoy the quiet.',
  'All caught up. The next wave will come.',
  'Nothing new since your last visit.',
] as const

const DAY_MS = 86_400_000

export function emptyCopy(now: Date = new Date()): string {
  const day = Math.floor(now.getTime() / DAY_MS)
  const index = ((day % EMPTY_STATES.length) + EMPTY_STATES.length) % EMPTY_STATES.length
  return EMPTY_STATES[index]
}
