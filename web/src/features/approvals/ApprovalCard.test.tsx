import { screen, waitFor, within } from '@testing-library/react'
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

  it('says what the runtime said the command is for', () => {
    // As Claude Code sends its Bash tool's input (docs/progress.md, step 153).
    const request = approval('ap2', { tool: 'Bash', input: { command: 'go test ./... -v', description: '运行现有的测试' } })
    renderWithProviders(<ApprovalCard approval={request} memberName="Tester" names={names} />)
    expect(screen.getByText('go test ./... -v')).toBeInTheDocument()
    expect(screen.getByText('运行现有的测试')).toBeInTheDocument()
  })

  it('lets the like of it through for the turn or from now on, as the pattern the runtime offered', async () => {
    const posted: unknown[] = []
    stubApi({
      '/approvals/ap1/decide': async (req) => {
        posted.push(await req.json())
        return { approval: approval('ap1', { status: 'allowed', decided_by: 'u1', scope: 'always' }) }
      },
    })
    const request = approval('ap1', { input: { command: 'go test ./...' }, similar: { rules: ['Bash(go test *)'] } })
    renderWithProviders(<ApprovalCard approval={request} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '允许的范围' }))
    const menu = await screen.findByRole('menu', { name: '允许的范围' })
    expect(within(menu).getByRole('menuitem', { name: '本轮允许 go test *' })).toBeInTheDocument()
    expect(within(menu).getByRole('menuitem', { name: '本轮自动批准' })).toBeInTheDocument()
    await userEvent.click(within(menu).getByRole('menuitem', { name: '始终允许 go test *' }))
    await waitFor(() => expect(posted).toEqual([{ user_id: 'u1', allow: true, scope: 'always', message: '' }]))
  })

  it('takes a Codex prefix for the command pattern, and trusts the rest of the turn', async () => {
    const posted: unknown[] = []
    stubApi({
      '/approvals/ap1/decide': async (req) => {
        posted.push(await req.json())
        return { approval: approval('ap1', { status: 'allowed', decided_by: 'u1', scope: 'turn' }) }
      },
    })
    const request = approval('ap1', {
      tool: 'commandExecution',
      input: { command: "/bin/zsh -lc 'go vet ./...'" },
      similar: { same: true, prefix: ['go', 'vet'] },
    })
    renderWithProviders(<ApprovalCard approval={request} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '允许的范围' }))
    const menu = await screen.findByRole('menu', { name: '允许的范围' })
    expect(within(menu).getByRole('menuitem', { name: '始终允许 go vet *' })).toBeInTheDocument()
    await userEvent.click(within(menu).getByRole('menuitem', { name: '本轮自动批准' }))
    await waitFor(() => expect(posted).toEqual([{ user_id: 'u1', allow: true, scope: 'turn', message: '' }]))
  })

  it('offers only the rest of the turn when the runtime offered nothing to keep', async () => {
    const request = approval('ap1', { tool: 'fileChange', input: { paths: ['a.go'] }, similar: { same: true } })
    renderWithProviders(<ApprovalCard approval={request} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '允许的范围' }))
    const menu = await screen.findByRole('menu', { name: '允许的范围' })
    expect(within(menu).getByRole('menuitem', { name: '本轮允许 改同样的文件' })).toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: /始终允许/ })).not.toBeInTheDocument()
  })

  it('is one line once settled, opening onto the whole command and the note', async () => {
    const denied = renderWithProviders(
      <ApprovalCard
        approval={approval('ap1', { status: 'denied', decided_by: 'u1', decided_at: '2026-09-14T02:12:00Z', message: '不要 force push' })}
        names={names}
      />,
    )
    const line = screen.getByRole('button', { name: /请求被拒绝/ })
    expect(line).toHaveTextContent('make test')
    expect(line).toHaveTextContent('alice 拒绝')
    expect(screen.queryByText('“不要 force push”')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '允许' })).not.toBeInTheDocument()
    await userEvent.click(line)
    expect(screen.getByText('“不要 force push”')).toBeInTheDocument()
    denied.unmount()

    const alike = renderWithProviders(
      <ApprovalCard
        approval={approval('ap4', { status: 'allowed', decided_by: 'u1', similar: { rules: ['Bash(go test:*)'] }, scope: 'similar' })}
        names={names}
      />,
    )
    const allowed = screen.getByRole('button', { name: /运行了/ })
    expect(allowed).toHaveTextContent('alice 允许')
    await userEvent.click(allowed)
    expect(screen.getByText('本轮内同类不再询问：go test *')).toBeInTheDocument()
    alike.unmount()

    const kept = renderWithProviders(
      <ApprovalCard
        approval={approval('ap5', { status: 'allowed', decided_by: 'u1', similar: { prefix: ['go', 'test'], same: true }, scope: 'always' })}
        names={names}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /运行了/ }))
    expect(screen.getByText('已加入始终允许：go test *')).toBeInTheDocument()
    kept.unmount()

    // Let through with the rest of the turn: the shield and no one's name.
    const trusted = renderWithProviders(
      <ApprovalCard approval={approval('ap6', { status: 'allowed', decided_by: 'u1', reviewer: 'turn', decided_at: '2026-09-14T02:12:00Z' })} names={names} />,
    )
    const shielded = screen.getByRole('button', { name: /运行了/ })
    expect(shielded).toHaveTextContent('本轮自动批准')
    expect(shielded).not.toHaveTextContent('alice')
    trusted.unmount()

    renderWithProviders(<ApprovalCard approval={approval('ap2', { status: 'expired' })} names={names} />)
    expect(screen.getByText('请求已过期')).toBeInTheDocument()
    expect(screen.getByText('无人决定')).toBeInTheDocument()
  })

  it("names the runtime's own reviewer and the risk it saw", () => {
    renderWithProviders(
      <ApprovalCard
        approval={approval('ap3', {
          status: 'allowed',
          reviewer: 'codex_auto_review',
          answer: { risk: 'low', authorization: 'high' },
          decided_at: '2026-09-19T01:11:00Z',
          message: '公开的 HEAD 请求',
        })}
        names={names}
      />,
    )
    const line = screen.getByRole('button', { name: /Codex 自动审核 允许/ })
    expect(line).toHaveTextContent('低风险')
    expect(screen.queryByRole('button', { name: '允许' })).not.toBeInTheDocument()
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
