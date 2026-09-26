import { BookHeartIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useRoomMembers } from '@/api/agents'
import { useProject } from '@/api/projects'
import type { UpkeepStatus } from '@/api/types'
import { upkeepBusy, useStartUpkeep, useUpkeepStatus } from '@/api/upkeep'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatTime } from '@/lib/format'
import { useT } from '@/lib/i18n'
import type { OpenTopic } from './CommitRow'
import { errorText } from '@/api/errorText'
import { MaintainerControls } from './MaintainerControls'
import { OfferControls } from './OfferControls'

export interface MaintainerCardProps {
  projectId: string
  roomId: string
  onOpenThread: OpenTopic
}

// The project's wiki maintainer on the wiki's front page (docs/design.md
// 5.12, 5.16, 5.21): who keeps the wiki, the leader unless someone else
// was chosen, and how often, changed right here; how the last upkeep went,
// what waits for the next, and a way to run one now. A project whose
// upkeep is off is offered it, once there is something to go over, until
// a person says no.
export function MaintainerCard({ projectId, roomId, onOpenThread }: MaintainerCardProps) {
  const t = useT()
  const status = useUpkeepStatus(projectId)
  const start = useStartUpkeep(projectId)
  const data = status.data
  if (!data) return null
  if (!data.member_id) return <MaintainerOffer projectId={projectId} roomId={roomId} status={data} />
  const busy = upkeepBusy(data)
  const name = data.member_name ?? ''

  function run() {
    start.mutate(undefined, {
      onSuccess: () => toast.success(t('maintainer.started')),
      onError: (err) => toast.error(t('maintainer.runFailed', { error: errorText(err) })),
    })
  }

  const runButton = (
    <Button variant="outline" size="xs" disabled={busy || start.isPending} onClick={run}>
      {t('maintainer.run')}
    </Button>
  )
  return (
    <section aria-labelledby="wiki-maintainer" className="flex flex-col gap-2 rounded-[10px] bg-muted px-3.5 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <BookHeartIcon className="size-4 flex-none text-subtle" aria-hidden="true" />
        <h2 id="wiki-maintainer" className="min-w-0 text-sm font-semibold break-words">
          {t(data.leader ? 'maintainer.keepsLeader' : 'maintainer.keeps', { name })}
        </h2>
      </div>
      <MaintainerControls projectId={projectId} roomId={roomId} status={data} />
      <p className="text-[0.8125rem] text-muted-foreground">
        <LastUpkeep status={data} />
        <span aria-hidden="true"> · </span>
        <WaitingLine status={data} />
      </p>
      <div className="flex flex-wrap items-center gap-2">
        {busy ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span tabIndex={0}>{runButton}</span>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t('maintainer.runBusy')}</TooltipContent>
          </Tooltip>
        ) : (
          runButton
        )}
        {data.thread_id ? (
          <Button variant="ghost" size="xs" className="text-muted-foreground" onClick={() => onOpenThread(data.thread_id as string, roomId)}>
            {t('maintainer.openTopic')}
          </Button>
        ) : null}
      </div>
    </section>
  )
}

// LastUpkeep says how the latest upkeep stands.
function LastUpkeep({ status }: { status: UpkeepStatus }) {
  const t = useT()
  const last = status.last
  if (status.queued) return <>{t('maintainer.queued', { name: status.member_name ?? '' })}</>
  if (!last) return <>{t('maintainer.never')}</>
  if (last.status === 'running') return <span className="text-status-run">{t('maintainer.running')}</span>
  const when = formatTime(last.ended_at ?? last.started_at)
  if (last.status === 'done') return <>{t('maintainer.last', { when })}</>
  return <span className="text-status-fail">{t('maintainer.lastFailed', { when })}</span>
}

// MaintainerOffer asks, once there is something to go over, whether one of
// the members should keep the wiki, until a person says no (docs/design.md
// 5.16); the chat asks the same on a card of its own.
function MaintainerOffer({ projectId, roomId, status }: { projectId: string; roomId: string; status: UpkeepStatus }) {
  const t = useT()
  const project = useProject(projectId)
  const members = useRoomMembers(roomId)
  if (project?.wiki_offer_declined_at || status.waiting.own === 0 || !(members.data ?? []).some((member) => member.enabled)) return null
  return (
    <section aria-labelledby="wiki-maintainer-offer" className="flex flex-col gap-2 rounded-[10px] bg-muted px-3.5 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <BookHeartIcon className="size-4 flex-none text-subtle" aria-hidden="true" />
        <h2 id="wiki-maintainer-offer" className="text-sm font-semibold">
          {t('maintainer.pitchTitle')}
        </h2>
      </div>
      <p className="text-[0.8125rem] leading-relaxed text-muted-foreground">{t('maintainer.pitch')}</p>
      <OfferControls projectId={projectId} roomId={roomId} />
    </section>
  )
}

// WaitingLine says what the next upkeep goes over: turns of this chat and
// of other projects using the team's skills, and what people said.
function WaitingLine({ status }: { status: UpkeepStatus }) {
  const t = useT()
  const { own, uses } = status.waiting
  const people = status.waiting.people ?? 0
  if (own + uses + people === 0) return <>{t('maintainer.nothingWaiting')}</>
  return (
    <>
      {t('maintainer.waiting', { own })}
      {uses > 0 ? t('maintainer.waitingUses', { uses }) : ''}
      {people > 0 ? t('maintainer.waitingPeople', { people }) : ''}
    </>
  )
}
