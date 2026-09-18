import { describe, expect, it } from 'vitest'
import { ApiError, api } from './client'
import { stubApi } from '@/test/fetch'

describe('api client', () => {
  it('returns the parsed JSON body', async () => {
    stubApi({ '/projects': { projects: [] } })
    await expect(api.get('/projects')).resolves.toEqual({ projects: [] })
  })

  it('turns an error body into ApiError with the server text', async () => {
    stubApi({ '/rooms/x': Response.json({ error: 'room x: not found' }, { status: 404 }) })
    const err = await api.get('/rooms/x').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 404, message: 'room x: not found' })
  })

  it('falls back to the status line when the error is not JSON', async () => {
    stubApi({ '/boom': new Response('nope', { status: 502, statusText: 'Bad Gateway' }) })
    await expect(api.get('/boom')).rejects.toMatchObject({ status: 502, message: '502 Bad Gateway' })
  })

  it('posts JSON with the content type set', async () => {
    let seen: { type: string | null; body: unknown } | undefined
    stubApi({
      '/projects': async (req) => {
        seen = { type: req.headers.get('content-type'), body: await req.json() }
        return Response.json({ ok: true }, { status: 201 })
      },
    })
    await api.post('/projects', { name: 'p' })
    expect(seen).toEqual({ type: 'application/json', body: { name: 'p' } })
  })
})
