import {useEffect, useId, useRef, useState, type ReactNode} from 'react'
import {useQuery} from '@tanstack/react-query'
import type {Entry, ReaderActions, Subscription} from './reader'
import {readerURL} from './reader'
import {request} from './request'
import {Typography, settingsInput, withoutFeedOverride, type ReadingSettings} from './Settings'
import {OfflineArticle, useOnline} from './offline/Offline'
import {MoreLikeThis} from './search/Search'
import {Summary} from './pipeline/Pipeline'
import {Icon} from './Icon'
import {longDate} from './format'

export function Reader({entry, subscription, settings, actions, onClose, onSettings, renderBody, onOpen, onNotify}: {entry: Entry; subscription?: Subscription; settings: ReadingSettings; actions: ReaderActions; onClose: () => void; onSettings: (value: ReadingSettings) => void; renderBody: (url: string) => ReactNode; onOpen?: (entry: Entry) => void; onNotify?: (message: string) => void}) {
  const online = useOnline()
  const ids = useId()
  const title = useRef<HTMLHeadingElement>(null)
  const firstTypographyField = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const feedOverrideKey = String(entry.feed_id)
  const hasOverride = feedOverrideKey in settings.feed_overrides
  const typography = {...settings, ...settings.feed_overrides[feedOverrideKey]}
  // The draft holds only the typography triple, so saving stores a clean
  // per-feed override instead of the whole settings document.
  const [overrideDraft, setOverrideDraft] = useState<Pick<ReadingSettings, 'theme' | 'measure' | 'font'>>(() => ({theme: typography.theme, measure: typography.measure, font: typography.font}))
  const [savingOverride, setSavingOverride] = useState(false)
  const [dark, setDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => { title.current?.focus({preventScroll: true}) }, [entry.id])
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const changed = () => setDark(query.matches)
    query.addEventListener('change', changed)
    return () => query.removeEventListener('change', changed)
  }, [])
  const content = useQuery({queryKey: ['content', entry.id, typography.theme, typography.measure, typography.font], enabled:online, gcTime:0, queryFn: () => request<{url: string}>(`/web/content-url/${entry.id}`)})
  const state = actions.state(entry)
  // The reading surface takes the article frame's palette, so a per-feed
  // theme never leaves a light frame floating on a dark pane.
  const surface = typography.theme === 'system' ? (dark ? 'dark' : 'light') : typography.theme
  const canShare = typeof navigator.share === 'function'
  function act(register: 'seen' | 'shelf' | 'kept', value: 'unseen' | 'done' | 'later' | 'none' | boolean) {
    void actions.act(entry, register, value).catch(error => setError(error instanceof Error ? error.message : 'Could not save action.'))
  }
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(entry.url)
      onNotify?.('Link to the original copied.')
    } catch {
      setError('Could not copy the link. Your browser may need permission to use the clipboard.')
    }
  }
  async function share() {
    try {
      await navigator.share({title: entry.title || undefined, url: entry.url})
    } catch (cause) {
      // Closing the share sheet is a choice, not an error.
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError('Could not open the share sheet.')
    }
  }
  async function override(value: {theme: ReadingSettings['theme']; measure: number; font: ReadingSettings['font']}) {
    setSavingOverride(true)
    try {
      const next = {...settings, feed_overrides: {...settings.feed_overrides, [feedOverrideKey]: value}}
      onSettings(await request<ReadingSettings>('/v1/settings', {method: 'PUT', body: JSON.stringify(settingsInput(next))}))
      setError('')
      onNotify?.('Typography saved for this feed.')
    } catch (error) { setError(error instanceof Error ? error.message : 'Could not save feed typography.') }
    finally { setSavingOverride(false) }
  }
  async function resetOverride() {
    setSavingOverride(true)
    try {
      const next = withoutFeedOverride(settings, feedOverrideKey)
      onSettings(await request<ReadingSettings>('/v1/settings', {method: 'PUT', body: JSON.stringify(settingsInput(next))}))
      setOverrideDraft({theme: next.theme, measure: next.measure, font: next.font})
      setError('')
      onNotify?.('This feed uses your account typography again.')
    } catch (error) { setError(error instanceof Error ? error.message : 'Could not reset feed typography.') }
    finally { setSavingOverride(false) }
  }
  const date = entry.published_at || entry.first_seen_at
  const shelf = state.shelf.value === 'done' ? 'Done' : state.shelf.value === 'later' ? 'Later' : ''
  return <article className="reader" aria-labelledby={`${ids}-title`} data-theme={surface} data-font={typography.font} style={{'--measure': typography.measure} as React.CSSProperties}>
    <div className="reader-toolbar" role="group" aria-label="Article actions">
      <button type="button" className="tool back" onClick={onClose} title="Back (Esc)" aria-keyshortcuts="Escape"><Icon name="back" /><span className="label">Back</span></button>
      <span className="tool-divider" aria-hidden="true" />
      <button type="button" className="tool" data-tone="green" onClick={() => act('shelf', 'done')} title="Done (e)" aria-keyshortcuts="e"><Icon name="done" /><span className="label">Done</span></button>
      <button type="button" className="tool" data-tone="amber" aria-pressed={state.shelf.value === 'later'} onClick={() => act('shelf', state.shelf.value === 'later' ? 'none' : 'later')} title="Later (s)" aria-keyshortcuts="s"><Icon name="later" /><span className="label">Later</span></button>
      <button type="button" className="tool" data-tone="plum" aria-pressed={state.kept.value === true} onClick={() => act('kept', state.kept.value !== true)} title="Keep (f)" aria-keyshortcuts="f"><Icon name="keep" /><span className="label">Keep</span></button>
      <button type="button" className="tool" onClick={() => act('seen', 'unseen')} title="Mark unread (m)" aria-keyshortcuts="m"><Icon name="unread" /><span className="label">Mark unread</span></button>
      <span className="tool-spacer" />
      <a className="tool secondary" href={entry.url} target="_blank" rel="noopener noreferrer" title="Open original (v)" aria-keyshortcuts="v"><Icon name="external" /><span className="label">Open original</span></a>
      <button type="button" className={`tool secondary copy${canShare ? ' has-share' : ''}`} onClick={() => {void copyLink()}} title="Copy link to the original"><Icon name="link" /><span className="label">Copy link</span></button>
      {canShare && <button type="button" className="tool secondary" onClick={() => {void share()}} title="Share"><Icon name="share" /><span className="label">Share</span></button>}
      <button type="button" className="tool secondary" popoverTarget={`${ids}-typography`} title="Typography for this feed"><Icon name="type" /><span className="label">Typography</span></button>
    </div>
    <div className="reader-scroll">
      <header className="article-header">
        <h2 id={`${ids}-title`} ref={title} tabIndex={-1} dir="auto">{entry.title || 'Untitled'}</h2>
        <p className="article-meta"><span dir="auto">{subscription?.title || 'Feed'}</span>{entry.author && <> · <span dir="auto">{entry.author}</span></>}{date && <> · <time dateTime={date}>{longDate(date)}</time></>}{shelf && <> · <span className="chip" data-tone={shelf === 'Done' ? 'green' : 'amber'}>{shelf}</span></>}{state.kept.value === true && <> · <span className="chip" data-tone="plum">Kept</span></>}</p>
      </header>
      <Summary entryID={entry.id}/>
      {error && <p role="alert" className="banner problem inline">{error} <button type="button" className="quiet" onClick={() => setError('')}>Dismiss</button></p>}
      {content.error && <p role="alert" className="banner problem inline">{content.error.message} <button type="button" onClick={() => { void content.refetch() }}>Try again</button></p>}
      {online && content.isPending && <div className="article-loading" aria-busy="true"><p role="status" className="sr-only">Opening article…</p><div className="skeleton line" /><div className="skeleton line" /><div className="skeleton line short" /></div>}
      <div className="article-body">{(!online && !content.data) || content.isError ? <OfflineArticle entryID={entry.id}/> : content.data && renderBody(readerURL(content.data.url, typography, dark))}</div>
      {onOpen && online && <MoreLikeThis key={entry.id} entryID={entry.id} onOpen={onOpen}/>}
    </div>
    <div id={`${ids}-typography`} popover="auto" className="popover" aria-labelledby={`${ids}-typography-title`} onToggle={event => { if ((event.nativeEvent as ToggleEvent).newState === 'open') firstTypographyField.current?.querySelector<HTMLElement>('select, input')?.focus() }}>
      <h3 id={`${ids}-typography-title`}>Typography for {subscription?.title || 'this feed'}</h3>
      <form onSubmit={event => {event.preventDefault();void override(overrideDraft)}}>
        <div ref={firstTypographyField}><Typography value={overrideDraft} onChange={value => setOverrideDraft({...overrideDraft, ...value})} /></div>
        <p className="button-row"><button className="primary" disabled={savingOverride}>Save for this feed</button>{hasOverride && <button type="button" disabled={savingOverride} onClick={() => {void resetOverride()}}>Reset to account default</button>}</p>
      </form>
    </div>
  </article>
}
