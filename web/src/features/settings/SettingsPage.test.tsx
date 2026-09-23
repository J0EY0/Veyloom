import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { getCurrentUser, setCurrentUser } from '@/lib/currentUser'
import { getLocale, setLocale } from '@/lib/i18n'
import { getTheme, setTheme } from '@/lib/theme'
import { getUiSize, setUiSize } from '@/lib/uiSize'
import { stubApi } from '@/test/fetch'
import { user } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { SettingsPage } from './SettingsPage'

function open(section = '') {
  return renderWithProviders(
    <SettingsPage />,
    section ? { route: `/settings/${section}`, path: '/settings/:section' } : { route: '/settings', path: '/settings' },
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    setCurrentUser({ id: 'u1', name: 'jinghao' })
    setTheme('system')
    setUiSize('default')
    setLocale('zh-CN')
  })

  it('lists the kinds of setting in a column of their own, the first shown beside it', async () => {
    stubApi({})
    open()
    const nav = screen.getByRole('navigation', { name: '设置分类' })
    const links = within(nav).getAllByRole('link')
    expect(links.map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
      ['通用', '/settings/general'],
      ['快捷键', '/settings/shortcuts'],
      ['账号', '/settings/account'],
    ])
    expect(links[0]).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('heading', { name: '通用' })).toBeInTheDocument()
  })

  it('opens the global memory in General at the address its page had', async () => {
    stubApi({
      '/settings/memory': { memory: { enabled: true, personal: true, project: true } },
      '/memory': { memory: { entries: [{ text: '回复用中文。', date: '2026-09-20', source: 'jinghao' }], chars: 30, budget: 2000, hash: 'h1' } },
      '/memory/history': { commits: [] },
    })
    const { router } = open('memory')
    await waitFor(() => expect(router.state.location.pathname).toBe('/settings/general'))
    const dialog = await screen.findByRole('dialog', { name: '全局记忆' })
    expect(await within(dialog).findByText('回复用中文。')).toBeInTheDocument()
    expect(within(dialog).getByText('1 条')).toBeInTheDocument()
  })

  it('goes back to the column for a kind it does not know', async () => {
    stubApi({})
    const { router } = open('nope')
    await waitFor(() => expect(router.state.location.pathname).toBe('/settings'))
  })

  it('switches the colour scheme and the interface size as they are picked', async () => {
    stubApi({})
    open('general')
    await userEvent.click(screen.getByRole('tab', { name: '深色' }))
    expect(getTheme()).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByRole('tab', { name: '深色' })).toHaveAttribute('aria-selected', 'true')

    await userEvent.click(screen.getByRole('tab', { name: '大' }))
    expect(getUiSize()).toBe('large')
    expect(document.documentElement.style.fontSize).toBe('125%')
    // Picking the chosen one again keeps it.
    await userEvent.click(screen.getByRole('tab', { name: '大' }))
    expect(getUiSize()).toBe('large')

    await userEvent.click(screen.getByRole('tab', { name: '默认' }))
    expect(document.documentElement.style.fontSize).toBe('112.5%')
  })

  it('switches the language under general', async () => {
    stubApi({})
    open('general')
    await userEvent.click(screen.getByRole('tab', { name: 'English' }))
    expect(getLocale()).toBe('en')
    expect(await screen.findByRole('heading', { name: 'General' })).toBeInTheDocument()
    expect(screen.getByRole('tablist', { name: 'Interface language' })).toBeInTheDocument()
  })

  it('lists the shortcuts by where they work', async () => {
    stubApi({})
    open('shortcuts')
    const general = screen.getByRole('region', { name: '通用' })
    const search = within(general).getByText('搜索项目和页面').closest('[role=listitem]') as HTMLElement
    expect(search).toHaveTextContent('⌘K')
    expect(
      within(screen.getByRole('region', { name: '群聊' }))
        .getByText('换行')
        .closest('[role=listitem]'),
    ).toHaveTextContent('⇧↵')
    expect(within(screen.getByRole('region', { name: 'Agents' })).getByText('搜索 Agent')).toBeInTheDocument()
  })

  it('renames the account', async () => {
    let patched: unknown
    stubApi({
      '/me': async (req) => {
        patched = await req.json()
        return { user: user('u1', 'Jinghao Xian') }
      },
    })
    open('account')
    expect(screen.getByText('jinghao')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '修改用户名' }))
    const input = await screen.findByLabelText('用户名')
    expect(input).toHaveValue('jinghao')
    await userEvent.clear(input)
    await userEvent.type(input, 'Jinghao Xian')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(getCurrentUser()).toEqual({ id: 'u1', name: 'Jinghao Xian' }))
    expect(patched).toEqual({ name: 'Jinghao Xian' })
    expect(await screen.findByText('Jinghao Xian')).toBeInTheDocument()
  })

  it('changes the password after checking the two entries match', async () => {
    let posted: unknown
    stubApi({
      '/me/password': async (req) => {
        posted = await req.json()
        return new Response(null, { status: 204 })
      },
    })
    open('account')
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }))
    await userEvent.type(await screen.findByLabelText('当前密码'), 'correct horse')
    await userEvent.type(screen.getByLabelText('新密码'), 'battery staple')
    await userEvent.type(screen.getByLabelText('再输一遍新密码'), 'battery stapl')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('两次输入的不一样。')

    await userEvent.type(screen.getByLabelText('再输一遍新密码'), 'e')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(posted).toEqual({ current: 'correct horse', new: 'battery staple' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('says when the current password is wrong', async () => {
    stubApi({ '/me/password': () => Response.json({ error: 'the current password is wrong' }, { status: 403 }) })
    open('account')
    await userEvent.click(screen.getByRole('button', { name: '修改密码' }))
    await userEvent.type(await screen.findByLabelText('当前密码'), 'nope')
    await userEvent.type(screen.getByLabelText('新密码'), 'battery staple')
    await userEvent.type(screen.getByLabelText('再输一遍新密码'), 'battery staple')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('当前密码不对。')
  })

  it('signs out from the account section', async () => {
    const calls = stubApi({ '/auth/logout': () => new Response(null, { status: 204 }) })
    open('account')
    await userEvent.click(screen.getByRole('button', { name: '退出登录' }))
    await waitFor(() => expect(calls).toContain('POST /auth/logout'))
    await waitFor(() => expect(getCurrentUser()).toBeNull())
  })
})
