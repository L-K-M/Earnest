import type {components} from './api.generated'
import {defaultSettings, settingsInput, type ReadingSettings} from './Settings'
import {request} from './request'

export type Subscription = components['schemas']['Subscription']
export type Folder = components['schemas']['Folder']
export type Mute = components['schemas']['Mute']
export type FeedHealth = components['schemas']['FeedHealth']
export type OPMLResult = components['schemas']['OPMLResult']
export type KeptImportReport = components['schemas']['KeptImportReport']
export type KeptImportResult = components['schemas']['KeptImportResult']
export type MissingFeeds = 'skip' | 'subscribe'
export type SubscribeBody = components['schemas']['SubscriptionBody']
export type SubscriptionPatch = components['schemas']['SubscriptionPatch']

export function newOp(): string { return crypto.randomUUID() }

export const CADENCES = [
  {value: 'now', label: 'Now', hint: 'Arrive in the river at once; rules may notify.'},
  {value: 'today', label: 'Today', hint: 'Arrive in the river, kept for the days you choose.'},
  {value: 'week', label: 'This week', hint: 'Arrives in the river and stays longer; the front page selects from every cadence.'},
] as const
export type Cadence = (typeof CADENCES)[number]['value']

// The server stores 'inherit', 'always' or 'never'; the API reads inherit back
// as an empty string and spells never as 'none'.
export const EXTRACT_MODES = [
  {value: '', label: 'Inherit the feed default'},
  {value: 'always', label: 'Always fetch full text'},
  {value: 'none', label: 'Never fetch full text'},
] as const

// Mirrors service.normalizedFeedURL so an invalid address fails before a fetch.
export function normalizeFeedURLInput(raw: string): string | null {
  const trimmed = raw.trim()
  if (!trimmed || trimmed.length > 8192) return null
  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return null
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
  if (!parsed.hostname || parsed.username || parsed.password) return null
  parsed.hash = ''
  return parsed.toString()
}

// One "Name: value" per line; returns null when a non-empty line is malformed.
export function parseCredentialHeaders(text: string): Record<string, string> | null {
  const headers: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const split = trimmed.indexOf(':')
    if (split <= 0) return null
    const name = trimmed.slice(0, split).trim()
    const value = trimmed.slice(split + 1).trim()
    if (!name || !value) return null
    headers[name] = value
  }
  return headers
}

// Plain-language explanations for each stored failure class, plus the outbound
// vocabulary that can appear while a class is still in transit.
const HEALTH_EXPLANATIONS: Record<string, string> = {
  dns: 'The address cannot be found. Check the feed URL for typos.',
  tls: 'The site certificate could not be verified.',
  network: 'The site cannot be reached.',
  connect: 'The site cannot be reached.',
  timeout: 'The site takes too long to answer. Earnest keeps retrying.',
  gone: 'The feed reports itself gone for good. Its address probably moved; find the new one and resubscribe.',
  http_4xx_gone: 'The feed reports itself gone for good. Its address probably moved; find the new one and resubscribe.',
  rate_limit: 'The site is limiting how often Earnest may ask. It backs off and retries on its own.',
  http_429: 'The site is limiting how often Earnest may ask. It backs off and retries on its own.',
  challenge: 'The site is blocking the fetcher, usually with a bot challenge or HTTP 403.',
  http_403: 'The site is blocking the fetcher, usually with a bot challenge or HTTP 403.',
  http: 'The site answers with an HTTP error.',
  http_4xx: 'The site answers with an HTTP error.',
  http_5xx: 'The site is having server trouble. Earnest keeps retrying.',
  parse: 'The site answers but not with a readable feed. The address may point at a page instead of a feed.',
  decode: 'The site answers but not with a readable feed. The address may point at a page instead of a feed.',
  size: 'The feed response is too large to process.',
  too_large: 'The feed response is too large to process.',
  policy: 'Earnest refused the address for safety. A private or otherwise disallowed address is not fetched.',
  denied_address: 'Earnest refused the address for safety. A private or otherwise disallowed address is not fetched.',
  redirect_loop: 'The feed redirects in a loop.',
  extraction: 'Full-text extraction fails on this feed\u2019s pages.',
}

