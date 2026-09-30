// What was typed into an input box and not sent, kept by where it was going
// (the chat, or a topic): the box goes away with the chat's other tabs or
// another chat, and finds it again when it comes back. Kept as long as the
// page is open, as IM clients keep a conversation's draft.

const drafts = new Map<string, string>()

// draftKey names where a box sends to: a topic, else the room.
export function draftKey(roomId: string, threadId?: string): string {
  return threadId ? `thread:${threadId}` : `room:${roomId}`
}

export function readDraft(key: string): string {
  return drafts.get(key) ?? ''
}

// writeDraft keeps text for key; nothing left, nothing kept.
export function writeDraft(key: string, text: string) {
  if (text === '') drafts.delete(key)
  else drafts.set(key, text)
}

// clearDrafts forgets them all, for tests, which share one page.
export function clearDrafts() {
  drafts.clear()
}
