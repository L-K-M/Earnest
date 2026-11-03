import {useEffect, useRef, useState, type ReactNode} from 'react'
import {useQuery} from '@tanstack/react-query'
import type {Entry, ReaderActions, Subscription} from './reader'
import {readerURL} from './reader'
import {request} from './request'
import {Typography, settingsInput, type ReadingSettings} from './Settings'
import {OfflineArticle, useOnline} from './offline/Offline'
import {MoreLikeThis} from './search/Search'
import {Summary} from './pipeline/Pipeline'

export function Reader({entry, subscription, settings, actions, onClose, onSettings, renderBody, onOpen}: {entry: Entry; subscription?: Subscription; settings: ReadingSettings; actions: ReaderActions; onClose: () => void; onSettings: (value: ReadingSettings) => void; renderBody: (url: string) => ReactNode; onOpen?: (entry: Entry) => void}) {
  const online = useOnline()
  const title = useRef<HTMLHeadingElement>(null)
  const [error, setError] = useState('')
  const [overrideDraft, setOverrideDraft] = useState(() => ({...settings, ...settings.feed_overrides[String(entry.feed_id)]}))
  const [savingOverride, setSavingOverride] = useState(false)
  const [dark, setDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => { title.current?.focus() }, [entry.id])
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const changed = () => setDark(query.matches)
    query.addEventListener('change', changed)
    return () => query.removeEventListener('change', changed)
  }, [])
  const typography = {...settings, ...settings.feed_overrides[String(entry.feed_id)]}
  const content = useQuery({queryKey: ['content', entry.id, typography.theme, typography.measure, typography.font], enabled:online, gcTime:0, queryFn: () => request<{url: string}>(`/web/content-url/${entry.id}`)})
  const state = actions.state(entry)
  function act(register: 'seen' | 'shelf' | 'kept', value: 'unseen' | 'done' | 'later' | 'none' | boolean) {
    void actions.act(entry, register, value).catch(error => setError(error instanceof Error ? error.message : 'Could not save action.'))
  }
  async function override(value: {theme: ReadingSettings['theme']; measure: number; font: ReadingSettings['font']}) {
    setSavingOverride(true)
    try {
      const next = {...settings, feed_overrides: {...settings.feed_overrides, [String(entry.feed_id)]: value}}
      onSettings(await request<ReadingSettings>('/v1/settings', {method: 'PUT', body: JSON.stringify(settingsInput(next))}))
      setError('')
    } catch (error) { setError(error instanceof Error ? error.message : 'Could not save feed typography.') }
    finally { setSavingOverride(false) }
  }
  return <article className="reader" aria-label="Reader">
    <button onClick={onClose}>Back to list</button>
    <header><h2 ref={title} tabIndex={-1}>{entry.title || 'Untitled'}</h2><p>{subscription?.title || 'Feed'}{entry.author && ` · ${entry.author}`}{entry.published_at && <> · <time dateTime={entry.published_at}>{new Date(entry.published_at).toLocaleString()}</time></>}</p>
      <a href={entry.url} target="_blank" rel="noopener noreferrer">Open original</a>
    </header>
    <Summary entryID={entry.id} shortcuts={settings.single_key_shortcuts}/>
    <div className="actions" aria-label="Article actions"><button onClick={() => act('shelf', 'done')}>Done (e)</button><button aria-pressed={state.shelf.value === 'later'} onClick={() => act('shelf', state.shelf.value === 'later' ? 'none' : 'later')}>Later (s)</button><button aria-pressed={state.kept.value === true} onClick={() => act('kept', state.kept.value !== true)}>Keep (f)</button><button onClick={() => act('seen', 'unseen')}>Unread (m)</button></div>
    {error && <p role="alert">{error}</p>}
    <details><summary>Typography for this feed</summary><form onSubmit={event => {event.preventDefault();void override(overrideDraft)}}><Typography value={overrideDraft} onChange={value => setOverrideDraft({...overrideDraft, ...value})} /><button disabled={savingOverride}>Save for this feed</button></form></details>
    {content.error && <p role="alert">{content.error.message} <button onClick={() => { void content.refetch() }}>Try again</button></p>}
    {online && content.isPending && <p role="status">Opening article…</p>}
    {!online || content.isError ? <OfflineArticle entryID={entry.id}/> : content.data && renderBody(readerURL(content.data.url, typography, dark))}
    {onOpen && online && <MoreLikeThis key={entry.id} entryID={entry.id} onOpen={onOpen}/>}
  </article>
}
