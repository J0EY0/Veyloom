import { useProject } from '@/api/projects'
import { useRoom } from '@/api/rooms'
import type { Message } from '@/api/types'
import { useThreadRelayHolds } from '@/api/relays'
import { PendingSteps } from '@/features/branches/SetupSteps'
import { useT } from '@/lib/i18n'
import { HoldNote } from './HoldNote'
import { NoteLine } from '@/components/shared/note-line'
import { isHoldNote, systemNote } from './systemNote'

// ThreadNote is a note of the system's inside a topic: a quiet line in the
// UI's words. The setup steps waiting for a person are a card there
// (docs/design.md 5.21), a wake a limit held back offers to go on (5.22),
// and what a failed setup command said is shown as it said it.
export function ThreadNote({ message }: { message: Message }) {
  const t = useT()
  const room = useRoom(message.room_id)
  const project = useProject(room.data?.project_id ?? '')
  const holds = useThreadRelayHolds(message.thread_id ?? '', isHoldNote(message.body))
  const hold = holds.data?.find((h) => h.message_id === message.id)
  if (hold) return <HoldNote message={message} hold={hold} />
  if (project?.workspace_pending && project.workspace_pending_message_id === message.id) {
    return (
      <div className="mt-4 ml-10 first:mt-1">
        <PendingSteps project={project} />
      </div>
    )
  }
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
