import { useId, useState, type FormEvent } from 'react'
import { useRenameMe } from '@/api/auth'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useT } from '@/lib/i18n'

export interface RenameUserDialogProps {
  name: string
  onClose: () => void
}

// Renames the local user: the name agents and other people see.
export function RenameUserDialog({ name, onClose }: RenameUserDialogProps) {
  const rename = useRenameMe()
  const [error, setError] = useState<string>()
  const id = useId()
  const t = useT()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const next = String(new FormData(event.currentTarget).get('name') ?? '').trim()
    if (next === '') {
      setError(t('user.nameRequired'))
      return
    }
    setError(undefined)
    rename.mutate(next, { onSuccess: onClose, onError: (err) => setError(err.message) })
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('user.renameTitle')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4">
            <Field data-invalid={error ? true : undefined}>
              <FieldLabel htmlFor={id}>{t('user.nameLabel')}</FieldLabel>
              <Input id={id} name="name" defaultValue={name} autoFocus autoComplete="off" spellCheck={false} aria-invalid={error ? true : undefined} />
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={rename.isPending} aria-busy={rename.isPending}>
              {rename.isPending ? t('common.saving') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
