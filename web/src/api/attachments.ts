import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { apiBase } from './base'
import { api } from './client'
import type { Attachment, AttachmentKind, AttachmentResponse, AttachmentSort, RoomAttachmentsResponse } from './types'

// Where an attachment's bytes are served from.
export function attachmentUrl(id: string): string {
  return `${apiBase}/attachments/${id}`
}

// Where the smaller copy of a big picture is; a small one is its own.
export function thumbnailUrl(id: string): string {
  return `${apiBase}/attachments/${id}/thumbnail`
}

// pictureUrl is what a chat or the attachments tab shows of a picture.
export function pictureUrl(attachment: Pick<Attachment, 'id' | 'thumbnail'>): string {
  return attachment.thumbnail ? thumbnailUrl(attachment.id) : attachmentUrl(attachment.id)
}

// archiveUrl downloads attachments of a room together, as one zip.
export function archiveUrl(roomId: string, ids: string[]): string {
  return `${apiBase}/rooms/${roomId}/attachments/archive?ids=${ids.map(encodeURIComponent).join(',')}`
}

// What the attachments tab asks for (docs/webui.md 4.21): words, kinds, a
// sender as user:<id> or member:<id>, an order.
export interface AttachmentFilter {
  q: string
  kinds: AttachmentKind[]
  sender: string
  sort: AttachmentSort
}

export const attachmentKeys = {
  room: (roomId: string) => ['rooms', roomId, 'attachments'] as const,
  list: (roomId: string, filter: AttachmentFilter) => ['rooms', roomId, 'attachments', filter] as const,
  text: (id: string, bytes: number) => ['attachments', id, 'text', bytes] as const,
}

export const ATTACHMENT_PAGE = 60

// useRoomAttachments pages through what messages of a room carry, the
// next page as the list is scrolled. What was shown stays up while a new
// filter loads.
export function useRoomAttachments(roomId: string, filter: AttachmentFilter, enabled = true) {
  return useInfiniteQuery({
    queryKey: attachmentKeys.list(roomId, filter),
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams({ sort: filter.sort, offset: String(pageParam), limit: String(ATTACHMENT_PAGE) })
      if (filter.q.trim() !== '') params.set('q', filter.q.trim())
      if (filter.kinds.length > 0) params.set('kind', filter.kinds.join(','))
      if (filter.sender !== '') params.set('sender', filter.sender)
      return api.get<RoomAttachmentsResponse>(`/rooms/${roomId}/attachments?${params}`)
    },
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, page) => n + page.attachments.length, 0)
      return last.attachments.length > 0 && loaded < last.total ? loaded : undefined
    },
    enabled: enabled && roomId !== '',
    placeholderData: keepPreviousData,
  })
}

// TextHead is the start of a text attachment, and whether it goes on.
export interface TextHead {
  text: string
  more: boolean
}

// fetchText reads up to bytes of a text attachment. A cut ends at the last
// whole line, so no character is split in two.
export async function fetchText(id: string, bytes: number): Promise<TextHead> {
  const res = await fetch(attachmentUrl(id), { credentials: 'include', headers: { Range: `bytes=0-${bytes - 1}` } })
  if (!res.ok) throw new Error(`read ${id}: ${res.status}`)
  const buffer = await res.arrayBuffer()
  const whole = Number(res.headers.get('Content-Range')?.split('/')[1] ?? buffer.byteLength)
  const more = res.status === 206 && whole > buffer.byteLength
  let text = new TextDecoder().decode(buffer)
  if (more) {
    const end = text.lastIndexOf('\n')
    if (end > 0) text = text.slice(0, end + 1)
  }
  return { text, more }
}

// useAttachmentText is fetchText, kept: the bytes behind an id never change.
export function useAttachmentText(id: string, bytes: number, enabled = true) {
  return useQuery({ queryKey: attachmentKeys.text(id, bytes), queryFn: () => fetchText(id, bytes), staleTime: Infinity, enabled })
}

// uploadAttachment sends one file to the room; the returned record is
// carried by a message through its attachment_ids.
export async function uploadAttachment(roomId: string, file: Blob, filename: string): Promise<Attachment> {
  const form = new FormData()
  form.append('file', file, filename)
  return (await api.upload<AttachmentResponse>(`/rooms/${roomId}/attachments`, form)).attachment
}

// blobFromUrl turns the URL the prompt input holds for a picked file back
// into bytes: a data: URL is decoded here, anything else is fetched.
export async function blobFromUrl(url: string, mediaType: string): Promise<Blob> {
  if (url.startsWith('data:')) {
    const comma = url.indexOf(',')
    const meta = url.slice(5, comma)
    const payload = url.slice(comma + 1)
    const type = meta.split(';')[0] || mediaType
    if (meta.endsWith(';base64')) {
      const binary = atob(payload)
      const bytes = new Uint8Array(binary.length)
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
      return new Blob([bytes], { type })
    }
    return new Blob([decodeURIComponent(payload)], { type })
  }
  return (await fetch(url, { credentials: 'include' })).blob()
}
