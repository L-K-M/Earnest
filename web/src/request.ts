// The session cookie is HttpOnly; the matching CSRF token is shared by tabs.
const csrfKey = 'earnest.csrf'
export function setCSRF(token: string): void { sessionStorage.setItem(csrfKey, token); localStorage.setItem(csrfKey, token) }

export class RequestError extends Error {
  constructor(public readonly status: number, message: string) { super(message) }
}

async function fetchResponse(path: string, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers)
  const method = options.method ?? 'GET'
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())) {
    const token = localStorage.getItem(csrfKey) ?? sessionStorage.getItem(csrfKey)
    if (token) headers.set('X-CSRF-Token', token)
  }
  if (typeof options.body === 'string' && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, {...options, headers, credentials: 'same-origin'})
  if (!response.ok) {
    let detail = `Request failed (${response.status}).`
    try {
      const problem = await response.json() as {detail?: string; title?: string}
      detail = problem.detail || problem.title || detail
    } catch { /* A proxy may return plain text instead of a problem document. */ }
    throw new RequestError(response.status, detail)
  }
  return response
}

// Sync decodes integer literals losslessly; the ordinary UI rejects unsafe IDs.
export async function requestText(path: string, options: RequestInit = {}): Promise<string> {
  return (await fetchResponse(path, options)).text()
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetchResponse(path, options)
  if (response.status === 204) return undefined as T
  if (!(response.headers.get('Content-Type') ?? '').includes('json')) return await response.text() as T
  return JSON.parse(await response.text(), (_key: string, value: unknown) => {
    // Refuse rounded IDs before they can address the wrong article or register.
    if (typeof value === 'number' && Number.isInteger(value) && !Number.isSafeInteger(value)) throw new Error('The server returned an integer outside this browser’s supported range.')
    return value
  }) as T
}
