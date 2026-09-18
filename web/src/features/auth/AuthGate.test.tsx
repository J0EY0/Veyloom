import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { beforeEach, describe, expect, it } from 'vitest'
import { authKeys } from '@/api/auth'
import type { AuthStatus } from '@/api/types'
import { signedOut } from '@/lib/authEvents'
import { getCurrentUser, setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { user } from '@/test/fixtures'
import { AuthGate } from './AuthGate'

// The gate with stand-ins for what it guards.
function renderGate(route: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [
      {
        path: '/',
        Component: AuthGate,
        children: [
          { path: 'setup', element: <p>setup page</p> },
          { path: 'login', element: <p>login page</p> },
          { index: true, element: <p>the app</p> },
          { path: 'rooms/:id', element: <p>the app</p> },
        ],
      },
    ],
    { initialEntries: [route] },
  )
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { router, client }
}

const me = user('u1', 'jinghao')

describe('AuthGate', () => {
  beforeEach(() => setCurrentUser(null))

  it('sends a fresh install to the setup page, wherever it was going', async () => {
    stubApi({ '/auth/status': { setup_required: true, user: null } })
    const { router } = renderGate('/rooms/r1')
    expect(await screen.findByText('setup page')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/setup')
  })

  it('sends a signed-out visitor to the login page and back in once signed in', async () => {
    stubApi({ '/auth/status': { setup_required: false, user: null } })
    const { router, client } = renderGate('/rooms/r1')
    expect(await screen.findByText('login page')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')

    // Signing in updates the status; the gate leaves the login page.
    client.setQueryData<AuthStatus>(authKeys.status, { setup_required: false, user: me })
    expect(await screen.findByText('the app')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/')
  })

  it('lets the signed-in person through and remembers who they are', async () => {
    stubApi({ '/auth/status': { setup_required: false, user: me } })
    const { router } = renderGate('/rooms/r1')
    expect(await screen.findByText('the app')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/rooms/r1')
    expect(getCurrentUser()).toEqual({ id: 'u1', name: 'jinghao' })
  })

  it('keeps a signed-in person off the sign-in pages', async () => {
    stubApi({ '/auth/status': { setup_required: false, user: me } })
    const { router } = renderGate('/login')
    expect(await screen.findByText('the app')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/')
  })

  it('shows the login page when some request comes back 401', async () => {
    stubApi({ '/auth/status': { setup_required: false, user: me } })
    const { router } = renderGate('/rooms/r1')
    await screen.findByText('the app')
    signedOut()
    expect(await screen.findByText('login page')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/login')
    expect(getCurrentUser()).toBeNull()
  })

  it('says when the server cannot be reached, and retries', async () => {
    let attempts = 0
    stubApi({
      '/auth/status': () => {
        attempts += 1
        return attempts === 1 ? Response.json({ error: 'db down' }, { status: 500 }) : { setup_required: false, user: me }
      },
    })
    renderGate('/')
    expect(await screen.findByText('连不上服务：db down')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('the app')).toBeInTheDocument()
  })
})
