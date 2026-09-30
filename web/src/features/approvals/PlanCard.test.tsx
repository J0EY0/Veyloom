import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Approval } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { decisionNote } from './describe'
import { RequestCard } from './RequestCard'

const names = new Map([['u1', 'alice']])

function plan(overrides: Partial<Approval> = {}): Approval {
  return approval('p1', { tool: 'ExitPlanMode', input: { plan: '# Tidy up\n\n1. Remove dead code\n2. Run the tests' }, ...overrides })
}

describe('PlanCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('shows the plan as markdown and approves it', async () => {
    let posted: unknown
    stubApi({
      '/approvals/p1/decide': async (req) => {
        posted = await req.json()
        return { approval: plan({ status: 'allowed', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={plan()} memberName="Claude" names={names} />)
    expect(screen.getByRole('group', { name: '计划' })).toBeInTheDocument()
    expect(screen.getByText('请你审批计划')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Tidy up' })).toBeInTheDocument()
    expect(screen.getByText('Remove dead code')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '批准' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: true, message: '' }))
  })

  it('sends a plan back with a note', async () => {
    let posted: unknown
    stubApi({
      '/approvals/p1/decide': async (req) => {
        posted = await req.json()
        return { approval: plan({ status: 'denied', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={plan()} names={names} />)
    await userEvent.type(screen.getByLabelText('备注'), '先拆小一点')
    await userEvent.click(screen.getByRole('button', { name: '退回' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: false, message: '先拆小一点' }))
  })

  it('says who decided once settled, and why when the hub settled it', () => {
    const approved = renderWithProviders(
      <RequestCard approval={plan({ status: 'allowed', decided_by: 'u1', decided_at: '2026-09-19T02:12:00Z' })} names={names} />,
    )
    expect(screen.getByText('计划已批准')).toBeInTheDocument()
    expect(screen.getByText(/alice 批准/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '批准' })).not.toBeInTheDocument()
    approved.unmount()

    renderWithProviders(<RequestCard approval={plan({ status: 'cancelled', message: 'the runtime took the request back' })} names={names} />)
    expect(screen.getByText('计划已撤回')).toBeInTheDocument()
    expect(screen.getByText(/无人处理/)).toHaveTextContent('“运行时撤回了这个请求”')
  })
})

describe('decisionNote', () => {
  it("puts the hub's own reasons in the reader's language and leaves people's notes alone", () => {
    expect(decisionNote('the turn ended before a decision')).toBe('这一轮已结束，没有等到处理')
    expect(decisionNote('the runtime took the request back')).toBe('运行时撤回了这个请求')
    expect(decisionNote('nobody decided within 10m0s')).toBe('10m0s 内无人处理')
    expect(decisionNote('不要 force push')).toBe('不要 force push')
  })
})

describe('ConfirmCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  const confirm = (overrides: Partial<Approval> = {}) =>
    approval('c1', { tool: 'confirm', input: { title: 'Dangerous command', message: 'Allow rm -rf build?' }, ...overrides })

  it('asks yes or no, with nothing to add', async () => {
    let posted: unknown
    stubApi({
      '/approvals/c1/decide': async (req) => {
        posted = await req.json()
        return { approval: confirm({ status: 'denied', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={confirm()} memberName="Pi" names={names} />)
    expect(screen.getByRole('group', { name: '确认' })).toBeInTheDocument()
    expect(screen.getByText('请你确认')).toBeInTheDocument()
    expect(screen.getByText('Dangerous command')).toBeInTheDocument()
    expect(screen.getByText('Allow rm -rf build?')).toBeInTheDocument()
    expect(screen.queryByLabelText('备注')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '否' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: false, message: '' }))
  })

  it('says who answered once settled', () => {
    renderWithProviders(<RequestCard approval={confirm({ status: 'allowed', decided_by: 'u1' })} names={names} />)
    expect(screen.getByText('已确认')).toBeInTheDocument()
    expect(screen.getByText(/alice 确认了/)).toBeInTheDocument()
  })
})
