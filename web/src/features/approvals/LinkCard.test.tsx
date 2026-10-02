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

function link(url: string, overrides: Partial<Approval> = {}): Approval {
  return approval('l1', { kind: 'link', tool: 'elicitation', input: { server: 'deploy', message: 'Sign in to deploy', url }, ...overrides })
}

describe('LinkCard', () => {
  beforeEach(() => setCurrentUser({ id: 'u1', name: 'alice' }))

  it('shows where the link goes and opens it only on a click, in a new tab', async () => {
    let posted: unknown
    stubApi({
      '/approvals/l1/decide': async (req) => {
        posted = await req.json()
        return { approval: link('https://login.example.com/device', { status: 'allowed', decided_by: 'u1' }) }
      },
    })
    renderWithProviders(<RequestCard approval={link('https://login.example.com/device')} names={names} />)
    expect(screen.getByText('login.example.com')).toBeInTheDocument()
    const open = screen.getByRole('link', { name: '打开链接' })
    expect(open).toHaveAttribute('href', 'https://login.example.com/device')
    expect(open).toHaveAttribute('target', '_blank')
    expect(open).toHaveAttribute('rel', 'noopener noreferrer')
    await userEvent.click(screen.getByRole('button', { name: '已完成' }))
    await waitFor(() => expect(posted).toEqual({ user_id: 'u1', allow: true, message: '' }))
  })

  it('never offers to open anything but a web link', () => {
    renderWithProviders(<RequestCard approval={link('javascript:alert(1)')} names={names} />)
    expect(screen.queryByRole('link', { name: '打开链接' })).not.toBeInTheDocument()
    // And nothing said about it.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says who dealt with it', () => {
    renderWithProviders(<RequestCard approval={link('https://login.example.com', { status: 'denied', decided_by: 'u1' })} names={names} />)
    expect(screen.getByText('未打开')).toBeInTheDocument()
    expect(screen.getByText(/alice 拒绝了/)).toBeInTheDocument()
  })
})
