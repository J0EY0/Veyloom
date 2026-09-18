import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ApprovalCard } from './ApprovalCard'

const names = new Map([['u1', 'alice']])

describe('ApprovalCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('shows the command and sends the decision with a note', async () => {
    let posted: unknown
    stubApi({
      '/approvals/ap1/decide': async (req) => {
        posted = await req.json()
        return { approval: approval('ap1', { status: 'allowed', decided_by: 'u1', decided_at: '2026-09-14T02:12:00Z', message: '本地库已起' }) }
      },
    })
    renderWithProviders(<ApprovalCard approval={approval('ap1')} memberName="Codex Implementer" names={names} />)
    expect(screen.getByText('make test')).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText('备注'), '本地库已起')
    await userEvent.click(screen.getByRole('button', { name: '允许' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: true, message: '本地库已起' }))
  })

  it('says who decided once settled, and dims what nobody decided', () => {
    const denied = renderWithProviders(
      <ApprovalCard
        approval={approval('ap1', { status: 'denied', decided_by: 'u1', decided_at: '2026-09-14T02:12:00Z', message: '不要 force push' })}
        names={names}
      />,
    )
    expect(screen.getByText('请求被拒绝')).toBeInTheDocument()
    expect(screen.getByText(/alice 拒绝/)).toHaveTextContent('“不要 force push”')
    expect(screen.queryByRole('button', { name: '允许' })).not.toBeInTheDocument()
    denied.unmount()

    renderWithProviders(<ApprovalCard approval={approval('ap2', { status: 'expired' })} names={names} />)
    expect(screen.getByText('请求已过期')).toBeInTheDocument()
    expect(screen.getByText('无人决定')).toBeInTheDocument()
  })

  it('explains a lost race', async () => {
    stubApi({ '/approvals/ap1/decide': Response.json({ error: 'approval ap1: already decided' }, { status: 409 }) })
    renderWithProviders(<ApprovalCard approval={approval('ap1')} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '拒绝' }))
    expect(await screen.findByText('已经有人决定了。')).toBeInTheDocument()
  })

  it('waits for a user to be chosen', () => {
    setCurrentUser(null)
    renderWithProviders(<ApprovalCard approval={approval('ap1')} names={names} />)
    expect(screen.getByRole('button', { name: '允许' })).toBeDisabled()
    expect(screen.getByText('正在确认登录状态')).toBeInTheDocument()
  })
})
