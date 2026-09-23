import { useRemoveMember } from '@/api/agents'
import { ApiError } from '@/api/client'
import type { Member } from '@/api/types'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

export interface RemoveMemberDialogProps {
  roomId: string
  roomName: string
  member: Member
  onClose: () => void
}

// Asks before taking a member out of the project. What it said stays, under
// its name. The menu already refuses while it works; the hub has the last
// word, since a turn can start after the menu was opened.
export function RemoveMemberDialog({ roomId, roomName, member, onClose }: RemoveMemberDialogProps) {
  const remove = useRemoveMember(roomId)
  const t = useT()
  const busy = remove.error instanceof ApiError && remove.error.status === 409

  return (
    <AlertDialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent size="sm" aria-describedby={undefined}>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('member.removeTitle', { name: member.display_name, room: roomName })}</AlertDialogTitle>
        </AlertDialogHeader>
        {remove.error ? (
          <p role="alert" className="text-center text-[0.8125rem] leading-relaxed text-status-fail">
            {busy ? t('member.removeBusyError') : errorText(remove.error)}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              // The action would close the dialog at once; wait for the hub.
              event.preventDefault()
              remove.mutate(member.id, { onSuccess: onClose })
            }}
          >
            {remove.isPending ? t('member.removing') : t('member.removeConfirm')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
