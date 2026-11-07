import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import type {components} from '../api.generated'
import {request} from '../request'

type Label = components['schemas']['Label']
const newOp = () => crypto.randomUUID()
const message = (error: unknown, fallback: string) => error instanceof Error && error.message ? error.message : fallback
const json = (body: unknown, method: string): RequestInit => ({method, body: JSON.stringify(body)})

// LabelSettings owns the native label definitions. The sidebar and rule
// choices read the same ['labels'] cache entry, so every successful mutation
// invalidates it and the whole app sees the new definitions.
export function LabelSettings() {
  const cache = useQueryClient()
  const list = useQuery({queryKey: ['labels'], queryFn: ({signal}) => request<Label[] | null>('/v1/labels', {signal})})
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<number | null>(null)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  async function act(work: () => Promise<unknown>, note: string): Promise<boolean> {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await work()
      await cache.invalidateQueries({queryKey: ['labels']})
      setNotice(note)
      return true
    } catch (e) {
      setError(message(e, 'The change did not go through.'))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) { setError('Give the label a name.'); return }
    setError('')
    if (await act(() => request<Label>('/v1/labels', json({name: trimmed, op_id: newOp()}, 'POST')), 'Label created.')) setName('')
  }

  const labels = (list.data ?? []).filter(label => !label.deleted)
  return <section aria-labelledby="native-labels-heading">
    <h2 id="native-labels-heading">Native labels</h2>
    <p>Labels collect articles without changing their shelves. Deleting a label removes its memberships; the articles themselves are kept.</p>
    {(error || list.error) && <p role="alert">{error || list.error?.message}</p>}
    {notice && <p role="status">{notice}</p>}
    {list.isPending && <p role="status">Loading labels…</p>}
    <form onSubmit={event => {void add(event)}}>
      <label>New label<input value={name} onChange={event => setName(event.target.value)} required /></label>
      <button disabled={busy}>Create label</button>
    </form>
    {list.isSuccess && labels.length === 0 && <p>No labels yet.</p>}
    <ul>{labels.map(label => <li key={label.id}>
      {editing === label.id
        ? <form onSubmit={event => {
            event.preventDefault()
            const renamed = draft.trim()
            if (!renamed) { setError('Give the label a name.'); return }
            void act(() => request<Label>(`/v1/labels/${label.id}`, json({name: renamed, op_id: newOp()}, 'PATCH')), 'Label renamed.')
              .then(ok => { if (ok) setEditing(null) })
          }}>
            <label>Rename {label.name}<input value={draft} onChange={event => setDraft(event.target.value)} required /></label>
            <button disabled={busy}>Save</button>
            <button type="button" disabled={busy} onClick={() => setEditing(null)}>Cancel</button>
          </form>
        : <>{label.name} <button disabled={busy} aria-label={`Rename ${label.name}`} onClick={() => { setEditing(label.id); setDraft(label.name); setError('') }}>Rename</button>
          <button disabled={busy} aria-label={`Delete ${label.name}`} onClick={() => {
            if (window.confirm(`Delete label “${label.name}”? Articles are kept; only this label’s memberships are removed.`)) {
              void act(() => request<Label>(`/v1/labels/${label.id}?op_id=${encodeURIComponent(newOp())}`, {method: 'DELETE'}), 'Label deleted.')
            }
          }}>Delete</button></>}
    </li>)}</ul>
  </section>
}
