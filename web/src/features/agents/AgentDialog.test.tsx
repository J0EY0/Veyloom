import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { WikiCatalog, WikiPageInfo } from '@/api/types'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import { pickOption } from '@/test/select'
import { claude, codex, laptop, pi } from '@/test/machines'
import { AgentDialog } from './AgentDialog'
import { runtimeTraits } from '@/test/fixtures'

// jsdom cannot draw: the square is taken as given.
vi.mock('@/lib/avatarImage', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/avatarImage')>()),
  squareAvatar: vi.fn(async () => new Blob(['square'], { type: 'image/webp' })),
}))

const picture = '0123456789abcdef0123456789abcdef.webp'

// The skill library: one skill for every runtime, one kept for Claude
// Code, and a retired one.
function skillPage(name: string, title: string, overrides: Partial<WikiPageInfo> = {}): WikiPageInfo {
  return {
    path: `/skills/${name}/SKILL.md`,
    type: 'Skill',
    title,
    description: `Use for ${title}.`,
    tags: [],
    status: 'stable',
    tier: 'human-reviewed',
    modified: '2026-09-22T00:00:00Z',
    resident: false,
    ...overrides,
  }
}
const library: WikiCatalog = {
  pages: [
    skillPage('go-table-tests', 'Go table tests'),
    skillPage('claude-only', 'Claude only', { tags: ['runtime-claude'] }),
    skillPage('old-habit', 'Old habit', { status: 'deprecated' }),
    { ...skillPage('copy-paste', 'Copy-pasted tests'), path: '/patterns/copy-paste.md', type: 'Pattern' },
  ],
  dirs: [],
  folder: '/state/wiki/library',
  history: true,
}

// skillBoxes are the skills offered, by title, and which are ticked.
function skillBoxes() {
  return screen
    .getAllByRole('checkbox')
    .map((box) => `${box.closest('li')?.querySelector('label')?.firstChild?.textContent}${box.getAttribute('aria-checked') === 'true' ? ' ✓' : ''}`)
}

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
  skills: [] as string[],
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
      '/library': { wiki: library },
      '/skills/builtin': { skills: [{ name: 'team-practices', description: 'How members work together.' }] },
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
    await pickOption('权限', '完全信任')
    await userEvent.type(screen.getByLabelText('角色卡'), 'Review carefully.')
    await userEvent.type(screen.getByLabelText('运行时选项'), '{{"approval": true}')
    // Pi is offered the skills for every runtime, not the retired one;
    // Veyloom's own are named apart, with nothing to install.
    await waitFor(() => expect(skillBoxes()).toEqual(['Go table tests']))
    expect(screen.getByText('Veyloom 内置，所有 agent 默认可用，无需安装：')).toBeInTheDocument()
    expect(screen.getByText('team-practices')).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /team-practices/ })).toBeNull()
    await userEvent.click(screen.getByRole('checkbox', { name: /Go table tests/ }))
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
      skills: ['go-table-tests'],
    })
  })

  it('shows the skills an agent has that it is given no longer, to take off', async () => {
    let put: { skills?: string[] } = {}
    stubApi({
      '/machines': machines,
      '/library': { wiki: library },
      '/agents/t1': async (req) => {
        put = (await req.json()) as typeof put
        return { agent: { ...existing, ...put } }
      },
    })
    renderWithProviders(<AgentDialog agent={{ ...existing, skills: ['claude-only', 'gone-one', 'old-habit'] }} onClose={vi.fn()} />)
    await waitFor(() => expect(skillBoxes()).toEqual(['Claude only ✓', 'Go table tests', 'gone-one ✓', 'Old habit ✓']))
    expect(screen.getByText('仅适用于 Claude Code，取消勾选即可卸载。')).toBeInTheDocument()
    expect(screen.getByText('技能库中已没有这个技能，取消勾选即可卸载。')).toBeInTheDocument()
    expect(screen.getByText('已停用，不会再提供给 agent，取消勾选即可卸载。')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('checkbox', { name: /gone-one/ }))
    await userEvent.click(screen.getByRole('checkbox', { name: /Go table tests/ }))
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put.skills).toEqual(['claude-only', 'old-habit', 'go-table-tests']))
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
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('没有已连接的机器'))
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

  it("says a Codex agent's members start a new session once its role card changes", async () => {
    stubApi({ '/machines': machines, '/runtime-traits': runtimeTraits })
    renderWithProviders(<AgentDialog agent={{ ...existing, runtime: 'codex' }} onClose={vi.fn()} />)
    const hint = '修改角色卡后，Codex 成员会从下一轮开始使用新会话。'
    const roleCard = await screen.findByLabelText('角色卡')
    expect(screen.queryByText(hint)).not.toBeInTheDocument()
    await userEvent.type(roleCard, ' Always.')
    expect(screen.getByText(hint)).toBeInTheDocument()
    // Written back as it was, there is nothing to start over for.
    await userEvent.clear(roleCard)
    await userEvent.type(roleCard, 'Be brief.')
    expect(screen.queryByText(hint)).not.toBeInTheDocument()
  })

  it('says nothing of sessions for a runtime told its role card with every run', async () => {
    stubApi({ '/machines': machines, '/runtime-traits': runtimeTraits })
    renderWithProviders(<AgentDialog agent={existing} onClose={vi.fn()} />)
    await userEvent.type(await screen.findByLabelText('角色卡'), ' Always.')
    expect(screen.queryByText(/新会话/)).not.toBeInTheDocument()
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
    await waitFor(() => expect(screen.getByLabelText('机器')).toHaveTextContent('old-box（离线）'))
    expect(screen.getByLabelText('运行时')).toHaveTextContent('Claude Code')
  })

  it('says so when the agent joined a project while the dialog was open', async () => {
    stubApi({
      '/machines': machines,
      '/agents/t1': () => Response.json({ error: 'still a member of Veyloom', projects: ['Veyloom'] }, { status: 409 }),
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
    expect(await screen.findByText('头像需要是 PNG、JPG 或 WebP 图片。')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '移除头像' }))
    expect(screen.getByRole('button', { name: '上传头像' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(put).toMatchObject({ avatar: '' }))
  })
})
