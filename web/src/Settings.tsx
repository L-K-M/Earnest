import {useState} from 'react'
import {request} from './request'
import type {components} from './api.generated'

export type ReadingSettings = Omit<components['schemas']['ReaderSettings'], 'reading_mode' | 'theme' | 'font' | 'feed_overrides' | 'rev'> & {
  reading_mode: 'river' | 'unread_counts'
  theme: 'system' | 'light' | 'dark' | 'black' | 'eink'
  font: 'serif' | 'sans' | 'dyslexic'
  feed_overrides: Record<string, {theme?: ReadingSettings['theme']; measure?: number; font?: ReadingSettings['font']}>
  rev?: number
}
export const defaultSettings: ReadingSettings = {reading_mode: 'river', today_decay_days: 2, single_key_shortcuts: true, theme: 'system', measure: 66, font: 'serif', feed_overrides: {}}
export function settingsInput(settings: ReadingSettings, opID: string = crypto.randomUUID()) {
  return {reading_mode:settings.reading_mode,today_decay_days:settings.today_decay_days,single_key_shortcuts:settings.single_key_shortcuts,theme:settings.theme,measure:settings.measure,font:settings.font,feed_overrides:settings.feed_overrides,op_id:opID}
}

// withoutFeedOverride returns settings with one feed's typography override
// removed, so the feed inherits the account-wide typography again. It returns
// the input unchanged when the feed has no override.
export function withoutFeedOverride(settings: ReadingSettings, feedID: string): ReadingSettings {
  if (!(feedID in settings.feed_overrides)) return settings
  const feed_overrides = {...settings.feed_overrides}
  delete feed_overrides[feedID]
  return {...settings, feed_overrides}
}

export function Settings({settings, onSaved}: {settings: ReadingSettings; onSaved: (settings: ReadingSettings) => void}) {
  const [draft, setDraft] = useState(settings)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  async function save(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    try { onSaved(await request<ReadingSettings>('/v1/settings', {method: 'PUT', body: JSON.stringify(settingsInput(draft))})); setError('') }
    catch (error) { setError(error instanceof Error ? error.message : 'Could not save settings.') }
    finally { setSaving(false) }
  }
  return <form onSubmit={save}><h2>Reading settings</h2>
    <label>Reading mode<select value={draft.reading_mode} onChange={e => setDraft({...draft, reading_mode: e.target.value as ReadingSettings['reading_mode']})}><option value="river">A river that ends</option><option value="unread_counts">Unread counts</option></select></label>
    <label>Today items stay for this many days<input type="number" min="1" max="365" value={draft.today_decay_days} onChange={e => setDraft({...draft, today_decay_days: e.target.valueAsNumber})} required /></label>
    <label><input type="checkbox" checked={draft.single_key_shortcuts} onChange={e => setDraft({...draft, single_key_shortcuts: e.target.checked})} /> Single-key shortcuts</label>
    <Typography value={draft} onChange={value => setDraft({...draft, ...value})} />
    <button disabled={saving}>Save settings</button>{error && <p role="alert">{error}</p>}
  </form>
}
type TypographySettings = Pick<ReadingSettings, 'theme' | 'measure' | 'font'>
export function Typography({value, onChange}: {value: TypographySettings; onChange: (value: TypographySettings) => void}) {
  return <fieldset><legend>Typography</legend>
    <label>Theme<select value={value.theme} onChange={e => onChange({...value, theme: e.target.value as ReadingSettings['theme']})}>{(['system', 'light', 'dark', 'black', 'eink'] as const).map(theme => <option key={theme}>{theme}</option>)}</select></label>
    <label>Measure (characters)<input type="number" min="62" max="72" value={value.measure} onChange={e => onChange({...value, measure: e.target.valueAsNumber})} required /></label>
    <label>Font<select value={value.font} onChange={e => onChange({...value, font: e.target.value as ReadingSettings['font']})}><option value="serif">Serif</option><option value="sans">Sans serif</option><option value="dyslexic">Dyslexia friendly</option></select></label>
  </fieldset>
}
