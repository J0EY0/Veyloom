import { useRoomMembers } from '@/api/agents'
import { useTranscript } from '@/api/transcript'
import { useCancelTurn, useTurn } from '@/api/turns'
import type { TranscriptLine } from '@/api/types'
import { StatusDot, type StatusTone } from '@/components/shared/status-dot'
import { TokenCount } from '@/components/shared/token-count'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { formatDuration, formatFullTime } from '@/lib/format'
import { useLiveTurn } from '@/lib/liveTurns'
import { totalTokens } from '@/lib/tokens'
import { TurnEventRow } from './TurnEventRow'
import { useT } from '@/lib/i18n'
import type { MessageKey } from '@/i18n/zh-CN'

export interface TurnDrawerProps {
  roomId: string
  // Empty when closed.
  turnId: string
  onClose: () => void
}

const statusKey = {
  running: { tone: 'run', key: 'turn.running' },
  done: { tone: 'ok', key: 'turn.done' },
  failed: { tone: 'fail', key: 'turn.failed' },
  cancelled: { tone: 'idle', key: 'turn.cancelled' },
} as const satisfies Record<string, { tone: StatusTone; key: MessageKey }>

// Everything one turn did, event by event, in a sheet over the room
// (docs/webui.md §4.6). A running turn shows what has arrived since the
// room was opened; a finished one reads its transcript.
export function TurnDrawer({ roomId, turnId, onClose }: TurnDrawerProps) {
  const t = useT()
  return (
    <Sheet open={turnId !== ''} onOpenChange={(open) => (open ? undefined : onClose())}>
      <SheetContent side="right" aria-label={t('drawer.label')} aria-describedby={undefined} className="w-140 gap-0 sm:max-w-140" showCloseButton>
        {turnId ? <DrawerBody roomId={roomId} turnId={turnId} /> : null}
      </SheetContent>
    </Sheet>
  )
}

function DrawerBody({ roomId, turnId }: { roomId: string; turnId: string }) {
  const turn = useTurn(turnId)
  const members = useRoomMembers(roomId, { removed: true })
  const live = useLiveTurn(turnId)
  const running = turn.data?.status === 'running'
  const transcript = useTranscript(turnId, turn.data !== undefined && !running)
  const cancel = useCancelTurn()
  const t = useT()

  if (turn.isPending) {
    return (
      <SheetHeader>
        <SheetTitle className="flex items-center gap-2">
          <Spinner className="size-3.5" />
          {t('common.loading')}
        </SheetTitle>
      </SheetHeader>
    )
  }
  if (turn.isError) {
    return (
      <>
        <SheetHeader>
          <SheetTitle>{t('drawer.label')}</SheetTitle>
        </SheetHeader>
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('drawer.failed')}</EmptyTitle>
            <EmptyDescription>{turn.error.message}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </>
    )
  }

  const record = turn.data
  const member = members.data?.find((m) => m.id === record.member_id)
  const known = record.status in statusKey ? statusKey[record.status as keyof typeof statusKey] : undefined
  const status = { tone: known?.tone ?? ('idle' as StatusTone), text: known ? t(known.key) : record.status }
  const lines: TranscriptLine[] = running ? (live?.events ?? []).map((event) => ({ kind: 'event', at: event.at, event })) : (transcript.data ?? [])

  return (
    <>
      <SheetHeader className="pr-12">
        <SheetTitle>{t('drawer.label')}</SheetTitle>
      </SheetHeader>
      <div className="flex flex-none flex-wrap items-center gap-x-3 gap-y-1 px-4 pb-3 text-[0.78125rem] text-muted-foreground">
        <span className="font-medium text-foreground">{member?.display_name ?? 'agent'}</span>
        <span className="inline-flex items-center gap-[0.4375rem]">
          <StatusDot tone={status.tone} />
          {status.text}
        </span>
        <span>{t('drawer.startedAt', { time: formatFullTime(record.started_at) })}</span>
        {record.ended_at ? <span>{t('drawer.took', { duration: formatDuration(record.started_at, record.ended_at) })}</span> : null}
        {totalTokens(record.usage) > 0 ? <TokenCount usage={record.usage} /> : null}
        <span className="grow" />
        {running ? (
          <Button variant="outline" size="xs" onClick={() => cancel.mutate(turnId)} disabled={cancel.isPending}>
            {cancel.isPending ? t('turn.cancelling') : t('drawer.cancel')}
          </Button>
        ) : null}
      </div>
      {record.error ? <p className="px-4 pb-2 text-[0.78125rem] text-status-fail">{record.error}</p> : null}
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pb-4">
        {transcript.isPending && !running ? (
          <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
            <Spinner className="size-3" />
            {t('drawer.loadingTranscript')}
          </p>
        ) : transcript.isError ? (
          <p role="alert" className="py-2 text-xs text-status-fail">
            {t('drawer.transcriptFailed', { error: transcript.error.message })}
          </p>
        ) : lines.length === 0 ? (
          <p className="py-2 text-xs text-subtle">{running ? t('drawer.noEventsYet') : t('drawer.noRecord')}</p>
        ) : (
          <ol>
            {lines.map((line, index) => (
              <TurnEventRow key={index} line={line} />
            ))}
          </ol>
        )}
      </div>
    </>
  )
}
