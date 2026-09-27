import { useSyncExternalStore } from 'react'
import type { AttachmentFilter } from '@/api/attachments'
import type { Attachment } from '@/api/types'

// What the attachment viewer shows (docs/webui.md 4.21): a module store,
// so a message, the attachments tab or the search can open the viewer the
// shell renders.

export interface ViewerTarget {
  roomId: string
  attachment: Attachment
  // The topic the message carrying it is in; absent for the room itself.
  threadId?: string
  // The attachments tab's list it was opened from, walked in the tab's
  // order; absent, every attachment of the room, oldest to newest as the
  // chat reads.
  filter?: AttachmentFilter
}

let target: ViewerTarget | null = null
const listeners = new Set<() => void>()

function set(next: ViewerTarget | null) {
  target = next
  listeners.forEach((listener) => listener())
}

export function openViewer(next: ViewerTarget) {
  set(next)
}

export function closeViewer() {
  set(null)
}

function get() {
  return target
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useViewerTarget(): ViewerTarget | null {
  return useSyncExternalStore(subscribe, get, get)
}
