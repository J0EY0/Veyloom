// Wire types for a person's inbox (docs/webui.md 4.19); see types.ts.

import type { Message } from './types'

// A message that mentions the current user, as the inbox lists it.
export interface InboxItem extends Message {
  room_name: string
  project_name: string
  sender_name: string
  // The person read it: in the inbox, in its topic, or all at once.
  read: boolean
}

// A page of the inbox, and how many of all the mentions are unread.
export interface InboxResponse {
  items: InboxItem[]
  unread: number
}

// What of the inbox to mark read: the messages named, those in a topic,
// or all up to a seq.
export interface InboxRead {
  message_ids?: string[]
  thread_id?: string
  up_to?: number
}

export interface InboxReadResponse {
  marked: number
  unread: number
}
