// Wire types for attachments (docs/webui.md 4.21); see types.ts.

import type { SenderKind } from './types'

// What the chat draws an attachment as.
export type AttachmentKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'office' | 'archive' | 'other'

// A file attached to a message. The bytes are at attachmentUrl(id).
export interface Attachment {
  id: string
  room_id: string
  message_id?: string
  filename: string
  media_type: string
  kind: AttachmentKind
  size: number
  // A picture's size in pixels as a browser shows it; absent when unknown.
  width?: number
  height?: number
  // A smaller copy is served at thumbnailUrl(id).
  thumbnail?: boolean
  created_at: string
}

export interface AttachmentResponse {
  attachment: Attachment
}

// An attachment as the attachments tab lists it: the file, and the message
// that carried it.
export interface RoomAttachment extends Attachment {
  // The topic the message is in; absent for the room itself.
  thread_id?: string
  thread_number?: number
  sender_kind: SenderKind
  user_id?: string
  member_id?: string
  sender_name: string
  // Where the message is in the chat, for finding it again.
  message_seq: number
  // What the message said, in a line.
  said?: string
}

export interface RoomAttachmentsResponse {
  attachments: RoomAttachment[]
  total: number
}

// How the attachments tab sorts.
export type AttachmentSort = 'newest' | 'oldest' | 'size' | 'name'
