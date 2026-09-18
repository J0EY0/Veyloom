import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { pickOption } from '@/test/select'
import { claude, codex, laptop, pi } from '@/test/machines'
import { AgentDialog } from './AgentDialog'

// jsdom cannot draw: the square is taken as given.
vi.mock('@/lib/avatarImage', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/avatarImage')>()),
  squareAvatar: vi.fn(async () => new Blob(['square'], { type: 'image/webp' })),
}))

const picture = '0123456789abcdef0123456789abcdef.webp'

// Two machines: Claude Code on the laptop with Codex missing, Pi and the
// test runtime on the build box.
const machines = {
  machines: [laptop({ runtimes: [claude, codex] }), laptop({ id: 'w2', name: 'build-box', runtimes: [pi, { name: 'fake', binary: '-', status: 'ready' }] })],
}
const existing = {
  id: 't1',
  name: 'Fake Implementer',
  avatar: '',
  machine_id: 'w1',
  machine_name: 'laptop',
  projects: [] as string[],
  runtime: 'fake',
  model: '',
  role_card: 'Be brief.',
  permission_preset: 'read_only' as const,
  runtime_options: { tool: true },
  created_at: '2026-09-14T00:00:00Z',
  updated_at: '2026-09-14T00:00:00Z',
}

async function options(label: string) {
  await userEvent.click(screen.getByLabelText(label))
  const found = (await screen.findAllByRole('option')).map((option) => option.textContent)
  await userEvent.keyboard('{Escape}')
  return found
}

