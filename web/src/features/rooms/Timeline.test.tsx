import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { agentKeys } from '@/api/agents'
import { stubApi } from '@/test/fetch'
import { message, user } from '@/test/fixtures'
import { intersectAll } from '@/test/intersection'
import { renderWithProviders } from '@/test/render'
import { Timeline } from './Timeline'

describe('Timeline', () => {
  it('shows the latest page with names and loads older messages at the top', async () => {
    const latest = Array.from({ length: 50 }, (_, i) => message(`m${51 + i}`, 51 + i, { body: `msg ${51 + i}` }))
    const older = [message('m50', 50, { user_id: 'u2', body: 'the older one' })]
    const calls = stubApi({
      '/users': { users: [user('u1', 'alice'), user('u2', 'bob')] },
      '/rooms/r1/members': { members: [] },
      '/rooms/r1/messages': (req) => ({
        messages: new URL(req.url).searchParams.get('before') === '0' ? latest : older,
      }),
    })
    renderWithProviders(<Timeline roomId="r1" />)

    expect(await screen.findByText('msg 100')).toBeInTheDocument()
    expect(await screen.findAllByText('alice')).not.toHaveLength(0)
    expect(screen.queryByText('the older one')).not.toBeInTheDocument()

    intersectAll()
    expect(await screen.findByText('the older one')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(calls).toContain('GET /rooms/r1/messages?before=51&limit=50')
  })

  it('names agents and renders system notes quietly', async () => {
    stubApi({
      '/users': { users: [] },
      '/rooms/r1/members': { members: [{ id: 'a1', display_name: 'Claude Architect' }] },
      '/rooms/r1/messages': {
        messages: [
          message('m1', 1, { member_id: 'a1', user_id: undefined, body: '收到' }),
          message('m2', 2, { sender_kind: 'system', user_id: undefined, body: 'turn failed' }),
        ],
      },
    })
    renderWithProviders(<Timeline roomId="r1" />)
    expect(await screen.findByText('Claude Architect')).toBeInTheDocument()
    expect(screen.getByText('turn failed')).toBeInTheDocument()
  })

  it("shows an agent's own picture where it speaks and is mentioned, else its runtime's mark", async () => {
    const picture = '0123456789abcdef0123456789abcdef.webp'
    stubApi({
      '/users': { users: [user('u1', 'alice')] },
      '/agents': {
        agents: [
          { id: 'ag1', name: 'Architect', avatar: picture, runtime: 'claude' },
          { id: 'ag2', name: 'Tester', avatar: '', runtime: 'pi' },
        ],
      },
      '/rooms/r1/members': {
        members: [
          { id: 'a1', agent_id: 'ag1', display_name: 'Claude Architect' },
          { id: 'a2', agent_id: 'ag2', display_name: 'Pi Tester' },
        ],
      },
      '/rooms/r1/messages': {
        messages: [
          message('m1', 1, { member_id: 'a1', user_id: undefined, body: '@Pi Tester 你来', mentions: [{ kind: 'agent', id: 'a2' }] }),
          message('m2', 2, { member_id: 'a2', user_id: undefined, body: '好的' }),
          message('m3', 3, { body: '@Claude Architect 谢谢', mentions: [{ kind: 'agent', id: 'a1' }] }),
        ],
      },
    })
    renderWithProviders(<Timeline roomId="r1" />)
    const row = async (text: RegExp) => (await screen.findByText(text)).closest('li') as HTMLElement

    // The architect speaks with its picture; the tester it hands to has none.
    const architect = await row(/你来/)
    expect(architect.firstElementChild).toHaveAttribute('data-avatar', picture)
    expect(within(architect).getByText('Pi Tester').querySelector('svg')).not.toBeNull()
    // The tester speaks with Pi's mark, not an initial.
    const tester = await row(/好的/)
    expect(tester.querySelector('[data-avatar]')).toBeNull()
    expect(tester.firstElementChild?.querySelector('svg')).not.toBeNull()
    // A person mentioning the architect draws its picture in the pill.
    const thanks = await row(/谢谢/)
    expect(within(thanks).getByText('Claude Architect').querySelector(`[data-avatar="${picture}"]`)).not.toBeNull()
  })

  it('redraws a row drawn already when its agent takes a new picture', async () => {
    const picture = '0123456789abcdef0123456789abcdef.webp'
    let avatar = ''
    stubApi({
      '/users': { users: [] },
      '/agents': () => ({ agents: [{ id: 'ag1', name: 'Architect', avatar, runtime: 'claude' }] }),
      '/rooms/r1/members': { members: [{ id: 'a1', agent_id: 'ag1', display_name: 'Claude Architect' }] },
      '/rooms/r1/messages': { messages: [message('m1', 1, { member_id: 'a1', user_id: undefined, body: '收到' })] },
    })
    const { client } = renderWithProviders(<Timeline roomId="r1" />)
    const row = (await screen.findByText('收到')).closest('li') as HTMLElement
    await waitFor(() => expect(row.firstElementChild?.querySelector('svg')).not.toBeNull())

    avatar = picture
    await client.invalidateQueries({ queryKey: agentKeys.all })
    await waitFor(() => expect(row.firstElementChild).toHaveAttribute('data-avatar', picture))
  })

  it('shows an empty state', async () => {
    stubApi({ '/users': { users: [] }, '/rooms/r1/members': { members: [] }, '/rooms/r1/messages': { messages: [] } })
    renderWithProviders(<Timeline roomId="r1" />)
    expect(await screen.findByText('还没有消息。')).toBeInTheDocument()
  })
})
