import { useTurnApprovals } from '@/api/approvals'
import { useTranscript, useTranscriptSoFar } from '@/api/transcript'
import { useCancelTurn } from '@/api/turns'
import type { Message, Turn } from '@/api/types'
import { Shimmer } from '@/components/ai-elements/shimmer'
import { AgentMarkdown } from '@/components/shared/agent-markdown'
import { askKind, waitingKeys } from '@/features/approvals/kinds'
import { Button } from '@/components/ui/button'
import type { Sender } from '@/features/rooms/useSenderNames'
import { ActivityBlock } from '@/features/turns/ActivityBlock'
import { activityFromEvents, activityFromTranscript, isNotice, withApprovals } from '@/features/turns/activity'
import { NoticeList } from '@/features/turns/NoticeList'
import { formatDuration } from '@/lib/format'
import { useT } from '@/lib/i18n'
import { useLiveTurn } from '@/lib/liveTurns'
import { ThreadMessage } from './ThreadMessage'
import { ThreadRow } from './ThreadRow'

export interface TurnPartProps {
  turn: Turn
  // What the turn said in this stretch of the topic, in order.
  messages: Message[]
  // The turn's member, who is speaking.
  who: Sender
  // A person writing while a turn runs splits it in two stretches: the
  // first carries what the turn did, the last the words still arriving.
  first: boolean
  last: boolean
  names?: Map<string, string>
  onOpenTurn?: (turnId: string) => void
  target?: string
  // The member whose message woke the turn, when an agent did
  // (docs/design.md 5.22).
  wokenBy?: string
}

// One agent's answer inside a topic, drawn as a chat message (docs/webui.md
// §4.2): its face and name, one quiet line for the tools it used, what it
// said, and while the turn runs the words arriving with a way to stop it.
export function TurnPart({ turn, messages, who, first, last, names, onOpenTurn, target, wokenBy }: TurnPartProps) {
  const t = useT()
  const running = turn.status === 'running'
  const approvals = useTurnApprovals(turn.id)
  const approvalByNote = new Map((approvals.data ?? []).flatMap((a) => (a.message_id ? [[a.message_id, a] as const] : [])))
  const live = useLiveTurn(running ? turn.id : undefined)
  // What the turn did before the page listened is read in under the live
  // events, so its steps are all counted.
  useTranscriptSoFar(turn.id, first && running)
  const transcript = useTranscript(turn.id, first && !running)
  const cancel = useCancelTurn()
  const activity = running ? withApprovals(activityFromEvents(live?.events ?? []), approvals.data ?? []) : activityFromTranscript(transcript.data ?? [])
  // What the runtime told people stays in sight; the rest folds into a line.
  const notices = activity.filter(isNotice)
  const items = activity.filter((item) => !isNotice(item))
  const took = turn.ended_at ? formatDuration(turn.started_at, turn.ended_at) : undefined
  const waiting = (approvals.data ?? []).find((a) => a.status === 'pending')

  return (
    <ThreadRow
      sender={who}
      time={messages[0]?.created_at ?? turn.started_at}
      label={turn.kind === 'upkeep' ? t('upkeep.badge') : turn.kind === 'setup' ? t('setup.badge') : wokenBy ? t('turn.wokenBy', { name: wokenBy }) : undefined}
    >
      {first ? <ActivityBlock items={items} duration={took} live={running} onOpen={onOpenTurn ? () => onOpenTurn(turn.id) : undefined} /> : null}
      {first ? <NoticeList notices={notices} /> : null}
      {messages.map((message) => (
        <ThreadMessage key={message.id} message={message} sender={who} approval={approvalByNote.get(message.id)} names={names} target={target} inTurn />
      ))}
      {running && last ? (
        <div className="mt-0.5 flex items-start gap-2">
          <div className="min-w-0 flex-1 text-[0.875rem] leading-[1.6] break-words">
            {live?.text ? (
              <AgentMarkdown text={live.text} mentions={null} names={names ?? new Map()} streaming className="text-[0.875rem]" />
            ) : (
              <Shimmer as="span" className="text-[0.875rem]">
                {waiting
                  ? t(waitingKeys[askKind(waiting.kind)].turn)
                  : live?.compacting
                    ? t('turn.compacting')
                    : live?.tool
                      ? t('topic.running', { tool: live.tool })
                      : t('turn.typing')}
              </Shimmer>
            )}
          </div>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => cancel.mutate({ turnId: turn.id })}
            disabled={cancel.isPending}
            className="-mr-1.5 flex-none text-subtle hover:text-foreground"
          >
            {cancel.isPending ? t('turn.cancelling') : t('turn.cancel')}
          </Button>
        </div>
      ) : null}
    </ThreadRow>
  )
}
