import { api } from './client'
import type { Attachment, AttachmentResponse } from './types'

// Where an attachment's bytes are served from.
export function attachmentUrl(id: string): string {
  return `/api/v1/attachments/${id}`
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
