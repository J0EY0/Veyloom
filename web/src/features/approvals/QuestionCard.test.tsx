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

function asked(overrides: Partial<Approval> = {}): Approval {
  return approval('q1', {
    kind: 'question',
    tool: 'AskUserQuestion',
    input: {
      questions: [
        {
          id: '1',
          header: 'Database',
          question: 'Which database?',
          options: [{ label: 'Postgres', description: 'the one we use' }, { label: 'SQLite' }],
          other: true,
        },
        { id: '2', question: 'Which checks?', options: [{ label: 'lint' }, { label: 'test' }], multiSelect: true },
        { id: '3', question: 'Your token?', secret: true },
      ],
    },
    ...overrides,
  })
}

describe('QuestionCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('sends the answers once every question has one', async () => {
    let posted: unknown
    stubApi({
      '/approvals/q1/decide': async (req) => {
        posted = await req.json()
        return { approval: asked({ status: 'allowed', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={asked()} memberName="Claude Architect" names={names} />)
    expect(screen.getByText('Database')).toBeInTheDocument()
    expect(screen.getByText('the one we use')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: '提交' })
    expect(send).toBeDisabled()

    // One's own answer instead of an option.
    await userEvent.click(screen.getByRole('radio', { name: '其他' }))
    await userEvent.type(screen.getByLabelText('回答：Which database?'), 'DuckDB')
    await userEvent.click(screen.getByRole('checkbox', { name: 'lint' }))
    await userEvent.click(screen.getByRole('checkbox', { name: 'test' }))
    const token = screen.getByLabelText('回答：Your token?')
    expect(token).toHaveAttribute('type', 'password')
    await userEvent.type(token, 'hunter2')
    await userEvent.click(send)
    await waitFor(() =>
      expect(posted).toEqual({ user_id: 'u1', allow: true, message: '', answer: { answers: { '1': ['DuckDB'], '2': ['lint', 'test'], '3': ['hunter2'] } } }),
    )
  })

  it('declines without answering', async () => {
    let posted: unknown
    stubApi({
      '/approvals/q1/decide': async (req) => {
        posted = await req.json()
        return { approval: asked({ status: 'denied', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={asked()} names={names} />)
    await userEvent.click(screen.getByRole('button', { name: '不回答' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: false, message: '' }))
  })

  it('shows what was answered, secrets as the mark kept', () => {
    renderWithProviders(
      <RequestCard
        approval={asked({
          status: 'allowed',
          decided_by: 'u1',
          decided_at: '2026-09-19T03:00:00Z',
          answer: { answers: { '1': ['Postgres'], '2': ['lint', 'test'], '3': ['••••••'] } },
        })}
        names={names}
      />,
    )
    expect(screen.getByText('已回答')).toBeInTheDocument()
    expect(screen.getByText('Postgres')).toBeInTheDocument()
    expect(screen.getByText('lint、test')).toBeInTheDocument()
    expect(screen.getByText('••••••')).toBeInTheDocument()
    expect(screen.getByText(/alice 回答/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '提交' })).not.toBeInTheDocument()
  })

  it('says when nobody answered', () => {
    renderWithProviders(<RequestCard approval={asked({ status: 'expired', message: 'nobody decided within 30m0s' })} names={names} />)
    expect(screen.getByText('提问已过期')).toBeInTheDocument()
    expect(screen.getByText(/无人回答/)).toHaveTextContent('“30m0s 内没人处理”')
  })

  it('takes several lines starting from the text given, and hints at a line', async () => {
    let posted: unknown
    stubApi({
      '/approvals/q2/decide': async (req) => {
        posted = await req.json()
        return { approval: approval('q2', { kind: 'question', status: 'allowed', decided_by: 'u1' }) }
      },
    })
    const input = {
      questions: [
        { id: '1', question: 'Edit the changelog', multiline: true, default: '- fixed things\n' },
        { id: '2', question: 'Release name?', placeholder: 'v1.2.3' },
      ],
    }
    renderWithProviders(<RequestCard approval={approval('q2', { kind: 'question', tool: 'editor', input })} names={names} />)
    const box = screen.getByRole('textbox', { name: /Edit the changelog/ })
    expect(box.tagName).toBe('TEXTAREA')
    expect(box).toHaveValue('- fixed things\n')
    expect(screen.getByRole('textbox', { name: /Release name/ })).toHaveAttribute('placeholder', 'v1.2.3')

    await userEvent.type(box, '- and more')
    await userEvent.type(screen.getByRole('textbox', { name: /Release name/ }), 'v2')
    await userEvent.click(screen.getByRole('button', { name: '提交' }))
    await waitFor(() =>
      expect(posted).toEqual({ user_id: 'u1', allow: true, message: '', answer: { answers: { '1': ['- fixed things\n- and more'], '2': ['v2'] } } }),
    )
  })
})