export function explainHealth(health: FeedHealth | undefined): string | null {
  if (!health || health.consecutive_failures <= 0) return null
  if (!health.error_class) return 'Fetch failed.'
  return HEALTH_EXPLANATIONS[health.error_class] ?? `Fetch failed (${health.error_class}).`
}

// Feeds above twenty posts a day are marked firehose by the service.
export const FIREHOSE_POSTS_PER_DAY = 20
export function isFirehose(sub: Subscription): boolean {
  return sub.health.firehose || sub.health.posts_per_day > FIREHOSE_POSTS_PER_DAY
}
export function healthPending(sub: Subscription): boolean { return !sub.health.last_attempt_at }

export type ImportSummary = {feeds: number; folders: number; deferred: number; failed: string[]}
export function summarizeImport(results: OPMLResult[]): ImportSummary {
  const summary: ImportSummary = {feeds: 0, folders: 0, deferred: 0, failed: []}
  for (const result of results ?? []) {
    if (result.error) {
      summary.failed.push(result.error)
    } else if (result.subscription) {
      summary.feeds++
      if (result.deferred) summary.deferred++
    } else if (result.folder) {
      summary.folders++
    }
  }
  return summary
}

export type KeptImportSummary = {
  added: number
  existing: number
  unkept: number
  gone: number
  skipped: {feedURL: string; count: number}[]
  failed: string[]
  subscribed: number
  // Articles the import added at least one of the export's labels to.
  labelled: number
}
export function summarizeKeptImport(report: KeptImportReport): KeptImportSummary {
  const summary: KeptImportSummary = {added: 0, existing: 0, unkept: 0, gone: 0, skipped: [], failed: [], subscribed: 0, labelled: 0}
  const skipped = new Map<string, number>()
  for (const result of report.results ?? []) {
    if ((result.labels ?? []).length > 0) summary.labelled++
    if (result.status === 'created') {
      summary.added++
    } else if (result.status === 'matched' || result.status === 'unchanged') {
      summary.existing++
    } else if (result.status === 'unkept') {
      summary.unkept++
    } else if (result.status === 'gone') {
      summary.gone++
    } else if (result.status === 'not_subscribed') {
      const feedURL = result.feed_url ?? ''
      skipped.set(feedURL, (skipped.get(feedURL) ?? 0) + 1)
    } else if (result.status === 'failed') {
      const title = result.title || `Article ${result.index + 1}`
      const error = result.error || 'could not be imported'
      summary.failed.push(`${title}: ${error}`)
    }
  }
  summary.skipped = [...skipped].map(([feedURL, count]) => ({feedURL, count}))
    .sort((a, b) => b.count - a.count || (a.feedURL < b.feedURL ? -1 : a.feedURL > b.feedURL ? 1 : 0))
  summary.subscribed = report.subscribed?.length ?? 0
  return summary
}

export type FolderGroup = {folder: Folder | null; subscriptions: Subscription[]}
export function groupByFolder(subscriptions: Subscription[], folders: Folder[]): FolderGroup[] {
  const active = subscriptions.filter(sub => !sub.deleted)
  const groups: FolderGroup[] = folders.filter(folder => !folder.deleted)
    .map(folder => ({folder, subscriptions: active.filter(sub => sub.folder_id === folder.id)}))
  groups.push({folder: null, subscriptions: active.filter(sub => !groups.some(group => group.folder?.id === sub.folder_id))})
  return groups
}

// The advertised onboarding defaults; the service applies the same values to a
// user who never saves settings.
export function defaultReadingSettings(): ReadingSettings {
  return {...defaultSettings, feed_overrides: {}}
}
export type SettingsBody = Omit<ReadingSettings, 'rev'> & {op_id: string}
export function settingsBody(settings: ReadingSettings, opID = newOp()): SettingsBody {
  return settingsInput(settings, opID)
}

