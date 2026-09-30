import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { getCurrentUser, setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { LoginPage } from './LoginPage'

describe('LoginPage', () => {
  beforeEach(() => setCurrentUser(null))

  it('rejects a wrong password in one line and signs in with the right one', async () => {
    let posted: unknown
    stubApi({
      '/auth/login': async (req) => {
        const body = (await req.json()) as { name: string; password: string }
        posted = body
        return body.password === 'correct horse' ? { user: user('u1', 'jinghao') } : Response.json({ error: 'wrong name or password' }, { status: 401 })
      },
    })
    renderWithProviders(<LoginPage />)

    await userEvent.click(screen.getByRole('button', { name: '登录' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('用户名不能为空。')

    await userEvent.type(screen.getByLabelText('用户名'), 'jinghao')
    await userEvent.type(screen.getByLabelText('密码'), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: '登录' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('用户名或密码错误。')
    expect(getCurrentUser()).toBeNull()

    await userEvent.clear(screen.getByLabelText('密码'))
    await userEvent.type(screen.getByLabelText('密码'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: '登录' }))
    await waitFor(() => expect(getCurrentUser()).toEqual({ id: 'u1', name: 'jinghao' }))
    expect(posted).toEqual({ name: 'jinghao', password: 'correct horse' })
  })
})
