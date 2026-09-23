import { useState } from 'react'
import { useSetup } from '@/api/auth'
import { LoginForm, type LoginFormValues } from '@/components/login-form'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useT } from '@/lib/i18n'
import { AuthPage } from './AuthPage'
import { errorText } from '@/api/errorText'

const minPassword = 8

// First run: nobody has an account yet, so this page creates the one
// there is and signs it in.
export function SetupPage() {
  const setup = useSetup()
  const [error, setError] = useState<string>()
  const t = useT()
  useDocumentTitle(t('auth.setupTitle'))

  function onSubmit({ name, password, confirm }: LoginFormValues) {
    if (name === '') {
      setError(t('user.nameRequired'))
      return
    }
    // Counted in characters, as the server counts them.
    if ([...password].length < minPassword) {
      setError(t('user.passwordTooShort'))
      return
    }
    if (password !== confirm) {
      setError(t('user.passwordMismatch'))
      return
    }
    setError(undefined)
    setup.mutate(
      { name, password },
      {
        onError: (err) => setError(errorText(err, { 409: t('auth.setupDone') })),
      },
    )
  }

  return (
    <AuthPage>
      <LoginForm
        title={t('auth.setupTitle')}
        labels={{ name: t('auth.name'), password: t('auth.password'), confirm: t('auth.confirm'), submit: t('auth.setupSubmit'), busy: t('auth.setupBusy') }}
        pending={setup.isPending}
        error={error}
        onSubmit={onSubmit}
      />
    </AuthPage>
  )
}
