import {useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {KeptImportTools, OPMLTools} from './FeedSettings'

// Both entry points use the same import/export operations and reports. Keep
// Feeds available for existing workflows while making transfer tools easy to
// find with the other account settings.
export function ImportExportSettings() {
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function act(work: () => Promise<unknown>) {
    setBusy(true)
    setError('')
    try {
      await work()
      await client.invalidateQueries()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not finish the import. Please try again.')
      return false
    } finally {
      setBusy(false)
    }
  }
  return <section aria-labelledby="import-export-heading">
    <h2 id="import-export-heading">Import &amp; export</h2>
    <p>Move your subscriptions and saved articles between readers.</p>
    {error && <p role="alert">{error}</p>}
    <OPMLTools busy={busy} act={act} />
    <KeptImportTools busy={busy} act={act} />
  </section>
}
