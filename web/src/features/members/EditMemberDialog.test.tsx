import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { Member } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { EditMemberDialog } from './EditMemberDialog'

const member: Member = {
  id: 'a1',
  room_id: 'r1',
  agent_id: 't1',
  machine_id: 'w1',
  display_name: 'Reviewer',
  repo_path: '/src/linkkeeper',
  branch_mode: '',
  model: '',
  permission_preset: '',
  enabled: true,
  created_at: '2026-09-25T01:00:00Z',
}

const agents = {
  agents: [
    {
      id: 't1',
      name: 'Reviewer',
      machine_id: 'w1',
      machine_name: 'laptop',
      projects: [],
      runtime: 'claude',
      model: '',
      permission_preset: 'edit_with_approval',
    },
  ],
}

describe('EditMemberDialog', () => {
  it('picks one of the four presets, each said in a line, or the agent’s', async () => {
    let patched: unknown
    stubApi({
      '/agents': agents,
      '/members/a1/session': { turns: 0 },
      '/members/a1/rules': { rules: [] },
      '/members/a1': async (req: Request) => ((patched = await req.json()), { member: { ...member, permission_preset: 'auto_review' } }),
    })
    renderWithProviders(<EditMemberDialog roomId="r1" member={member} onClose={vi.fn()} />)
    const presets = await screen.findByRole('radiogroup')
    expect(within(presets).getAllByRole('radio')).toHaveLength(5)
    expect(within(presets).getByRole('radio', { name: '跟随 Agent' })).toBeChecked()
    expect(await within(presets).findByText('现在是需要审批')).toBeInTheDocument()
    expect(within(presets).getByText('由运行时判断风险，拿不准再问')).toBeInTheDocument()
    expect(screen.getByText('在审批里选“始终允许”后会列在这里')).toBeInTheDocument()

    await userEvent.click(within(presets).getByRole('radio', { name: '自动审核' }))
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(patched).toEqual({ display_name: 'Reviewer', repo_path: '/src/linkkeeper', permission_preset: 'auto_review', model: '' }))
  })

  it('lists what the member may always do, and takes one back', async () => {
    const calls = stubApi({
      '/agents': agents,
      '/members/a1/session': { turns: 0 },
      '/members/a1/rules': {
        rules: [
          { id: 'r1', member_id: 'a1', runtime: 'claude', rule: 'Bash(go test:*)', created_at: '2026-09-25T02:00:00Z' },
          { id: 'r2', member_id: 'a1', runtime: 'codex', rule: '["go","vet"]', created_at: '2026-09-25T03:00:00Z' },
        ],
      },
      '/members/a1/rules/r1': new Response(null, { status: 204 }),
    })
    renderWithProviders(<EditMemberDialog roomId="r1" member={member} onClose={vi.fn()} />)
    const rules = await screen.findByRole('list', { name: '始终允许的命令' })
    expect(within(rules).getByText('go test *')).toBeInTheDocument()
    // A rule kept for another runtime says whose it is.
    const vet = within(rules).getByText('go vet *').closest('li') as HTMLElement
    await waitFor(() => expect(vet).toHaveTextContent('Codex'))

    await userEvent.click(within(rules).getByRole('button', { name: '撤销 go test *' }))
    await waitFor(() => expect(calls).toContain('DELETE /members/a1/rules/r1'))
    await waitFor(() => expect(within(rules).queryByText('go test *')).toBeNull())
  })
})
