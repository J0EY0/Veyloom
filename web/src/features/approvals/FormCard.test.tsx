import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Approval } from '@/api/types'
import { setCurrentUser } from '@/lib/currentUser'
import { stubApi } from '@/test/fetch'
import { approval } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { RequestCard } from './RequestCard'

const names = new Map([['u1', 'alice']])

function form(overrides: Partial<Approval> = {}): Approval {
  return approval('f1', {
    kind: 'form',
    tool: 'elicitation',
    input: {
      server: 'deploy',
      message: 'Deploy where?',
      schema: {
        type: 'object',
        properties: {
          name: { type: 'string', title: 'Name', minLength: 2 },
          size: {
            type: 'string',
            title: 'Size',
            oneOf: [
              { const: 's', title: 'Small' },
              { const: 'l', title: 'Large' },
            ],
          },
          regions: { type: 'array', title: 'Regions', items: { type: 'string', enum: ['eu', 'us'] } },
          notify: { type: 'boolean', title: 'Notify me', default: true },
        },
        required: ['name', 'size'],
      },
    },
    ...overrides,
  })
}

describe('FormCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('sends the fields filled in, once the required ones are', async () => {
    let posted: unknown
    stubApi({
      '/approvals/f1/decide': async (req) => {
        posted = await req.json()
        return { approval: form({ status: 'allowed', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={form()} memberName="Codex Implementer" names={names} />)
    expect(screen.getByText('Deploy where?')).toBeInTheDocument()
    expect(screen.getByText('来自 deploy')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: '提交' })
    expect(send).toBeDisabled()
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute('aria-required', 'true')
    expect(screen.getByRole('radiogroup', { name: 'Size' })).toHaveAttribute('aria-required', 'true')

    await userEvent.type(screen.getByLabelText(/^Name/), 'a')
    expect(screen.getByText('至少 2 个字符')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText(/^Name/), 'lice')
    await userEvent.click(screen.getByRole('radio', { name: 'Large' }))
    await userEvent.click(screen.getByRole('checkbox', { name: 'eu' }))
    await userEvent.click(screen.getByRole('switch', { name: /Notify me/ }))
    await userEvent.click(send)
    await waitFor(() =>
      expect(posted).toEqual({ user_id: 'u1', allow: true, message: '', answer: { content: { name: 'alice', size: 'l', regions: ['eu'], notify: false } } }),
    )
  })

  it('declines without filling anything in', async () => {
    let posted: unknown
    stubApi({
      '/approvals/f1/decide': async (req) => {
        posted = await req.json()
        return { approval: form({ status: 'denied', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={form()} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '拒绝' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: false, message: '' }))
  })

  it('shows what was sent, choices by their titles', () => {
    renderWithProviders(
      <RequestCard
        approval={form({
          status: 'allowed',
          decided_by: 'u1',
          decided_at: '2026-09-19T03:00:00Z',
          answer: { content: { name: 'alice', size: 'l', regions: ['eu', 'us'], notify: true } },
        })}
        names={names}
      />,
    )
    expect(screen.getByText('已提交')).toBeInTheDocument()
    expect(screen.getByText('Large')).toBeInTheDocument()
    expect(screen.getByText('eu、us')).toBeInTheDocument()
    expect(screen.getByText('是')).toBeInTheDocument()
    expect(screen.getByText(/alice 提交/)).toBeInTheDocument()
  })
})
