import { useState } from 'react'
import type { TranscriptLine } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { runtimeName } from '@/lib/runtimes'
import { formatHour } from '@/lib/format'
import { cn } from '@/lib/utils'
import { describeTool } from './activity'
import { useT } from '@/lib/i18n'
import { decisionNote } from '@/features/approvals/describe'
import { turnErrorText } from './turnError'
import { BriefFold } from './BriefFold'
import { quotaText } from '@/features/machines/quota'
import { noticeText } from '@/features/threads/systemNote'

// How a notice reads by how much it matters (docs/design.md 4.6).
const noticeTones = { info: 'text-muted-foreground', warning: 'text-status-wait', error: 'text-status-fail' } as const

// One line of a turn's record. Long tool output is folded to three lines
// until asked for. past says something came after it, so what it said was
// under way is over.
export function TurnEventRow({ line, past = true }: { line: TranscriptLine; past?: boolean }) {
  const time = line.at ?? line.event?.at
  // The time of day only: the drawer's heading says which day the turn
  // began, and a date in this narrow column breaks over three lines.
  return (
    <li className="grid grid-cols-[2.75rem_minmax(0,1fr)] gap-x-3 py-1.5 text-[0.78125rem] [contain-intrinsic-size:auto_1.75rem] [content-visibility:auto]">
      <time dateTime={time} className="pt-px text-xs text-subtle tabular-nums">
        {time ? formatHour(time) : ''}
      </time>
      <Detail line={line} past={past} />
    </li>
  )
}

function Detail({ line, past }: { line: TranscriptLine; past: boolean }) {
  const event = line.event
  const t = useT()
  switch (line.kind) {
    case 'start':
      return (
        <div className="min-w-0">
          <span className="text-muted-foreground">{t('event.start', { runtime: line.runtime ? runtimeName(line.runtime) : '' })}</span>
          {line.spec?.prompt ? <BriefFold prompt={line.spec.prompt} /> : null}
          {line.spec?.system_prompt ? <BriefFold prompt={line.spec.system_prompt} kind="system" /> : null}
        </div>
      )
    case 'restart':
      // The session would not resume: the turn ran again in a new one, with
      // a brief of its own.
      return (
        <div className="min-w-0">
          <span className="text-muted-foreground">{t('event.restart', { error: turnErrorText(line.error ?? '') })}</span>
          {line.spec?.prompt ? <BriefFold prompt={line.spec.prompt} /> : null}
        </div>
      )
    case 'reply_asked':
      // It said nothing in the chat: asked, in the session it ran in, for
      // the reply it did not give.
      return (
        <div className="min-w-0">
          <span className="text-muted-foreground">{t('event.replyAsked')}</span>
          {line.spec?.prompt ? <BriefFold prompt={line.spec.prompt} kind="ask" /> : null}
        </div>
      )
    case 'done':
      return line.error ? (
        <span className="text-status-fail">{t('event.endError', { error: turnErrorText(line.error) })}</span>
      ) : (
        <span className="text-muted-foreground">{t('event.end')}</span>
      )
    case 'approval_decision':
      return (
        <span className="text-muted-foreground">
          {t('event.approval', { status: line.approval?.status ?? '' })}
          {line.approval?.decided_by ? ` · ${line.approval.decided_by}` : ''}
          {line.approval?.message ? ` · ${decisionNote(line.approval.message)}` : ''}
        </span>
      )
    default:
      break
  }
  if (!event) return <span className="text-subtle">{line.kind}</span>
  switch (event.kind) {
    case 'text':
      return <Folded text={event.text ?? ''} className="text-body" />
    case 'status':
      return <span className="text-subtle">{event.text}</span>
    case 'tool_call':
      // A long path breaks where it must, as the output under it does,
      // rather than run out of the drawer.
      return (
        <span className="min-w-0 font-mono break-words text-foreground" translate="no">
          {describeTool(event.tool ?? '', event.input ?? '')}
        </span>
      )
    case 'tool_result':
      // A call that gave nothing back says so, rather than leave a time
      // with nothing after it.
      return event.text ? <Folded text={event.text} className="font-mono text-muted-foreground" /> : <span className="text-subtle">{t('event.noOutput')}</span>
    case 'notice': {
      // What was said for people as the turn ran, the hub's own in the UI's
      // words; not an error unless it says so.
      const level = event.level === 'warning' || event.level === 'error' ? event.level : 'info'
      return <span className={cn('min-w-0 break-words', noticeTones[level])}>{noticeText(t, event.text ?? '', past)}</span>
    }
    case 'file_changed':
      return (
        <span className="font-mono text-muted-foreground" translate="no">
          {t('event.changed', { path: event.path ?? '' })}
        </span>
      )
    case 'approval_request':
      return (
        <span className="text-status-wait">
          {t('activity.requests')} <span className="font-mono">{describeTool(event.tool ?? '', event.input ?? '')}</span>
        </span>
      )
    case 'steer':
      // What people said in the topic, passed to the turn as it ran.
      return (
        <div className="min-w-0">
          <span className="text-muted-foreground">{t('event.steer')}</span>
          <BriefFold prompt={event.text ?? ''} kind="steer" />
        </div>
      )
    case 'steer_dropped':
      return <span className="text-subtle">{t('event.steerDropped')}</span>
    case 'quota':
      // How the account stands against its usage limits, as the runtime
      // told (docs/design.md 5.23.3).
      return event.quota ? <span className="text-subtle">{quotaText(t, event.quota)}</span> : null
    default:
      return <span className="text-status-fail">{event.text}</span>
  }
}

const foldAfter = 3

// The first lines always show; the rest open and close on a Collapsible.
function Folded({ text, className }: { text: string; className?: string }) {
  const [open, setOpen] = useState(false)
  const t = useT()
  const lines = text.split('\n')
  const style = cn('min-w-0 break-words whitespace-pre-wrap', className)
  if (lines.length <= foldAfter) return <div className={style}>{text}</div>
  return (
    <Collapsible open={open} onOpenChange={setOpen} className={style}>
      {lines.slice(0, foldAfter).join('\n')}
      <CollapsibleContent>{lines.slice(foldAfter).join('\n')}</CollapsibleContent>
      <CollapsibleTrigger asChild>
        <Button variant="ghost" size="xs" className="ml-2 h-5 px-1.5 font-sans text-xs text-subtle hover:text-foreground">
          {open ? t('event.fold') : t('event.more', { n: lines.length - foldAfter })}
        </Button>
      </CollapsibleTrigger>
    </Collapsible>
  )
}
