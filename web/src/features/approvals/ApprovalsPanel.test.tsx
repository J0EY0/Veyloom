import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { approval } from '@/test/fixtures'
import { renderWithProviders } from '@/test/render'
import { ApprovalsPanel } from './ApprovalsPanel'

describe('ApprovalsPanel', () => {
  it('lists what is pending with a way into each topic', async () => {
    stubApi({
      '/rooms/r1/approvals': { approvals: [approval('ap1', { thread_id: 't9' })] },
      '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Codex Implementer', enabled: true }] },
      '/users': { users: [] },
    })
    const onOpenThread = vi.fn()
    renderWithProviders(<ApprovalsPanel roomId="r1" onClose={() => {}} onOpenThread={onOpenThread} />)

    expect(await screen.findByText('make test')).toBeInTheDocument()
    expect(await screen.findByText('Codex Implementer')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '在话题里看' }))
    expect(onOpenThread).toHaveBeenCalledWith('t9')
  })

  it('says so when nothing waits', async () => {
    stubApi({
      '/rooms/r1/approvals': { approvals: [] },
      '/rooms/r1/members': { members: [] },
      '/users': { users: [] },
    })
    renderWithProviders(<ApprovalsPanel roomId="r1" onClose={() => {}} onOpenThread={() => {}} />)
    expect(await screen.findByText('没有在等你的。')).toBeInTheDocument()
  })
})
