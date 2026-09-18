import { useState } from 'react'
import type { TranscriptLine } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { runtimeName } from '@/lib/runtimes'
import { formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { describeTool } from './activity'
import { useT } from '@/lib/i18n'

// One line of a turn's record. Long tool output is folded to three lines
// until asked for.
export function TurnEventRow({ line }: { line: TranscriptLine }) {
  const time = line.at ?? line.event?.at
  return (
    <li className="grid grid-cols-[44px_minmax(0,1fr)] gap-x-3 py-1.5 text-[0.78125rem] [contain-intrinsic-size:auto_28px] [content-visibility:auto]">
      <time className="pt-px text-xs text-subtle tabular-nums">{time ? formatTime(time) : ''}</time>
      <Detail line={line} />
    </li>
  )
}

function Detail({ line }: { line: TranscriptLine }) {
  const event = line.event
  const t = useT()
  switch (line.kind) {
    case 'start':
      return <span className="text-muted-foreground">{t('event.start', { runtime: line.runtime ? runtimeName(line.runtime) : '' })}</span>
    case 'done':
      return line.error ? (
        <span className="text-status-fail">{t('event.endError', { error: line.error })}</span>
      ) : (
        <span className="text-muted-foreground">{t('event.end')}</span>
      )
    case 'approval_decision':
      return (
        <span className="text-muted-foreground">
          {t('event.approval', { status: line.approval?.status ?? '' })}
          {line.approval?.decided_by ? ` · ${line.approval.decided_by}` : ''}
          {line.approval?.message ? ` · ${line.approval.message}` : ''}
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
      return (
        <span className="font-mono text-foreground" translate="no">
          {describeTool(event.tool ?? '', event.input ?? '')}
        </span>
      )
    case 'tool_result':
      return <Folded text={event.text ?? ''} className="font-mono text-muted-foreground" />
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
