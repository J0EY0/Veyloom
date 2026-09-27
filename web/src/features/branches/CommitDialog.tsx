import { useId, useState, type FormEvent } from 'react'
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
