import { ShieldCheckIcon } from 'lucide-react'
import { usePendingApprovals } from '@/api/approvals'
import { SidePanel } from '@/components/layout/SidePanel'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { useMentionTargets } from '@/features/rooms/useMentionTargets'
import { ApprovalCard } from './ApprovalCard'
import { useT } from '@/lib/i18n'

export interface ApprovalsPanelProps {
  roomId: string
  onClose: () => void
  onOpenThread: (threadId: string) => void
}

// Everything in the room waiting for a person, oldest first, each with a
// way into the topic it belongs to.
export function ApprovalsPanel({ roomId, onClose, onOpenThread }: ApprovalsPanelProps) {
  const pending = usePendingApprovals(roomId)
  const { names } = useMentionTargets(roomId)
  const t = useT()

  return (
    <SidePanel
      label={t('approvals.title')}
      header={
        <>
          <h2 className="text-sm font-semibold">{t('approvals.title')}</h2>
          {pending.data?.length ? <span className="text-xs text-status-wait tabular-nums">{pending.data.length}</span> : null}
        </>
      }
      onClose={onClose}
      closeLabel={t('approvals.close')}
    >
      {pending.isPending ? (
        <p role="status" className="flex items-center gap-2 py-2 text-xs text-subtle">
          <Spinner className="size-3" />
          {t('common.loading')}
        </p>
      ) : pending.isError ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{t('approvals.failed')}</EmptyTitle>
            <EmptyDescription>{pending.error.message}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : pending.data.length === 0 ? (
        <Empty className="h-full">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ShieldCheckIcon />
            </EmptyMedia>
            <EmptyTitle>{t('approvals.empty')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        pending.data.map((approval) => (
          <div key={approval.id}>
            <ApprovalCard approval={approval} memberName={names.get(approval.member_id) ?? 'agent'} names={names} />
            <Button variant="ghost" size="xs" onClick={() => onOpenThread(approval.thread_id)} className="mt-1 text-subtle hover:text-foreground">
              {t('approvals.openTopic')}
            </Button>
          </div>
        ))
      )}
    </SidePanel>
  )
}
