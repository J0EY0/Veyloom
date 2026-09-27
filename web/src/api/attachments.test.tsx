import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { roomAttachment } from '@/test/fixtures'
import { stubApi } from '@/test/fetch'
import { ATTACHMENT_PAGE, archiveUrl, attachmentUrl, fetchText, pictureUrl, useRoomAttachments, type AttachmentFilter } from './attachments'

describe('where attachments are served', () => {
  it('shows a big picture by its smaller copy, a small one by itself', () => {
    expect(attachmentUrl('a1')).toBe('/api/v1/attachments/a1')
    expect(pictureUrl({ id: 'a1', thumbnail: true })).toBe('/api/v1/attachments/a1/thumbnail')
    expect(pictureUrl({ id: 'a1' })).toBe('/api/v1/attachments/a1')
  })

  it('packs the picked ones of a room into one download', () => {
    expect(archiveUrl('r1', ['a1', 'a 2'])).toBe('/api/v1/rooms/r1/attachments/archive?ids=a1,a%202')
  })
})

// A text attachment is read in part: the chat shows its start, the viewer
// up to a limit (docs/webui.md 4.21).
describe('reading a text attachment', () => {
  it('asks for the start and cuts it after the last whole line', async () => {
    let range = ''
    stubApi({
      '/attachments/a1': (req: Request) => {
        range = req.headers.get('Range') ?? ''
        return new Response('第一行\nsecond\nthi', { status: 206, headers: { 'Content-Range': 'bytes 0-19/400' } })
      },
    })
    await expect(fetchText('a1', 20)).resolves.toEqual({ text: '第一行\nsecond\n', more: true })
    expect(range).toBe('bytes=0-19')
  })

  it('keeps a file that fits whole, however the server answers', async () => {
    stubApi({
      '/attachments/small': new Response('one\ntwo', { status: 206, headers: { 'Content-Range': 'bytes 0-6/7' } }),
      '/attachments/whole': new Response('one\ntwo', { status: 200 }),
    })
    await expect(fetchText('small', 1024)).resolves.toEqual({ text: 'one\ntwo', more: false })
    await expect(fetchText('whole', 1024)).resolves.toEqual({ text: 'one\ntwo', more: false })
  })

  it('fails when the file cannot be read', async () => {
    stubApi({ '/attachments/gone': new Response('', { status: 404 }) })
    await expect(fetchText('gone', 1024)).rejects.toThrow(/404/)
  })
})

describe('the attachments of a room', () => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
  )

  it('asks with the filter, then for the next page until every one is loaded', async () => {
    const total = ATTACHMENT_PAGE + 1
    const calls = stubApi({
      '/rooms/r1/attachments': (req: Request) => {
        const offset = Number(new URL(req.url).searchParams.get('offset'))
        const count = offset === 0 ? ATTACHMENT_PAGE : 1
        return { attachments: Array.from({ length: count }, (_, i) => roomAttachment(`a${offset + i}`, `f${offset + i}.png`, 'image')), total }
      },
    })
    const filter: AttachmentFilter = { q: '  spec ', kinds: ['pdf', 'text'], sender: 'member:m1', sort: 'size' }
    const { result } = renderHook(() => useRoomAttachments('r1', filter), { wrapper })
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1))
    expect(calls[0]).toBe(`GET /rooms/r1/attachments?sort=size&offset=0&limit=${ATTACHMENT_PAGE}&q=spec&kind=pdf%2Ctext&sender=member%3Am1`)
    expect(result.current.hasNextPage).toBe(true)

    await act(() => result.current.fetchNextPage())
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2))
    expect(calls[1]).toContain(`offset=${ATTACHMENT_PAGE}&`)
    expect(result.current.hasNextPage).toBe(false)
  })

  it('leaves out what the filter does not narrow', async () => {
    const calls = stubApi({ '/rooms/r1/attachments': { attachments: [], total: 0 } })
    const { result } = renderHook(() => useRoomAttachments('r1', { q: ' ', kinds: [], sender: '', sort: 'newest' }), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(calls).toEqual([`GET /rooms/r1/attachments?sort=newest&offset=0&limit=${ATTACHMENT_PAGE}`])
    expect(result.current.hasNextPage).toBe(false)
  })
})
