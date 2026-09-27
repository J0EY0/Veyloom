import type { Message } from '@/api/types'
import { useThreadRelayHolds } from '@/api/relays'
import { useThreadReminders } from '@/api/reminders'
import { useThreadDrafts } from '@/api/drafts'
import { useT } from '@/lib/i18n'
import { HoldNote } from './HoldNote'
import { ReminderNote } from './ReminderNote'
import { DraftCard } from './DraftCard'
import { NoteLine } from '@/components/shared/note-line'
import { isDraftNote } from './draftNote'
import { isHoldNote, isReminderNote, systemNote } from './systemNote'

// ThreadNote is a note of the system's inside a topic: a quiet line in the
// UI's words. A draft is a card a person runs (docs/design.md 5.23.5), the
// leader's setup steps waiting for a person among them (5.21); a wake a
// limit held back offers to go on (5.22), a reminder not yet due offers to
// take it back (5.23.4), and what a failed setup command said is shown as
// it said it.
export function ThreadNote({ message }: { message: Message }) {
  const t = useT()
  const holds = useThreadRelayHolds(message.thread_id ?? '', isHoldNote(message.body))
  const hold = holds.data?.find((h) => h.message_id === message.id)
  const reminders = useThreadReminders(message.thread_id ?? '', isReminderNote(message.body))
  const reminder = reminders.data?.find((r) => r.set_message_id === message.id)
  const drafts = useThreadDrafts(message.thread_id ?? '', isDraftNote(message.body))
  const draft = drafts.data?.find((d) => d.message_id === message.id)
  if (hold) return <HoldNote message={message} hold={hold} />
  if (reminder) return <ReminderNote message={message} reminder={reminder} />
  if (draft) return <DraftCard message={message} draft={draft} />
  const output = /```\n([\s\S]*?)\n```/.exec(message.body)
  return (
    <div className="mt-4 first:mt-1">
      <NoteLine note={systemNote(t, message.body)} time={message.created_at} />
      {output ? (
        <pre className="mt-1.5 ml-10 max-h-60 overflow-auto rounded-md bg-muted px-2.5 py-2 font-mono text-[0.71875rem] leading-relaxed whitespace-pre-wrap text-muted-foreground">
          {output[1]}
        </pre>
      ) : null}
    </div>
  )
}
