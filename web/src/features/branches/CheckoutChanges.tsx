import { useId, useState, type FormEvent } from 'react'
import { TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'
import { useCommitCheckout } from '@/api/branches'
import { errorText } from '@/api/errorText'
import type { WorktreeChange } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { useT } from '@/lib/i18n'

// filesShown caps the files the card names; the rest are counted.
const filesShown = 5

export interface CheckoutChangesProps {
  changed: WorktreeChange[]
  onDiff: () => void
  onCommit: () => void
}

// CheckoutChanges is what was changed in the project's checkout and not
// committed (docs/design.md 5.21): the members' worktrees lack it, and a
// merge that changes the same files is refused, so a person sees the files
// and reads or commits them from here.
export function CheckoutChanges({ changed, onDiff, onCommit }: CheckoutChangesProps) {
  const t = useT()
  const shown = changed.slice(0, filesShown)
  return (
    <section
      aria-label={t('branches.uncommittedTitle', { n: changed.length })}
      className="flex gap-3 rounded-xl border border-status-wait/35 bg-status-wait/6 px-4 py-3.5"
    >
      <TriangleAlertIcon aria-hidden="true" className="mt-0.5 size-4 flex-none text-status-wait" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex flex-col gap-0.5">
          <h2 className="text-[0.84375rem] font-semibold">{t('branches.uncommittedTitle', { n: changed.length })}</h2>
          <p className="text-[0.8125rem] text-muted-foreground">{t('branches.uncommittedHint')}</p>
        </div>
        <ul className="flex flex-col gap-0.5 font-mono text-[0.78125rem]" translate="no">
          {shown.map((file) => (
            <li key={file.path} className="flex min-w-0 gap-2">
              <span className="w-3 flex-none text-status-wait">{file.status}</span>
              <span className="min-w-0 break-all text-foreground">{file.path}</span>
            </li>
          ))}
          {changed.length > shown.length ? (
            <li className="pl-5 font-sans text-xs text-subtle">{t('branches.moreFiles', { n: changed.length - shown.length })}</li>
          ) : null}
        </ul>
      </div>
      <div className="flex flex-none items-start gap-2">
        <Button size="sm" variant="outline" onClick={onDiff}>
          {t('branches.diff')}
        </Button>
        <Button size="sm" onClick={onCommit}>
          {t('branches.commit')}
        </Button>
      </div>
    </section>
  )
}

export interface CommitDialogProps {
  projectId: string
  changed: WorktreeChange[]
  onClose: () => void
}

// CommitDialog commits, on the checkout's branch, the changes to the files a
// person keeps ticked, with the message they write.
export function CommitDialog({ projectId, changed, onClose }: CommitDialogProps) {
  const t = useT()
  const id = useId()
  const commit = useCommitCheckout(projectId)
  const [picked, setPicked] = useState(() => new Set(changed.map((file) => file.path)))
  const [invalid, setInvalid] = useState<string>()
  const [refused, setRefused] = useState<string>()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const message = String(new FormData(event.currentTarget).get('message') ?? '').trim()
    if (message === '') {
      setInvalid(t('error.commitMessage'))
      return
    }
    setInvalid(undefined)
    setRefused(undefined)
    // A rename is committed with the path it came from.
    const paths = changed.filter((file) => picked.has(file.path)).flatMap((file) => (file.from ? [file.path, file.from] : [file.path]))
    commit.mutate(
      { message, paths },
      {
        onSuccess: (hash) => {
          toast.success(t('branches.committed', { commit: hash.slice(0, 7) }))
          onClose()
        },
        onError: (err) => setRefused(errorText(err)),
      },
    )
  }

  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t('branches.commitTitle')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={onSubmit}>
          <FieldGroup className="my-4 gap-4">
            <ul className="flex max-h-44 flex-col gap-1 overflow-y-auto">
              {changed.map((file) => {
                const box = `${id}-${file.path}`
                return (
                  <li key={file.path} className="flex items-center gap-2.5">
                    <Checkbox
                      id={box}
                      checked={picked.has(file.path)}
                      onCheckedChange={(on) =>
                        setPicked((was) => {
                          const next = new Set(was)
                          if (on === true) next.add(file.path)
                          else next.delete(file.path)
                          return next
                        })
                      }
                    />
                    <label htmlFor={box} className="flex min-w-0 gap-2 font-mono text-[0.78125rem]" translate="no">
                      <span className="w-3 flex-none text-subtle">{file.status}</span>
                      <span className="min-w-0 break-all">{file.path}</span>
                    </label>
                  </li>
                )
              })}
            </ul>
            <Field data-invalid={invalid ? true : undefined}>
              <FieldLabel htmlFor={`${id}-message`}>{t('branches.message')}</FieldLabel>
              <Textarea id={`${id}-message`} name="message" rows={3} autoFocus />
              {invalid ? <FieldError>{invalid}</FieldError> : null}
            </Field>
            {refused ? (
              <p role="alert" className="text-[0.8125rem] text-status-fail">
                {refused}
              </p>
            ) : null}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={commit.isPending || picked.size === 0}>
              {commit.isPending ? t('branches.committing') : t('branches.commitNow')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
