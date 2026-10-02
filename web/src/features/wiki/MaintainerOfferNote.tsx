import { useProject } from '@/api/projects'
import { useUpkeepStatus } from '@/api/upkeep'
import { useT } from '@/lib/i18n'
import { OfferControls } from './OfferControls'
import { triggerWhenKeys } from './TriggerSelect'

// The note in the chat that offered the project a wiki maintainer
// (docs/design.md 5.16), drawn as a card: the offer until a person answers
// it, then what they chose.
export function MaintainerOfferNote({ projectId, roomId }: { projectId: string; roomId: string }) {
  const t = useT()
  const project = useProject(projectId)
  const status = useUpkeepStatus(projectId).data
  if (status?.member_id) {
    return (
      <div className="min-w-0">
        <div className="mb-1 text-[0.8125rem] leading-tight font-medium text-foreground">
          {t(status.leader ? 'maintainer.keepsLeader' : 'maintainer.keeps', { name: status.member_name ?? '' })}
        </div>
        <p className="text-[0.8125rem] text-muted-foreground">{t(triggerWhenKeys[status.trigger], { n: status.idle_minutes })}</p>
      </div>
    )
  }
  if (project?.wiki_offer_declined_at) {
    return <p className="min-w-0 text-[0.8125rem] text-muted-foreground">{t('maintainer.declined')}</p>
  }
  const waiting = status?.waiting.own ?? 0
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="text-[0.8125rem] leading-tight font-medium text-foreground">{t('maintainer.pitchTitle')}</div>
      <p className="text-[0.8125rem] leading-relaxed text-muted-foreground">
        {waiting > 0 ? `${t('maintainer.offerWaiting', { n: waiting })} ` : ''}
        {t('maintainer.pitch')}
      </p>
      <OfferControls projectId={projectId} roomId={roomId} />
    </div>
  )
}
