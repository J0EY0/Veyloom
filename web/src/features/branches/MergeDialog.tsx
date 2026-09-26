import { useId, useRef, useState, type FormEvent } from 'react'
import { CircleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useMergeMember } from '@/api/branches'
import { ApiError } from '@/api/client'
import { errorText } from '@/api/errorText'
import type { MemberBranch } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT } from '@/lib/i18n'
import { Conflicts } from './Conflicts'
import { leftOutAtFirst, MergeFiles } from './MergeFiles'

export interface MergeDialogProps {
  roomId: string
  member: MemberBranch
  // The main line's branch.
  branch: string
  onClose: () => void
  // What a person does when changes not committed in the checkout are in
  // the way: read them, or commit them.
  onCheckoutDiff?: () => void
  onCommitCheckout?: () => void
}

// MergeDialog puts a member's work on the main line as one commit
// (docs/design.md 5.21): the files it changed, committed or not, less the
// new files a person leaves out, and the commit's message, a draft from
// what its own commits say or else what the member was last asked. Work
// that conflicts with the main line changes nothing; the conflicts are
// listed and handed to the member. A merge refused says why in words and
// what to do, apart from the message, which was not what was wrong.
export function MergeDialog({ roomId, member, branch, onClose, onCheckoutDiff, onCommitCheckout }: MergeDialogProps) {
  const t = useT()
  const id = useId()
  const merge = useMergeMember()
  const [error, setError] = useState<string>()
  const [refused, setRefused] = useState<{ text: string; inTheWay: boolean }>()
  const [conflicts, setConflicts] = useState<string[]>()
  const files = member.status?.files ?? []
  const [leave, setLeave] = useState(() => leftOutAtFirst(files))
  const nothingLeft = files.length > 0 && files.every((file) => leave.has(file.path))
  const fromCommits = (member.status?.commits?.length ?? 0) > 0
  // A dialog the refusal hands over to, opened once this one has closed:
  // one dialog at a time, the merge asked for again after, its draft as it
  // was. Focus stays put as this one goes, or the next would take it for a
  // press outside and close.
  const next = useRef<() => void>(undefined)
  function checkoutDiff() {
    next.current = onCheckoutDiff
    onClose()
  }
  function commitCheckout() {
    next.current = onCommitCheckout
    onClose()
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const message = String(new FormData(event.currentTarget).get('message') ?? '').trim()
    if (message === '') {
      setError(t('error.mergeMessage'))
      return
    }
    setError(undefined)
    setRefused(undefined)
    merge.mutate(
      { memberId: member.member_id, message, leave: [...leave] },
      {
        onSuccess: (result) => {
          if (result.conflicts && result.conflicts.length > 0) {
            setConflicts(result.conflicts)
            return
          }
          const merged = t('branches.merged', { branch, commit: (result.commit ?? '').slice(0, 7) })
          if (result.unsettled) toast.warning(merged, { description: t('branches.mergedUnsettled', { name: member.name, reason: result.unsettled }) })
          else toast.success(merged)
          onClose()
        },
        onError: (err) => setRefused({ text: errorText(err), inTheWay: err instanceof ApiError && err.code === 'checkoutChanged' }),
      },
    )
  }

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent
        aria-describedby={undefined}
        onCloseAutoFocus={(event) => {
          const open = next.current
          if (!open) return
          event.preventDefault()
          next.current = undefined
          open()
        }}
      >
        <DialogHeader>
          <DialogTitle>{t('branches.mergeTitle', { name: member.name, branch })}</DialogTitle>
        </DialogHeader>
        {conflicts ? (
          <div className="my-4">
            <Conflicts roomId={roomId} memberId={member.member_id} name={member.name} branch={branch} files={conflicts} onDone={onClose} />
          </div>
        ) : (
          <form onSubmit={onSubmit}>
            <FieldGroup className="my-4 gap-4">
              <MergeFiles
                name={member.name}
                files={files}
                leave={leave}
                onLeave={(path, left) =>
                  setLeave((was) => {
                    const next = new Set(was)
                    if (left) next.add(path)
                    else next.delete(path)
                    return next
                  })
                }
              />
              <Field data-invalid={error ? true : undefined}>
                <FieldLabel htmlFor={`${id}-message`}>{t('branches.message')}</FieldLabel>
                <Textarea
                  id={`${id}-message`}
                  name="message"
                  rows={fromCommits ? 6 : 3}
                  defaultValue={member.draft || member.name}
                  autoFocus
                  className="font-mono text-[0.8125rem]"
                />
                {error ? (
                  <FieldError className="wrap-anywhere">{error}</FieldError>
                ) : fromCommits ? (
                  <FieldDescription>{t('branches.draftFromCommits', { name: member.name })}</FieldDescription>
                ) : null}
              </Field>
              {refused ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>{t('branches.mergeRefused')}</AlertTitle>
                  <AlertDescription className="wrap-anywhere">
                    <p>{refused.text}</p>
                    {refused.inTheWay && (onCheckoutDiff || onCommitCheckout) ? (
                      <div className="mt-2 flex flex-wrap gap-2">
                        {onCheckoutDiff ? (
                          <Button type="button" size="xs" variant="outline" onClick={checkoutDiff}>
                            {t('branches.diff')}
                          </Button>
                        ) : null}
                        {onCommitCheckout ? (
                          <Button type="button" size="xs" variant="outline" onClick={commitCheckout}>
                            {t('branches.commitCheckout')}
                          </Button>
                        ) : null}
                      </div>
                    ) : null}
                  </AlertDescription>
                </Alert>
              ) : null}
            </FieldGroup>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={onClose}>
                {t('common.cancel')}
              </Button>
              {nothingLeft ? (
                // Every file left out: nothing would go on the main line.
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span tabIndex={0}>
                      <Button type="submit" disabled>
                        {t('branches.merge')}
                      </Button>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">{t('branches.allLeftOut', { name: member.name })}</TooltipContent>
                </Tooltip>
              ) : (
                <Button type="submit" disabled={merge.isPending}>
                  {merge.isPending ? t('branches.mergingNow') : t('branches.merge')}
                </Button>
              )}
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
