import {useId, useState} from 'react'
import {Brand} from './Brand'

// Sign-in keeps the reader's place: whatever URL they opened is still in the
// address bar and renders once the session exists.
export function Login({onSignIn}: {onSignIn: (username: string, password: string) => Promise<string | null>}) {
  const ids = useId()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [reveal, setReveal] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setPending(true)
    try {
      setError(await onSignIn(String(form.get('username')), String(form.get('password'))) ?? '')
    } finally {
      setPending(false)
    }
  }
  const describedBy = error ? `${ids}-error` : undefined
  return <main className="login">
    <Brand />
    <form onSubmit={event => {void submit(event)}} aria-busy={pending}>
      <label>Username<input name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required aria-invalid={error ? true : undefined} aria-describedby={describedBy} /></label>
      <label>Password<input name="password" type={reveal ? 'text' : 'password'} autoComplete="current-password" required aria-invalid={error ? true : undefined} aria-describedby={describedBy} /></label>
      <label className="check"><input type="checkbox" checked={reveal} onChange={event => setReveal(event.target.checked)} /> Show password</label>
      <button className="primary" disabled={pending}>{pending ? 'Signing in…' : 'Sign in'}</button>
      {error && <p id={`${ids}-error`} role="alert" className="form-error">{error}</p>}
    </form>
  </main>
}
