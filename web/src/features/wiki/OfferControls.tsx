import { useState } from 'react'
import { toast } from 'sonner'
import { useAgents, useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useProject, useUpdateProject } from '@/api/projects'
import { readOnlyCodex } from '@/api/upkeep'
import { Button } from '@/components/ui/button'
import { byLeader, KeeperSelect } from '@/features/projects/MaintainerFields'
import { useT } from '@/lib/i18n'

// Turning the wiki's upkeep on, daily, kept by the leader unless another
// member is picked, or saying no (docs/design.md 5.16, 5.21), wherever it
// is offered: on the wiki's overview, or as a card in the chat. A no is
// kept on the project, so every place of the offer follows it.
export function OfferControls({ projectId, roomId }: { projectId: string; roomId: string }) {
  const t = useT()
  const project = useProject(projectId)
  const members = useRoomMembers(roomId)
  const agents = useAgents()
  const update = useUpdateProject(projectId)
  const [picked, setPicked] = useState(byLeader)
  const candidates = (members.data ?? []).filter((member) => member.enabled)
  const leader = (members.data ?? []).find((member) => member.id === project?.leader_id)
  const keeper = picked === byLeader ? leader : candidates.find((m) => m.id === picked)

  function enable() {
    if (!keeper) return
    update.mutate(
      { wiki_upkeep: true, wiki_maintainer_member_id: picked === byLeader ? '' : keeper.id, wiki_maintainer_trigger: 'daily' },
      {
        onSuccess: () => toast.success(t('maintainer.enabled', { name: keeper.display_name })),
        onError: (err) => toast.error(t('maintainer.enableFailed', { error: errorText(err) })),
      },
    )
  }

  function decline() {
    update.mutate({ wiki_offer_declined: true }, { onError: (err) => toast.error(t('maintainer.declineFailed', { error: errorText(err) })) })
  }

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <KeeperSelect
          size="sm"
          className="w-44 bg-background"
          label={t('maintainer.who')}
          value={picked}
          leader={leader}
          candidates={candidates}
          onChange={setPicked}
        />
        <Button size="sm" disabled={!keeper || update.isPending} onClick={enable}>
          {t('maintainer.enable')}
        </Button>
        <Button variant="ghost" size="sm" className="text-muted-foreground" disabled={update.isPending} onClick={decline}>
          {t('maintainer.decline')}
        </Button>
      </div>
      {readOnlyCodex(keeper, agents.data) ? <p className="text-xs text-status-wait">{t('maintainer.readOnlyCodex')}</p> : null}
    </>
  )
}
