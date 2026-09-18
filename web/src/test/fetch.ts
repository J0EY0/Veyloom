import { vi } from 'vitest'
import { objectUrlResponse } from './objectUrls'

// A handler gets the request and, for an upload, the form that was sent:
// jsdom's FormData is not the one Node's Request understands, so it
// travels beside the request instead of inside it.
type Handler = (req: Request, form?: FormData) => unknown | Promise<unknown>
type Stub = Handler | Response | object

// stubApi replaces fetch with a table of /api/v1 paths. A plain object is
// returned as JSON; a function is called with the request; a Response is
// sent as-is. Unknown paths get a 404 so a test fails on the path it hit.
export function stubApi(routes: Record<string, Stub>): string[] {
  const calls: string[] = []
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = input instanceof Request ? input.url : String(input)
    // A picked file's preview URL answers with the file itself.
    if (raw.startsWith('blob:')) {
      const found = await objectUrlResponse(raw)
      if (found) return found
    }
    // The app uses relative URLs; Request needs an absolute one.
    const url = new URL(raw, 'http://test')
    const form = init?.body instanceof FormData ? init.body : undefined
    const req = new Request(url, form ? { ...init, body: undefined } : init)
    const path = url.pathname.replace(/^\/api\/v1/, '')
    calls.push(`${req.method} ${path}${url.search}`)
    const stub = routes[path]
    if (stub === undefined) {
      return Response.json({ error: `no stub for ${path}` }, { status: 404 })
    }
    const body = typeof stub === 'function' ? await stub(req, form) : stub
    return body instanceof Response ? body : Response.json(body)
  })
  return calls
}
