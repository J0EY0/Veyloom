import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Navigate, Outlet, useLocation } from 'react-router'
import { authKeys, useAuthStatus } from '@/api/auth'
import type { AuthStatus } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'
import { onSignedOut } from '@/lib/authEvents'
import { setCurrentUser } from '@/lib/currentUser'
import { useT } from '@/lib/i18n'
import { AuthPage } from './AuthPage'
import { errorText } from '@/api/errorText'

// Decides what the URL may show (docs/webui.md §4.8): the setup page
// until the account exists, the login page until someone is signed in,
// the app after that. A 401 from any request brings the login page back.
export function AuthGate() {
  const status = useAuthStatus()
  const location = useLocation()
  const client = useQueryClient()
  const t = useT()

  useEffect(
    () =>
      onSignedOut(() => {
        setCurrentUser(null)
        client.setQueryData<AuthStatus>(authKeys.status, (old) => (old ? { ...old, user: null } : old))
      }),
    [client],
  )

  if (status.isPending) {
    return (
      <p role="status" className="flex min-h-dvh items-center justify-center gap-2 bg-background text-sm text-subtle">
        <Spinner className="size-3.5" />
        {t('auth.checking')}
      </p>
    )
  }
  if (status.isError) {
    return (
      <AuthPage>
        <Card>
          <CardHeader>
            <CardTitle>{errorText(status.error)}</CardTitle>
          </CardHeader>
          <CardContent>
            <Button size="sm" onClick={() => void status.refetch()}>
              {t('auth.retry')}
            </Button>
          </CardContent>
        </Card>
      </AuthPage>
    )
  }

  const { setup_required: setupRequired, user } = status.data
  const at = location.pathname
  if (setupRequired) {
    return at === '/setup' ? <Outlet /> : <Navigate to="/setup" replace />
  }
  if (!user) {
    return at === '/login' ? <Outlet /> : <Navigate to="/login" replace />
  }
  if (at === '/setup' || at === '/login') {
    return <Navigate to="/" replace />
  }
  return <Outlet />
}
