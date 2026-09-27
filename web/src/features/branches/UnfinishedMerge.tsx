import { useState } from 'react'
import { toast } from 'sonner'
import { useAbortMerge } from '@/api/branches'
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
import { Button } from '@/components/ui/button'
import { useT } from '@/lib/i18n'
import { useHandOver } from './handOver'

export interface UnfinishedMergeProps {
  roomId: string
  member: MemberBranch
  // The main line's branch, the one the member was merging in.
  branch: string
  // After handing it back, or giving it up.
  onDone?: () => void
}

// UnfinishedMerge is what a person does about a merge a member left under
// way in its worktree (docs/design.md 5.21): hand it back to the member,
// naming the files that still have conflict markers, or give the merge up,
// the worktree as it was before it began.
export function UnfinishedMerge({ roomId, member, branch, onDone }: UnfinishedMergeProps) {
  const t = useT()
  const files = member.status?.conflicts ?? []
  const { handOver, pending } = useHandOver(roomId, member.member_id, member.name)
  const abort = useAbortMerge()
  const [confirming, setConfirming] = useState(false)

  return (
    <>
      <Button size="xs" variant="ghost" disabled={abort.isPending} onClick={() => setConfirming(true)}>
        {t('branches.abortMerge')}
      </Button>
      <Button
        size="xs"
        variant="outline"
        disabled={pending}
        onClick={() =>
          handOver(files.length > 0 ? t('branches.handOverUnfinished', { branch, files: files.join('、') }) : t('branches.handOverCommit', { branch }), onDone)
        }
      >
        {t('branches.handOverAgain', { name: member.name })}
      </Button>
      {confirming ? (
        <AlertDialog open onOpenChange={(next) => (next ? undefined : setConfirming(false))}>
          <AlertDialogContent size="sm" aria-describedby={undefined}>
            <AlertDialogHeader>
              <AlertDialogTitle className="text-base">{t('branches.abortTitle', { name: member.name })}</AlertDialogTitle>
            </AlertDialogHeader>
            <p className="text-center text-[0.8125rem] leading-relaxed text-muted-foreground">{t('branches.abortHint', { name: member.name })}</p>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                disabled={abort.isPending}
                onClick={(event) => {
                  // The action would close the dialog at once; wait for the hub.
                  event.preventDefault()
                  abort.mutate(member.member_id, {
                    onSuccess: () => {
                      setConfirming(false)
                      toast.success(t('branches.aborted'))
                      onDone?.()
                    },
                    onError: (err) => toast.error(errorText(err)),
                  })
                }}
              >
                {t('branches.abortMerge')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </>
  )
}
