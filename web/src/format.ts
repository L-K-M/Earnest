// Human dates for lists and article headers. Lists use a compact age so a
// row stays one line; the full local date and time is always available in
// the <time> element's title and datetime attributes.
const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

export function shortAge(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return ''
  const at = new Date(iso)
  const time = at.getTime()
  if (Number.isNaN(time)) return ''
  const age = now.getTime() - time
  if (age < MINUTE) return 'now'
  if (age < HOUR) return `${Math.floor(age / MINUTE)} min`
  if (age < DAY) return `${Math.floor(age / HOUR)} h`
  if (age < 7 * DAY) return at.toLocaleDateString(undefined, {weekday: 'short'})
  return at.toLocaleDateString(undefined, at.getFullYear() === now.getFullYear() ? {month: 'short', day: 'numeric'} : {year: 'numeric', month: 'short', day: 'numeric'})
}

export function longDate(iso: string | undefined): string {
  if (!iso) return ''
  const at = new Date(iso)
  return Number.isNaN(at.getTime()) ? '' : at.toLocaleString(undefined, {dateStyle: 'medium', timeStyle: 'short'})
}
