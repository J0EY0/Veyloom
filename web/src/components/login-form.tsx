import { useId, type ComponentProps, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export interface LoginFormValues {
  name: string
  password: string
  confirm: string
}

export interface LoginFormLabels {
  name: string
  password: string
  // Set on the setup page, which asks for the password twice.
  confirm?: string
  submit: string
  busy: string
}

export interface LoginFormProps extends Omit<ComponentProps<'div'>, 'onSubmit' | 'title'> {
  title: string
  labels: LoginFormLabels
  pending?: boolean
  error?: string
  onSubmit: (values: LoginFormValues) => void
}

// shadcn's login-01 block ("a simple login form"), with a name instead of
// an email, a second password field when asked for, and without the
// social and sign-up links: there is one account, and no subtitle.
export function LoginForm({ className, title, labels, pending = false, error, onSubmit, ...props }: LoginFormProps) {
  const id = useId()
  const passwordKind = labels.confirm ? 'new-password' : 'current-password'
  const errorAfter = labels.confirm ? 'confirm' : 'password'

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    onSubmit({
      name: String(data.get('name') ?? '').trim(),
      password: String(data.get('password') ?? ''),
      confirm: String(data.get('confirm') ?? ''),
    })
  }

  return (
    <div className={cn('flex flex-col gap-6', className)} {...props}>
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor={`${id}-name`}>{labels.name}</FieldLabel>
                <Input id={`${id}-name`} name="name" autoComplete="username" autoFocus spellCheck={false} />
              </Field>
              <Field data-invalid={error && errorAfter === 'password' ? true : undefined}>
                <FieldLabel htmlFor={`${id}-password`}>{labels.password}</FieldLabel>
                <Input
                  id={`${id}-password`}
                  name="password"
                  type="password"
                  autoComplete={passwordKind}
                  aria-invalid={error && errorAfter === 'password' ? true : undefined}
                />
                {error && errorAfter === 'password' ? <FieldError>{error}</FieldError> : null}
              </Field>
              {labels.confirm ? (
                <Field data-invalid={error ? true : undefined}>
                  <FieldLabel htmlFor={`${id}-confirm`}>{labels.confirm}</FieldLabel>
                  <Input id={`${id}-confirm`} name="confirm" type="password" autoComplete="new-password" aria-invalid={error ? true : undefined} />
                  {error ? <FieldError>{error}</FieldError> : null}
                </Field>
              ) : null}
              <Field>
                <Button type="submit" disabled={pending} aria-busy={pending}>
                  {pending ? labels.busy : labels.submit}
                </Button>
              </Field>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
