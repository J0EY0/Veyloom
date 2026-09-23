import { toast } from 'sonner'
import { useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useUpdateProject } from '@/api/projects'
import type { UpdateProjectRequest, UpkeepStatus } from '@/api/types'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { noMaintainer } from '@/features/projects/MaintainerFields'
import { useT } from '@/lib/i18n'
import { TriggerSelect } from './TriggerSelect'

// Changing the wiki's maintainer where a person reads about it, on the
// wiki's overview (docs/design.md 5.16): another member, none, or how
// often. Each change is saved as it is made.
export function MaintainerControls({ projectId, roomId, status }: { projectId: string; roomId: string; status: UpkeepStatus }) {
  const t = useT()
  const members = useRoomMembers(roomId)
  const update = useUpdateProject(projectId)
  const current = status.member_id ?? noMaintainer
  const candidates = (members.data ?? []).filter((member) => member.enabled || member.id === current)

  function change(req: UpdateProjectRequest) {
    update.mutate(req, { onError: (err) => toast.error(t('maintainer.changeFailed', { error: errorText(err) })) })
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select value={current} onValueChange={(value) => change({ wiki_maintainer_member_id: value === noMaintainer ? '' : value })} disabled={update.isPending}>
        <SelectTrigger size="sm" className="w-40 bg-background" aria-label={t('maintainer.title')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={noMaintainer}>{t('maintainer.none')}</SelectItem>
          {candidates.map((member) => (
            <SelectItem key={member.id} value={member.id}>
              {member.display_name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
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
