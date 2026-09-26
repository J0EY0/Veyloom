import type { InboxItem, PendingApproval } from '@/api/types'
import { approvalCommand } from '@/features/approvals/describe'
import { systemText } from '@/features/threads/systemNote'
import type { t as translate } from '@/lib/i18n'

// One row of the inbox: a request waiting for a decision, or a message that
// mentioned you. Each leads to a topic.
export interface InboxEntry {
  kind: 'approval' | 'mention'
  id: string
  roomId: string
  // The topic, when the record names it. The answer heading a topic (and a
  // closing message of old) sits at the top of the chat and names only its
  // turn; the turn knows the topic.
  threadId?: string
  turnId?: string
  sender: string
  project: string
  // What the row shows under the names, as plain text; search reads it too.
  excerpt: string
  createdAt: string
  // A mention the person has not read; a request is waiting as long as it
  // is listed.
  unread?: boolean
  // Where it stands in the inbox: a mention's seq.
  seq?: number
  // The message itself, for a mention that belongs to no topic.
  message?: InboxItem
}

// toEntries lists what waits for a decision first, the runtimes' requests,
// then the mentions, each in the order the server sent them. A note of
// Veyloom's own is in the UI's words.
export function toEntries(approvals: PendingApproval[], items: InboxItem[], me: string, t: typeof translate): InboxEntry[] {
  return [
    ...approvals.map((approval): InboxEntry => ({
      kind: 'approval',
      id: approval.id,
      roomId: approval.room_id,
      threadId: approval.thread_id,
      turnId: approval.turn_id,
      sender: approval.member_name,
      project: approval.project_name,
      excerpt: approvalCommand(approval),
      createdAt: approval.created_at,
    })),
    ...items.map((item): InboxEntry => ({
      kind: 'mention',
      id: item.id,
      roomId: item.room_id,
      threadId: item.thread_id,
      turnId: item.turn_id,
      sender: item.sender_kind === 'system' ? t('inbox.system') : item.sender_name,
      project: item.project_name,
      excerpt:
        excerptOf(item.sender_kind === 'system' ? systemText(t, item.body) : item.body, me) || (item.attachments ?? []).map((file) => file.filename).join('、'),
      createdAt: item.created_at,
      unread: !item.read,
      seq: item.seq,
      message: item,
    })),
  ]
}

// filterEntries keeps the approvals only when asked, then what matches
// every word of the query in the sender, the project or the excerpt.
export function filterEntries(entries: InboxEntry[], query: string, approvalsOnly: boolean): InboxEntry[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return entries.filter((entry) => {
    if (approvalsOnly && entry.kind === 'mention') return false
    if (words.length === 0) return true
    const text = `${entry.sender} ${entry.project} ${entry.excerpt}`.toLowerCase()
    return words.every((word) => text.includes(word))
  })
}

// excerptOf turns a message into one line for the list: the @ that put it
// here goes (every row has it), and so does the markdown syntax.
export function excerptOf(body: string, me: string): string {
  const lead = `@${me}`
  const addressed = me !== '' && body.startsWith(lead) && (body.length === lead.length || /\s/.test(body[lead.length]))
  const text = addressed ? body.slice(lead.length) : body
  return plainText(text)
}

export function plainText(markdown: string): string {
  return markdown
    .replace(/^\s*(```|~~~).*$/gm, ' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s*(?:[-*+]|\d+[.)])\s+/gm, '')
    .replace(/^\s*[-*_|: ]{3,}\s*$/gm, ' ')
    .replace(/(\*\*|__|~~|`)/g, '')
    .replace(/(^|[\s(])[*_](\S(?:.*?\S)?)[*_](?=[\s).,;:!?，。；：！？]|$)/gm, '$1$2')
    .replace(/\s*\|\s*/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}
