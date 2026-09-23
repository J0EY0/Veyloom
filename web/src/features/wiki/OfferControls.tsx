import { useState } from 'react'
import { toast } from 'sonner'
import { useAgents, useRoomMembers } from '@/api/agents'
import { errorText } from '@/api/errorText'
import { useUpdateProject } from '@/api/projects'
import { readOnlyCodex } from '@/api/upkeep'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useT } from '@/lib/i18n'

// Picking the member who keeps the wiki, daily, or saying no (docs/design.md
// 5.16), wherever a maintainer is offered: on the wiki's overview, or as a
// card in the chat. A no is kept on the project, so every place of the
// offer follows it.
export function OfferControls({ projectId, roomId }: { projectId: string; roomId: string }) {
  const t = useT()
  const members = useRoomMembers(roomId)
  const agents = useAgents()
  const update = useUpdateProject(projectId)
  const [picked, setPicked] = useState('')
  const candidates = (members.data ?? []).filter((member) => member.enabled)

  function enable() {
    const member = candidates.find((m) => m.id === picked)
    if (!member) return
    update.mutate(
      { wiki_maintainer_member_id: member.id, wiki_maintainer_trigger: 'daily' },
      {
        onSuccess: () => toast.success(t('maintainer.enabled', { name: member.display_name })),
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
        <Select value={picked} onValueChange={setPicked}>
          <SelectTrigger size="sm" className="w-44 bg-background" aria-label={t('maintainer.pick')}>
            <SelectValue placeholder={t('maintainer.pick')} />
          </SelectTrigger>
          <SelectContent>
            {candidates.map((member) => (
              <SelectItem key={member.id} value={member.id}>
                {member.display_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" disabled={picked === '' || update.isPending} onClick={enable}>
          {t('maintainer.enable')}
        </Button>
        <Button variant="ghost" size="sm" className="text-muted-foreground" disabled={update.isPending} onClick={decline}>
          {t('maintainer.decline')}
        </Button>
      </div>
      {readOnlyCodex(
        candidates.find((m) => m.id === picked),
        agents.data,
      ) ? (
        <p className="text-xs text-status-wait">{t('maintainer.readOnlyCodex')}</p>
      ) : null}
    </>
  )
}
