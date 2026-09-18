import { useState } from 'react'
import { useLogin } from '@/api/auth'
import { ApiError } from '@/api/client'
import { LoginForm, type LoginFormValues } from '@/components/login-form'
import { useDocumentTitle } from '@/lib/useDocumentTitle'
import { useT } from '@/lib/i18n'
import { AuthPage } from './AuthPage'

// Every visit after the first: the account exists, say the password.
export function LoginPage() {
  const login = useLogin()
  const [error, setError] = useState<string>()
  const t = useT()
  useDocumentTitle(t('auth.loginTitle'))

  function onSubmit({ name, password }: LoginFormValues) {
    if (name === '') {
      setError(t('user.nameRequired'))
      return
    }
    setError(undefined)
    login.mutate(
      { name, password },
      {
        onError: (err) => setError(err instanceof ApiError && err.status === 401 ? t('auth.badCredentials') : err.message),
      },
    )
  }

  return (
    <AuthPage>
      <LoginForm
        title={t('auth.loginTitle')}
        labels={{ name: t('auth.name'), password: t('auth.password'), submit: t('auth.loginSubmit'), busy: t('auth.loginBusy') }}
        pending={login.isPending}
        error={error}
        onSubmit={onSubmit}
      />
    </AuthPage>
  )
}
