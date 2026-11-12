// Refine the generated nullable arrays after normalizing a stored response.
import type {components} from './api.generated'
import type {Entry, EntryState} from './reader'

export type FrontPeriod = 'daily' | 'weekly'
export type FrontClaim = components['schemas']['GroundedClaim']
export type FrontSummary = components['schemas']['FrontPageSummary']
export type FrontCoverage = components['schemas']['FrontPageCoverage']
export type FrontStory = Omit<components['schemas']['FrontPageStory'], 'coverage'> & {coverage: FrontCoverage[]}
export type FrontSection = Omit<components['schemas']['FrontPageSection'], 'stories'> & {stories: FrontStory[]}
export type FrontPage = Omit<components['schemas']['FrontPage'], 'period' | 'sections'> & {period: FrontPeriod; sections: FrontSection[]}

export const FRONT_WINDOWS: {period: FrontPeriod; name: string; label: string}[] = [
  {period: 'daily', name: 'Today', label: 'Past 24 hours'},
  {period: 'weekly', name: 'This week', label: 'Past 7 days'},
]

export function frontWindow(period: FrontPeriod): string {
  return FRONT_WINDOWS.find(window => window.period === period)?.label ?? 'Past 24 hours'
}

// A story is read only when every article in its coverage has been seen or
// marked Done. A partially read story stays unread.
export function storyRead(story: FrontStory, state: (entry: Entry) => EntryState): boolean {
  const coverage = story.coverage ?? []
  return coverage.length > 0 && coverage.every(item => {
    const current = state(item.entry)
    return current.seen.value === 'seen' || current.shelf.value === 'done'
  })
}

export function normalizeFrontPage(raw: unknown): FrontPage | null {
  if (!raw || typeof raw !== 'object') return null
  const value = raw as Partial<FrontPage>
  if (typeof value.revision !== 'string' || !Array.isArray(value.sections)) return null
  const period: FrontPeriod = value.period === 'weekly' ? 'weekly' : 'daily'
  return {
    period,
    window_start: value.window_start ?? value.selected_at ?? new Date(0).toISOString(),
    window_end: value.window_end ?? value.selected_at ?? new Date(0).toISOString(),
    selected_at: value.selected_at ?? value.window_end ?? new Date(0).toISOString(),
    revision: value.revision,
    sections: value.sections.map(section => ({
      name: section.name ?? '',
      stories: Array.isArray(section.stories) ? section.stories : [],
    })),
  }
}
