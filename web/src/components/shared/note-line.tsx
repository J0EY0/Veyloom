import {
  BookOpenIcon,
  CircleXIcon,
  CornerUpLeftIcon,
  GitCommitHorizontalIcon,
  GitMergeIcon,
  InfoIcon,
  ListChecksIcon,
  PauseIcon,
  RotateCcwIcon,
  TriangleAlertIcon,
  WrenchIcon,
  type LucideIcon,
} from 'lucide-react'
import type { ReactNode } from 'react'
import type { Note, NoteKind } from '@/features/threads/systemNote'
import { formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'

type Tone = 'idle' | 'ok' | 'wait' | 'fail'

// Each kind of note by its mark and the colour of what it asks of a person
// (docs/webui.md §4.1): news is quiet, a wait or a risk is the wait colour.
const kinds: Record<NoteKind, { icon: LucideIcon; tone: Tone }> = {
  setup: { icon: WrenchIcon, tone: 'idle' },
  steps: { icon: ListChecksIcon, tone: 'wait' },
  merged: { icon: GitMergeIcon, tone: 'ok' },
  committed: { icon: GitCommitHorizontalIcon, tone: 'ok' },
  setAside: { icon: RotateCcwIcon, tone: 'idle' },
  overlap: { icon: TriangleAlertIcon, tone: 'wait' },
  hold: { icon: PauseIcon, tone: 'wait' },
  sumUp: { icon: CornerUpLeftIcon, tone: 'ok' },
  concluded: { icon: GitMergeIcon, tone: 'ok' },
  unresolved: { icon: TriangleAlertIcon, tone: 'fail' },
  failed: { icon: CircleXIcon, tone: 'fail' },
  upkeep: { icon: BookOpenIcon, tone: 'idle' },
  other: { icon: InfoIcon, tone: 'idle' },
}

const tones: Record<Tone, string> = {
  idle: 'border-border text-muted-foreground',
  ok: 'border-status-ok/30 bg-status-ok/8 text-status-ok',
  wait: 'border-status-wait/35 bg-status-wait/8 text-status-wait',
  fail: 'border-status-fail/35 bg-status-fail/8 text-status-fail',
}

export interface NoteLineProps {
  note: Note
  // When the note was written, at the end of the line.
  time?: string
  // What a person does about it, before the time.
  children?: ReactNode
  // Let go of: drawn quieter, whatever its kind.
  settled?: boolean
  className?: string
}

// A note of the system's in the chat or a topic: one line, its mark in the
// column the faces take, the names it is about brought forward.
export function NoteLine({ note, time, children, settled, className }: NoteLineProps) {
  const { icon: Icon, tone } = kinds[note.kind]
  return (
    <div className={cn('flex items-center gap-3 text-[0.78125rem] leading-snug text-muted-foreground', className)}>
      <span className="flex w-7 flex-none justify-center">
        <span className={cn('flex size-5.5 items-center justify-center rounded-full border', tones[settled ? 'idle' : tone])}>
          <Icon className="size-3" aria-hidden="true" />
        </span>
      </span>
      <span className="min-w-0 flex-1 break-words">{emphasised(note.text, note.who)}</span>
      {children}
      {time ? (
        <time dateTime={time} className="flex-none text-xs text-subtle tabular-nums">
          {formatTime(time)}
        </time>
      ) : null}
    </div>
  )
}

// emphasised brings forward the first place each name is written.
function emphasised(text: string, names: string[]): ReactNode {
  const marks = names
    .filter((name) => name !== '')
    .map((name) => ({ name, at: text.indexOf(name) }))
    .filter((mark) => mark.at >= 0)
    .sort((a, b) => a.at - b.at)
  if (marks.length === 0) return text
  const parts: ReactNode[] = []
  let from = 0
  for (const { name, at } of marks) {
    if (at < from) continue
    parts.push(
      text.slice(from, at),
      <b key={at} className="font-medium text-foreground">
        {name}
      </b>,
    )
    from = at + name.length
  }
  parts.push(text.slice(from))
  return parts
}
