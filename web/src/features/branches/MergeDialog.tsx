import { useId, useRef, useState, type FormEvent } from 'react'
import { CircleAlertIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError } from '@/api/client'
import { errorText } from '@/api/errorText'
import type { MemberBranch, Task } from '@/api/types'
import { StatusMark } from '@/components/shared/status-mark'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useT } from '@/lib/i18n'
import { Conflicts } from './Conflicts'
import { DiffDialog } from './DiffDialog'
import { useLandWork } from './landWork'
import { leftOutAtFirst, MergeFiles } from './MergeFiles'
import type { MemberOverlap } from './overlaps'

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
  // The pieces of work waiting on the branch: a worktree is a member's, so
  // one branch can hold several.
  works?: Task[]
  // Files other members changed too.
  overlaps?: MemberOverlap[]
  // The draft a member made of the merge (docs/design.md 5.23.5): its
  // message comes first, and merging runs it.
  draft?: { id: string; message: string; by: string }
}

// MergeDialog puts a member's work on the main line as one commit
// (docs/design.md 5.21): the files it changed, committed or not, less the
// new files a person leaves out, and the commit's message, a draft from
// what its own commits say or else what the member was last asked. Work
// that conflicts with the main line changes nothing; the conflicts are
// listed and handed to the member. A merge refused says why in words and
// what to do, apart from the message, which was not what was wrong.
export function MergeDialog({ roomId, member, branch, onClose, onCheckoutDiff, onCommitCheckout, works = [], overlaps = [], draft }: MergeDialogProps) {
  const t = useT()
  const id = useId()
  const merge = useLandWork(draft?.id)
  // The patch, read over the dialog without losing the message.
  const [diffing, setDiffing] = useState(false)
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
    merge.land(
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
          {works.length > 0 ? <DialogDescription>{t('branches.works', { branch: member.branch ?? '', n: works.length })}</DialogDescription> : null}
        </DialogHeader>
        {works.length > 0 ? (
          <ul className="flex flex-col gap-1.5 rounded-lg bg-muted px-3 py-2.5">
            {works.map((work) => (
              <li key={`${work.chain}/${work.thread_id}`} className="flex min-w-0 items-center gap-2 text-[0.8125rem]">
                <StatusMark kind="merge" className="size-3" />
                <span className="flex-none font-mono text-xs text-subtle">#{work.thread_number}</span>
                <span className="min-w-0 truncate">{work.title || t('tasks.untitled', { n: work.thread_number })}</span>
              </li>
            ))}
          </ul>
        ) : null}
        {overlaps.map((o) => (
          <p key={o.names.join()} className="flex items-start gap-2 text-[0.8125rem] text-status-wait">
            <TriangleAlertIcon aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
            <span>{t('branches.overlapAlso', { names: o.names.join(t('common.listSeparator')), files: o.files.join(t('common.listSeparator')) })}</span>
          </p>
        ))}
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
                  rows={fromCommits || draft?.message.includes('\n') ? 6 : 3}
                  defaultValue={draft?.message || member.draft || member.name}
                  autoFocus
                  className="font-mono text-[0.8125rem]"
                />
                {error ? (
                  <FieldError className="wrap-anywhere">{error}</FieldError>
                ) : draft ? (
                  <FieldDescription>{t('branches.draftFromDraft', { name: draft.by })}</FieldDescription>
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
            <DialogFooter className="sm:justify-between">
              <Button type="button" variant="ghost" onClick={() => setDiffing(true)}>
                {t('branches.diff')}
              </Button>
              <span className="flex flex-col-reverse gap-2 sm:flex-row">
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
              </span>
            </DialogFooter>
          </form>
        )}
        {diffing ? <DiffDialog memberId={member.member_id} name={member.name} onClose={() => setDiffing(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}
