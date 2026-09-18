// The HTTP client for /api/v1. One function does every request so error
// handling and JSON encoding live in exactly one place.

import { signedOut } from '@/lib/authEvents'
import { apiBase as base } from './base'

// ApiError carries the status and the server's `error` text. Callers
// branch on status (404 → "没有这个房间") and show the message as-is. body
// is the whole JSON error, for the few answers that say more than a line.
export class ApiError extends Error {
  readonly status: number
  readonly body: Record<string, unknown>

  constructor(status: number, message: string, body: Record<string, unknown> = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  // json is encoded as the body with the matching content type.
  json?: unknown
  // form is sent as multipart; the browser sets the content type.
  form?: FormData
  // blob is sent as the body, as the type it says it is.
  blob?: Blob
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { json, form, blob, headers: givenHeaders, ...init } = options
  const headers = new Headers(givenHeaders)
  let body: BodyInit | undefined
  if (json !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(json)
  } else if (form !== undefined) {
    body = form
  } else if (blob !== undefined) {
    headers.set('Content-Type', blob.type || 'application/octet-stream')
    body = blob
  }

  const res = await fetch(base + path, { ...init, headers, body, credentials: 'include' })
  if (!res.ok) {
    noteUnauthorized(path, res)
    throw await apiError(res)
  }
  // 204 and the odd 202 without a body both mean "nothing to read".
  const text = await res.text()
  return (text === '' ? undefined : JSON.parse(text)) as T
}

// requestText fetches a non-JSON body, such as a transcript.
export async function requestText(path: string, options: RequestOptions = {}): Promise<string> {
  const { json: _json, form: _form, blob: _blob, ...init } = options
  void _json
  void _form
  void _blob
  const res = await fetch(base + path, { ...init, credentials: 'include' })
  if (!res.ok) {
    noteUnauthorized(path, res)
    throw await apiError(res)
  }
  return res.text()
}

// A 401 anywhere but the sign-in routes means the session ended; the
// auth gate hears it and shows the login page.
function noteUnauthorized(path: string, res: Response) {
  if (res.status === 401 && !path.startsWith('/auth/')) signedOut()
}

async function apiError(res: Response): Promise<ApiError> {
  const fallback = `${res.status} ${res.statusText}`.trim()
  try {
    const data: unknown = await res.json()
    if (data !== null && typeof data === 'object' && !Array.isArray(data)) {
      const body = data as Record<string, unknown>
      const message = typeof body.error === 'string' && body.error !== '' ? body.error : fallback
      return new ApiError(res.status, message, body)
    }
  } catch {
    // Not a JSON error body; fall through to the status line.
  }
  return new ApiError(res.status, fallback)
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>(path, { ...options, method: 'GET' }),
  post: <T>(path: string, json: unknown, options?: RequestOptions) => request<T>(path, { ...options, method: 'POST', json }),
  patch: <T>(path: string, json: unknown, options?: RequestOptions) => request<T>(path, { ...options, method: 'PATCH', json }),
  put: <T>(path: string, json: unknown, options?: RequestOptions) => request<T>(path, { ...options, method: 'PUT', json }),
  delete: (path: string, options?: RequestOptions) => request<void>(path, { ...options, method: 'DELETE' }),
  text: (path: string, options?: RequestOptions) => requestText(path, { ...options, method: 'GET' }),
  upload: <T>(path: string, form: FormData, options?: RequestOptions) => request<T>(path, { ...options, method: 'POST', form }),
  send: <T>(path: string, blob: Blob, options?: RequestOptions) => request<T>(path, { ...options, method: 'POST', blob }),
}
