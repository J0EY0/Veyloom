import { toast } from 'sonner'
import { useAgents, useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useProject, useUpdateProject } from '@/api/projects'
import { useRuntimeTraits } from '@/api/runtimes'
import type { UpdateProjectRequest, UpkeepStatus } from '@/api/types'
import { keepsNoWiki } from '@/api/upkeep'
import { byLeader, KeeperSelect, upkeepOff } from '@/features/projects/MaintainerFields'
import { useT } from '@/lib/i18n'
import { TriggerSelect } from './TriggerSelect'

// Changing the wiki's maintainer where a person reads about it, on the
// wiki's overview (docs/design.md 5.16, 5.21): the leader, another member,
// nobody, which turns upkeep off, or how often. Each change is saved as it
// is made.
export function MaintainerControls({ projectId, roomId, status }: { projectId: string; roomId: string; status: UpkeepStatus }) {
  const t = useT()
  const project = useProject(projectId)
  const members = useRoomMembers(roomId)
  const agents = useAgents()
  const traits = useRuntimeTraits()
  const update = useUpdateProject(projectId)
  const current = project?.wiki_maintainer_member_id || byLeader
  const candidates = (members.data ?? []).filter((member) => member.enabled || member.id === current)
  const leader = (members.data ?? []).find((member) => member.id === project?.leader_id)
  const canKeep = (value: string) => !keepsNoWiki(value === byLeader ? leader : candidates.find((m) => m.id === value), agents.data, traits.data)

  function change(req: UpdateProjectRequest) {
    update.mutate(req, { onError: (err) => toast.error(t('maintainer.changeFailed', { error: errorText(err) })) })
  }

  function keep(value: string) {
    if (value === upkeepOff) change({ wiki_upkeep: false })
    else change({ wiki_maintainer_member_id: value === byLeader ? '' : value })
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <KeeperSelect
        size="sm"
        className="w-44 bg-background"
        label={t('maintainer.title')}
        value={current}
        leader={leader}
        candidates={candidates}
        offLabel={t('maintainer.none')}
        canKeep={canKeep}
        onChange={keep}
        disabled={update.isPending}
      />
      <TriggerSelect
        size="sm"
        className="w-48 bg-background"
        label={t('maintainer.trigger')}
        value={status.trigger}
        idleMinutes={status.idle_minutes}
        onChange={(trigger) => change({ wiki_maintainer_trigger: trigger })}
        disabled={update.isPending}
      />
    </div>
  )
}
