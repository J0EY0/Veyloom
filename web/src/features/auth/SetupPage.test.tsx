import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { getCurrentUser, setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { SetupPage } from './SetupPage'

describe('SetupPage', () => {
  beforeEach(() => setCurrentUser(null))

  it('checks the password twice and creates the account', async () => {
    let posted: unknown
    const calls = stubApi({
      '/auth/setup': async (req) => {
        posted = await req.json()
        return Response.json({ user: user('u1', '敬浩先') }, { status: 201 })
      },
    })
    renderWithProviders(<SetupPage />)
    // Nothing is filled in for you, not even the machine's account name.
    expect(screen.getByLabelText('用户名')).toHaveValue('')
    await userEvent.type(screen.getByLabelText('用户名'), '敬浩先')

    await userEvent.type(screen.getByLabelText('密码'), 'short')
    await userEvent.type(screen.getByLabelText('再输一遍密码'), 'short')
    await userEvent.click(screen.getByRole('button', { name: '创建并进入' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('密码至少 8 个字符。')

    await userEvent.clear(screen.getByLabelText('密码'))
    await userEvent.type(screen.getByLabelText('密码'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: '创建并进入' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('两次输入的不一样。')
    expect(calls.filter((c) => c.startsWith('POST'))).toEqual([])

    await userEvent.clear(screen.getByLabelText('再输一遍密码'))
    await userEvent.type(screen.getByLabelText('再输一遍密码'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: '创建并进入' }))
    await waitFor(() => expect(posted).toEqual({ name: '敬浩先', password: 'correct horse' }))
    await waitFor(() => expect(getCurrentUser()).toEqual({ id: 'u1', name: '敬浩先' }))
  })

  it('points at the login page when the account already exists', async () => {
    stubApi({
      '/auth/setup': () => Response.json({ error: 'the account already exists; sign in instead' }, { status: 409 }),
    })
    renderWithProviders(<SetupPage />)
    await userEvent.type(screen.getByLabelText('用户名'), 'jinghao')
    await userEvent.type(screen.getByLabelText('密码'), 'correct horse')
    await userEvent.type(screen.getByLabelText('再输一遍密码'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: '创建并进入' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('账号已经建过了，直接登录。')
  })
})
