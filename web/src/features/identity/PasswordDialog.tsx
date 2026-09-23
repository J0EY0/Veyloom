import { useId, useState, type FormEvent } from 'react'
import { useChangePassword } from '@/api/auth'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useT } from '@/lib/i18n'
import { errorText } from '@/api/errorText'

const minPassword = 8

export interface PasswordDialogProps {
  onClose: () => void
}

// Changes the account's password; the current one is asked for first.
export function PasswordDialog({ onClose }: PasswordDialogProps) {
  const change = useChangePassword()
  const [error, setError] = useState<string>()
  const id = useId()
  const t = useT()

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const current = String(data.get('current') ?? '')
    const next = String(data.get('next') ?? '')
    const confirm = String(data.get('confirm') ?? '')
    // Counted in characters, as the server counts them.
    if ([...next].length < minPassword) {
      setError(t('user.passwordTooShort'))
      return
    }
    if (next !== confirm) {
      setError(t('user.passwordMismatch'))
      return
    }
    setError(undefined)
    change.mutate(
      { current, new: next },
      {
        onSuccess: onClose,
        onError: (err) => setError(errorText(err, { 403: t('user.currentPasswordWrong') })),
      },
    )
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{t('user.passwordTitle')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <Field>
              <FieldLabel htmlFor={`${id}-current`}>{t('user.currentPassword')}</FieldLabel>
              <Input id={`${id}-current`} name="current" type="password" autoComplete="current-password" autoFocus />
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-next`}>{t('user.newPassword')}</FieldLabel>
              <Input id={`${id}-next`} name="next" type="password" autoComplete="new-password" />
            </Field>
            <Field data-invalid={error ? true : undefined}>
              <FieldLabel htmlFor={`${id}-confirm`}>{t('user.confirmPassword')}</FieldLabel>
              <Input id={`${id}-confirm`} name="confirm" type="password" autoComplete="new-password" aria-invalid={error ? true : undefined} />
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={change.isPending} aria-busy={change.isPending}>
              {change.isPending ? t('common.saving') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
