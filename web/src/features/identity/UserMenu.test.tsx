import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { getCurrentUser, setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { UserMenu } from './UserMenu'

describe('UserMenu', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'jinghao' }))

  it('leads to the settings page', async () => {
    stubApi({})
    const { router } = renderWithProviders(<UserMenu />)
    await userEvent.click(screen.getByRole('button', { name: '账号 jinghao' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '设置' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/settings'))
  })

  it('signs out', async () => {
    const calls = stubApi({ '/auth/logout': () => new Response(null, { status: 204 }) })
    renderWithProviders(<UserMenu />)
    await userEvent.click(screen.getByRole('button', { name: '账号 jinghao' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: '退出登录' }))
    await waitFor(() => expect(getCurrentUser()).toBeNull())
    expect(calls).toContain('POST /auth/logout')
  })
})