describe('AgentDialog', () => {
  it('sets an agent up on a machine and one of its runtimes', async () => {
    let posted: unknown
    stubApi({
      '/machines': machines,
      '/agents': async (req) => {
        if (req.method === 'POST') {
          posted = await req.json()
          return Response.json({ agent: { id: 't9', ...(posted as object) } }, { status: 201 })
        }
        return { agents: [] }
      },
    })
    const onClose = vi.fn()
    renderWithProviders(<AgentDialog onClose={onClose} />)

    expect(await screen.findByRole('dialog', { name: '新建 Agent' })).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('名称'), 'Reviewer')
    // The first machine is picked once the list arrives, with its runtime.
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('laptop'))
    expect(screen.getByLabelText('运行时')).toHaveTextContent('Claude Code')
    await pickOption('机器', 'build-box')
    expect(screen.getByLabelText('运行时')).toHaveTextContent('Pi')
    await pickOption('权限', '全自动')
    await userEvent.type(screen.getByLabelText('角色卡'), 'Review carefully.')
    await userEvent.type(screen.getByLabelText('运行时选项'), '{{"approval": true}')
    await userEvent.click(screen.getByRole('button', { name: '创建 Agent' }))

    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(posted).toEqual({
      name: 'Reviewer',
      avatar: '',
      machine_id: 'w2',
      runtime: 'pi',
      model: '',
      role_card: 'Review carefully.',
      permission_preset: 'full_auto',
      runtime_options: { approval: true },
    })
  })

  it('offers only what the picked machine found, not what it lacks nor the test runtime', async () => {
    stubApi({ '/machines': machines, '/agents': { agents: [] } })
    renderWithProviders(<AgentDialog onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('laptop'))
    expect(await options('机器')).toEqual(['laptop', 'build-box'])
    expect(await options('运行时')).toEqual(['Claude Code'])
    await pickOption('机器', 'build-box')
    expect(await options('运行时')).toEqual(['Pi'])
  })

  it('will not create an agent without a machine or a runtime on it', async () => {
    stubApi({ '/machines': { machines: [laptop({ runtimes: [codex] })] }, '/agents': { agents: [] } })
    const { unmount } = renderWithProviders(<AgentDialog onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByLabelText('运行时')).toHaveTextContent('这台机器上没有检测到运行时'))
    expect(screen.getByLabelText('运行时')).toBeDisabled()
    expect(screen.getByRole('button', { name: '创建 Agent' })).toBeDisabled()
    unmount()

    stubApi({ '/machines': { machines: [] }, '/agents': { agents: [] } })
    renderWithProviders(<AgentDialog onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('没有连上的机器'))
    expect(screen.getByLabelText('机器')).toBeDisabled()
    expect(screen.getByRole('button', { name: '创建 Agent' })).toBeDisabled()
  })

  it('refuses options that are not a JSON object', async () => {
    const calls = stubApi({ '/machines': machines, '/agents': { agents: [] } })
    const onClose = vi.fn()
    renderWithProviders(<AgentDialog onClose={onClose} />)
    await userEvent.type(await screen.findByLabelText('名称'), 'x')
    await waitFor(() => expect(screen.getByLabelText('运行时')).toHaveTextContent('Claude Code'))
    await userEvent.type(screen.getByLabelText('运行时选项'), '[[1, 2]')
    await userEvent.click(screen.getByRole('button', { name: '创建 Agent' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('要是一个 JSON 对象')
    expect(calls.filter((c) => c.startsWith('POST'))).toEqual([])
    expect(onClose).not.toHaveBeenCalled()
  })

  it('edits an agent with a PUT, keeping a runtime its machine does not report', async () => {
    let put: unknown
    stubApi({
      '/machines': machines,
      '/agents/t1': async (req) => {
        put = await req.json()
        return { agent: { ...existing, ...(put as object) } }
      },
    })
    const onClose = vi.fn()
    renderWithProviders(<AgentDialog agent={existing} onClose={onClose} />)

    expect(await screen.findByRole('dialog', { name: 'Fake Implementer' })).toBeInTheDocument()
    const name = screen.getByLabelText('名称')
    expect(name).toHaveValue('Fake Implementer')
    expect(screen.getByLabelText('机器')).toHaveTextContent('laptop')
    expect(screen.getByLabelText('运行时')).toHaveTextContent('fake')
    expect(screen.getByLabelText('运行时选项')).toHaveValue('{\n  "tool": true\n}')

    await userEvent.clear(name)
    await userEvent.type(name, 'Fake Reviewer')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(put).toMatchObject({ name: 'Fake Reviewer', machine_id: 'w1', runtime: 'fake', role_card: 'Be brief.', runtime_options: { tool: true } })
  })

  it('keeps an agent that is in a project on its machine, and says why', async () => {
    stubApi({ '/machines': machines })
    renderWithProviders(<AgentDialog agent={{ ...existing, projects: ['Veyloom', 'docs-site'] }} onClose={vi.fn()} />)
    const machine = await screen.findByLabelText('机器')
    expect(machine).toBeDisabled()
    await userEvent.hover(machine.parentElement!)
    expect(await screen.findByRole('tooltip')).toHaveTextContent('还在 Veyloom、docs-site 中，先在成员面板将其移出，再换机器。')
  })

  it('shows the machine an agent is on while that machine is offline', async () => {
    stubApi({ '/machines': machines })
    renderWithProviders(<AgentDialog agent={{ ...existing, machine_id: 'w9', machine_name: 'old-box', runtime: 'claude' }} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('old-box（不在线）'))
    expect(screen.getByLabelText('运行时')).toHaveTextContent('Claude Code')
  })

  it('says so when the agent joined a project while the dialog was open', async () => {
    stubApi({
      '/machines': machines,
      '/agents/t1': () => Response.json({ error: 'agent t1: store: conflict', projects: ['Veyloom'] }, { status: 409 }),
    })
    renderWithProviders(<AgentDialog agent={existing} onClose={vi.fn()} />)
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('laptop'))
    await pickOption('机器', 'build-box')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('还在 Veyloom 中，先在成员面板将其移出，再换机器。')).toBeInTheDocument()
  })
  it('gives a new agent the picture picked, squared and uploaded before it is saved', async () => {
    let uploaded: Blob | undefined
    let posted: unknown
    stubApi({
      '/machines': machines,
      '/avatars': async (req: Request) => {
        uploaded = await req.blob()
        return Response.json({ avatar: picture }, { status: 201 })
      },
      '/agents': async (req: Request) => {
        if (req.method !== 'POST') return { agents: [] }
        posted = await req.json()
        return Response.json({ agent: { id: 't9', ...(posted as object) } }, { status: 201 })
      },
    })
    renderWithProviders(<AgentDialog onClose={vi.fn()} />)

    // Until a picture is picked the runtime's mark stands in.
    const face = await screen.findByRole('button', { name: '上传头像' })
    expect(face.querySelector('[data-avatar]')).toBeNull()
    await userEvent.upload(screen.getByLabelText('头像'), new File(['png'], 'me.png', { type: 'image/png' }))

    expect(await screen.findByRole('button', { name: '更换头像' })).toContainElement(document.querySelector(`[data-avatar="${picture}"]`) as HTMLElement)
    expect(uploaded?.type).toBe('image/webp')
    await userEvent.type(screen.getByLabelText('名称'), 'Reviewer')
    await userEvent.click(screen.getByRole('button', { name: '创建 Agent' }))
    await waitFor(() => expect(posted).toMatchObject({ name: 'Reviewer', avatar: picture }))
  })

  it('takes an avatar off, and refuses a picture it cannot keep', async () => {
    let put: unknown
    stubApi({
      '/machines': machines,
      '/agents/t1': async (req: Request) => {
        put = await req.json()
        return { agent: { ...existing, ...(put as object) } }
      },
    })
    const user = userEvent.setup({ applyAccept: false })
    renderWithProviders(<AgentDialog agent={{ ...existing, avatar: picture }} onClose={vi.fn()} />)

    expect(await screen.findByRole('button', { name: '更换头像' })).toBeInTheDocument()
    await user.upload(screen.getByLabelText('头像'), new File(['gif'], 'me.gif', { type: 'image/gif' }))
    expect(await screen.findByText('头像要是 PNG、JPG 或 WebP 图片。')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '移除头像' }))
    expect(screen.getByRole('button', { name: '上传头像' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toMatchObject({ avatar: '' }))
  })
})
