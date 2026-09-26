import { useState } from 'react'
import { toast } from 'sonner'
import { useSetAside } from '@/api/branches'
import { errorText } from '@/api/errorText'
import type { MemberBranch } from '@/api/types'
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

export interface SetAsideDialogProps {
  member: MemberBranch
  onClose: () => void
}

// SetAsideDialog asks before a member's branch is reset to the main line
// (docs/design.md 5.21): what it had, committed or not, is archived first
// under refs/veyloom/set-aside/, and the chat's note names the ref.
export function SetAsideDialog({ member, onClose }: SetAsideDialogProps) {
  const t = useT()
  const setAside = useSetAside()
  const [refused, setRefused] = useState<string>()
  const files = member.status?.files?.length ?? 0
  const commits = member.status?.ahead ?? 0

  return (
    <AlertDialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <AlertDialogContent size="sm" aria-describedby={undefined}>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-base">{t('branches.setAsideTitle', { name: member.name })}</AlertDialogTitle>
        </AlertDialogHeader>
        <p className="text-center text-[0.8125rem] leading-relaxed text-muted-foreground">
          {t(commits > 0 ? 'branches.setAsideHintCommits' : 'branches.setAsideHint', { branch: member.branch ?? member.name, files, commits })}
        </p>
        {refused ? (
          <p role="alert" className="text-center text-[0.8125rem] text-status-fail">
            {refused}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={setAside.isPending}
            onClick={(event) => {
              // The action would close the dialog at once; wait for the hub.
              event.preventDefault()
              setAside.mutate(member.member_id, {
                onSuccess: () => {
                  toast.success(t('branches.setAsideDone', { name: member.name }))
                  onClose()
                },
                onError: (err) => setRefused(errorText(err)),
              })
            }}
          >
            {setAside.isPending ? t('branches.settingAside') : t('branches.setAsideNow')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
