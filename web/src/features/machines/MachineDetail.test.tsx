import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { stubApi } from '@/test/fetch'
import { renderWithProviders } from '@/test/render'
import type { ActivityRange } from '@/api/types'
import { activity, claude, codex, laptop, member, pi } from '@/test/machines'
import { MachinesPage } from './MachinesPage'

// Matches the element holding exactly this text, however it is split into
// inner elements: the header's times are wrapped to stand out.
function whole(text: string) {
  return (_: string, el: Element | null) => el?.textContent === text && [...(el?.children ?? [])].every((child) => child.textContent !== text)
}

function open(stubs: Record<string, unknown> = {}) {
  const calls = stubApi({
    '/machines': { machines: [laptop()] },
    '/agents': { agents: [] },
    '/machines/w1/members': { members: [] },
    '/machines/w1/activity': { activity: activity() },
    ...stubs,
  })
  renderWithProviders(<MachinesPage />, { route: '/machines/w1', path: '/machines/:machineId' })
  return calls
}

describe('MachineDetail', () => {
  it('shows the machine and only the runtimes it detected, each with what to do', async () => {
    open()

    expect(await screen.findByRole('heading', { name: 'laptop' })).toBeInTheDocument()
    // On a phone this pane stands alone, so it links back to the list.
    expect(screen.getByRole('link', { name: '回到机器列表' })).toHaveAttribute('href', '/machines')
    expect(screen.getByText(whole('上次检测 3 分钟前')).closest('p')).toHaveTextContent(/前连上 · 最后心跳 15 秒前 · 上次检测 3 分钟前$/)
    // The times stand out from the words around them.
    expect(screen.getByText('15 秒')).toHaveClass('text-foreground')
    expect(screen.getByText('3 分钟')).toHaveClass('text-foreground')

    const rows = within(screen.getByRole('region', { name: '运行时' })).getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('Claude Code')
    expect(rows[0]).toHaveTextContent('2.1.85')
    expect(rows[0]).toHaveTextContent('/opt/homebrew/bin/claude')
    // Nothing to run with yet: yellow, and first. No sign-in wording.
    expect(within(rows[0]).getByText('未配置')).toBeInTheDocument()
    expect(rows[0]).not.toHaveTextContent('登录')
    // No command to copy: the state alone says what is needed.
    expect(within(rows[0]).queryByRole('button')).toBeNull()
    expect(rows[1]).toHaveTextContent('Pi')
    expect(rows[1]).toHaveTextContent(/Pi.*可用$/)
    expect(rows[1]).not.toHaveTextContent('登录')
    expect(within(rows[1]).queryByRole('button')).toBeNull()
    expect(screen.queryByText('Codex')).toBeNull()
    expect(screen.queryByText('fake')).toBeNull()
  })

  it('puts the last day above the runtimes and the agents below them', async () => {
    open()
    await screen.findByRole('region', { name: 'Agents' })
    expect(screen.getAllByRole('region').map((region) => region.getAttribute('aria-label'))).toEqual(['活动', '运行时', 'Agents'])
  })

  it('copies an install command with one click', async () => {
    open({ '/machines': { machines: [laptop({ runtimes: [codex] })] } })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: '复制命令 npm i -g @openai/codex' }))
    expect(await navigator.clipboard.readText()).toBe('npm i -g @openai/codex')
  })

  it('checks this machine again and shows what it found', async () => {
    let probed = false
    const calls = open({
      '/machines': () => ({
        machines: [
          probed
            ? laptop({ runtimes: [{ ...claude, status: 'ready' }, codex, pi], probed_at: new Date().toISOString() })
            : laptop({ runtimes: [{ ...claude, status: 'error', detail: 'version check failed: claude --version: exit status 1' }, codex, pi] }),
          laptop({ id: 'w2', name: 'build-box' }),
        ],
      }),
      '/machines/w1/probe': () => {
        probed = true
        return new Response(null, { status: 202 })
      },
    })
    expect(await screen.findByText('检测失败')).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: '重新检测' }))

    expect(await screen.findByText(whole('上次检测 刚刚'))).toBeInTheDocument()
    expect(calls.filter((call) => call.startsWith('POST'))).toEqual(['POST /machines/w1/probe'])
    expect(screen.queryByText('检测失败')).toBeNull()
  })

  it('says how to install a runtime on a machine that has none', async () => {
    open({ '/machines': { machines: [laptop({ runtimes: [codex] })] } })
    expect(await screen.findByText('这台机器上没有检测到运行时。安装一个，完成后点重新检测。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制命令 npm i -g @openai/codex' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制命令 npm i -g @anthropic-ai/claude-code' })).toBeInTheDocument()
  })

  it("lists this machine's agents as they stand, whoever needs a person first", async () => {
    const started = new Date(Date.now() - 42_000).toISOString()
    const agent = (id: string, name: string, runtime: string, machine_id = 'w1') => ({
      id,
      name,
      machine_id,
      machine_name: machine_id,
      projects: [],
      runtime,
      model: '',
      role_card: '',
      permission_preset: 'read_only',
      runtime_options: null,
    })
    const on = (agentId: string, id: string, name: string) => ({ ...member(id, name).member, agent_id: agentId })
    open({
      '/agents': {
        agents: [
          agent('t1', 'Fresh', 'pi'),
          agent('t2', 'Claude Architect', 'claude'),
          agent('t3', 'Codex Helper', 'codex'),
          agent('t4', 'Pi Tester', 'pi'),
          agent('t5', 'Reviewer', 'pi'),
          agent('t6', 'Release Helper', 'pi'),
          agent('t7', 'Idle One', 'pi'),
          // Set up on another machine: not this one's.
          agent('t8', 'Elsewhere', 'pi', 'w2'),
        ],
      },
      '/machines/w1/members': {
        members: [
          member('a4', 'Pi Tester', { member: on('t4', 'a4', 'Pi Tester'), turn: { id: 'x1', thread_id: 'th1', started_at: started } }),
          member('a5', 'Reviewer', {
            member: on('t5', 'a5', 'Reviewer'),
            approval: { id: 'ap1', tool: 'Bash', input: { command: 'make test' } },
            turn: { id: 'x2', thread_id: 'th2', started_at: started },
          }),
          member('a6', 'Release Helper', { member: { ...on('t6', 'a6', 'Release Helper'), enabled: false } }),
          member('a7', 'Idle One', { member: { ...on('t7', 'a7', 'Idle One'), room_id: 'r2' }, project_name: 'docs-site' }),
        ],
      },
      '/machines/w1/activity': { activity: activity({ 2: { turns: 3 } }) },
    })

    const region = await screen.findByRole('region', { name: 'Agents' })
    const rows = await within(region).findAllByRole('listitem')
    // Running first, then the order they were made; nothing but running or idle.
    expect(rows.map((row) => row.textContent)).toEqual([
      'Pi TesterPi · Veyloom运行中',
      'ReviewerPi · Veyloom运行中',
      'FreshPi空闲',
      'Claude ArchitectClaude Code空闲',
      'Codex HelperCodex空闲',
      'Release HelperPi · Veyloom空闲',
      'Idle OnePi · docs-site空闲',
    ])
    const link = (row: HTMLElement) => within(row).queryByRole('link')
    expect(link(rows[0])).toHaveAttribute('href', '/rooms/r1?thread=th1')
    expect(link(rows[1])).toHaveAttribute('href', '/rooms/r1?thread=th2')
    // Not in a project here: nowhere to go.
    expect(link(rows[2])).toBeNull()
    expect(link(rows[6])).toHaveAttribute('href', '/rooms/r2?panel=members')

    // The counts over the sections, each number in bold.
    expect((await screen.findByText('个正在运行')).closest('p')).toHaveTextContent('2个运行时7个 Agent2个正在运行3轮（24 小时）')
  })

  it('says so when there are no agents yet', async () => {
    open()
    expect(await screen.findByText('还没有 Agent。')).toBeInTheDocument()
  })

  it('charts turns and tokens over the last 24 hours to begin with', async () => {
    const usage = { input_tokens: 3_456, cache_read_tokens: 88_000, cache_write_tokens: 0, output_tokens: 2_000 }
    open({
      '/machines/w1/activity': {
        activity: activity({ 1: { turns: 2, tokens: 90_000 }, 5: { turns: 1, failed: 1, tokens: 3_456 } }, { median_ms: 42_000, usage }),
      },
    })

    const turns = await screen.findByRole('figure', { name: '轮次' })
    expect(await within(turns).findByText('3 轮 · 1 次失败 · 中位用时 42 秒')).toBeInTheDocument()
    // The tokens in short, and what they were made of on hover.
    const tokens = screen.getByRole('figure', { name: 'Token' })
    await userEvent.hover(within(tokens).getByText('9.3万 token'))
    expect(await screen.findByRole('tooltip')).toHaveTextContent('输入 3,456 · 缓存读取 88,000 · 输出 2,000')
    // Every hour is in the tables a screen reader gets.
    expect(within(within(turns).getByRole('table', { name: '轮次 · 最近 24 小时' })).getAllByRole('row')).toHaveLength(25)
    expect(within(within(tokens).getByRole('table', { name: 'Token · 最近 24 小时' })).getByRole('cell', { name: '90,000' })).toBeInTheDocument()
  })

  it('switches each card to its own range, and the turns card to one runtime', async () => {
    const calls = open({
      '/machines/w1/activity': (req: Request) => {
        const params = new URL(req.url).searchParams
        const range = params.get('range') as ActivityRange
        return { activity: activity({ 0: { turns: params.get('runtime') === 'pi' ? 1 : 4 } }, { range }) }
      },
    })
    const user = userEvent.setup()
    const turns = await screen.findByRole('figure', { name: '轮次' })
    const tokens = screen.getByRole('figure', { name: 'Token' })
    expect(await within(turns).findByText('4 轮')).toBeInTheDocument()

    // A week is seven days.
    await user.click(within(turns).getByRole('tab', { name: '7 天' }))
    const week = await within(turns).findByRole('table', { name: '轮次 · 最近 7 天' })
    expect(within(week).getAllByRole('row')).toHaveLength(8)
    // The tokens card keeps its own range.
    expect(within(tokens).getByRole('table', { name: 'Token · 最近 24 小时' })).toBeInTheDocument()

    // Only the runtimes found on this machine, by their product names.
    await user.click(within(turns).getByRole('combobox', { name: '按运行时筛选' }))
    expect((await screen.findAllByRole('option')).map((option) => option.textContent)).toEqual(['全部运行时', 'Claude Code', 'Pi'])
    await user.click(screen.getByRole('option', { name: 'Pi' }))
    expect(await within(turns).findByText('1 轮')).toBeInTheDocument()
    // Hours and days are asked for in the browser's own time zone.
    expect(calls).toContainEqual(expect.stringMatching(/^GET \/machines\/w1\/activity\?range=7d&tz=[^&]+&runtime=pi$/))

    await user.click(within(tokens).getByRole('tab', { name: '30 天' }))
    expect(within(await within(tokens).findByRole('table', { name: 'Token · 最近 30 天' })).getAllByRole('row')).toHaveLength(31)
    // No tokens: no summary under the name.
    expect(within(tokens).queryByText(/token/)).toBeNull()
  })

  it('draws empty charts without a summary when nothing ran', async () => {
    open()
    const turns = await screen.findByRole('figure', { name: '轮次' })
    const tokens = screen.getByRole('figure', { name: 'Token' })
    // The empty bars say it; no line under the names repeats it.
    expect(await within(turns).findByRole('table', { name: '轮次 · 最近 24 小时' })).toBeInTheDocument()
    expect(within(tokens).getByRole('table', { name: 'Token · 最近 24 小时' })).toBeInTheDocument()
    expect(turns.querySelector('[data-slot=card-description]')).toBeNull()
    expect(tokens.querySelector('[data-slot=card-description]')).toBeNull()
  })
})