// The river views the web reader resumes from; see viewKey in reader.ts.
export const RIVER_POSITION_VIEWS = ['today', 'edition'] as const

export const listSubscriptions = () => request<Subscription[]>('/v1/subscriptions')
export const listFolders = () => request<Folder[]>('/v1/folders')
export const listMutes = () => request<Mute[]>('/v1/mutes')

export const subscribe = (body: SubscribeBody) => request<Subscription>('/v1/subscriptions', {method: 'POST', body: JSON.stringify(body)})
export const patchSubscription = (id: number, patch: SubscriptionPatch) => request<Subscription>(`/v1/subscriptions/${id}`, {method: 'PATCH', body: JSON.stringify(patch)})
export const unsubscribe = (id: number, opID = newOp()) => request(`/v1/subscriptions/${id}?op_id=${encodeURIComponent(opID)}`, {method: 'DELETE'})
export const refreshSubscription = (id: number, opID = newOp()) => request(`/v1/subscriptions/${id}/refresh`, {method: 'POST', body: JSON.stringify({op_id: opID})})

export const createFolder = (name: string) => request<Folder>('/v1/folders', {method: 'POST', body: JSON.stringify({name, op_id: newOp()})})
export const renameFolder = (id: number, name: string) => request<Folder>(`/v1/folders/${id}`, {method: 'PATCH', body: JSON.stringify({name, op_id: newOp()})})
export const moveFolder = (id: number, parentID: number) => request<Folder>(`/v1/folders/${id}`, {method: 'PATCH', body: JSON.stringify({parent_id: parentID, op_id: newOp()})})

// Create inside the parent atomically, so a failed request cannot strand a
// newly created folder at the top level or create duplicates on a move retry.
export const createSubfolder = (name: string, parentID: number) => request<Folder>('/v1/folders', {method: 'POST', body: JSON.stringify({name, parent_id: parentID, op_id: newOp()})})
export const deleteFolder = (id: number, opID = newOp()) => request(`/v1/folders/${id}?op_id=${encodeURIComponent(opID)}`, {method: 'DELETE'})

export const createMute = (kind: 'keyword' | 'author', value: string) => request<Mute>('/v1/mutes', {method: 'POST', body: JSON.stringify({kind, value, op_id: newOp()})})
export const deleteMute = (id: number, opID = newOp()) => request(`/v1/mutes/${id}?op_id=${encodeURIComponent(opID)}`, {method: 'DELETE'})

// File.text() is not implemented by jsdom; FileReader works everywhere.
export function readFileText(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new window.FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error)
    reader.readAsText(file)
  })
}

export const importOPML = (text: string) => request<OPMLResult[]>('/v1/opml:import', {method: 'POST', headers: {'Content-Type': 'text/x-opml'}, body: text})
export const importKept = (text: string, missing: MissingFeeds) => request<KeptImportReport>(`/v1/kept:import?missing_feeds=${missing}`, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: text})
export const exportOPML = () => request<string>('/v1/opml')

export const putSettings = (settings: ReadingSettings) => request<ReadingSettings>('/v1/settings', {method: 'PUT', body: JSON.stringify(settingsBody(settings))})
export const putPosition = (view: string, seq: number) => request(`/v1/positions/${encodeURIComponent(view)}`, {method: 'PUT', body: JSON.stringify({seq, op_id: newOp()})})

// The newest seq across every subscription, so a fresh river can start at now.
export async function latestSeq(): Promise<number> {
  const page = await request<{entries: {seq: number}[] | null}>('/v1/entries?view=all&direction=backward&limit=1')
  return page.entries?.[0]?.seq ?? 0
}
export async function startRiverAt(seq: number): Promise<void> {
  for (const view of RIVER_POSITION_VIEWS) await putPosition(view, seq)
}
