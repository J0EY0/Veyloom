import { describe, expect, it } from 'vitest'
import type { Agent, RuntimeInfo, Machine } from '@/api/types'
import { member } from '@/test/machines'
import { detectedRuntimes, runtimeState, machineAgents, runtimeCounts } from './machines'

function machine(runtimes: RuntimeInfo[] | null): Machine {
  return { id: 'w1', name: 'mb0', runtimes, connected_at: '2026-09-15T09:13:00Z', last_seen: '2026-09-16T09:00:00Z', probed_at: '2026-09-15T09:13:00Z' }
}

const claude = { name: 'claude', binary: 'claude', version: '2.1.85', status: 'ready' }
const codex = { name: 'codex', binary: 'codex', status: 'not_installed', detail: '"codex" not found on PATH' }
const pi = { name: 'pi', binary: 'pi', version: '0.73.1', status: 'ready' }
const fake = { name: 'fake', binary: '-', version: 'builtin', status: 'ready' }

describe('runtimeState', () => {
  it('reads what the machine found set up, and nothing else, as ready', () => {
    // auth_unknown and not_logged_in are what an older hub stored: sign-in, not configuration.
    expect(['ready', 'not_configured', 'error', 'auth_unknown', 'not_logged_in'].map(runtimeState)).toEqual([
      'ready',
      'unconfigured',
      'failed',
      'ready',
      'ready',
    ])
  })
})

describe('detectedRuntimes', () => {
  it('lists only what the machine found, what needs you first', () => {
    const unconfigured = { ...claude, status: 'not_configured' }
    const broken = { ...pi, status: 'error', detail: 'version check failed' }
    const runtimes = detectedRuntimes(machine([unconfigured, codex, fake, broken]))
    expect(runtimes.map((runtime) => [runtime.info.name, runtime.state])).toEqual([
      ['claude', 'unconfigured'],
      ['pi', 'failed'],
    ])
    expect(runtimeCounts(machine([unconfigured, codex, fake, broken]))).toEqual({ detected: 2, attention: 2 })
    // Set up, and what an older hub said about sign-in, need nothing from you.
    expect(runtimeCounts(machine([{ ...claude, status: 'not_logged_in' }, pi]))).toEqual({ detected: 2, attention: 0 })
  })

  it('keeps the usual order among runtimes that are fine', () => {
    const runtimes = detectedRuntimes(
      machine([
        { ...pi, status: 'ready' },
        { ...claude, status: 'ready' },
      ]),
    )
    expect(runtimes.map((runtime) => runtime.info.name)).toEqual(['claude', 'pi'])
  })

  it('finds nothing on a machine still probing or with nothing installed', () => {
    expect(detectedRuntimes(machine(null))).toEqual([])
    expect(runtimeCounts(machine([codex, fake]))).toEqual({ detected: 0, attention: 0 })
  })
})

describe('machineAgents', () => {
  const agent = (id: string): Agent => ({
    id,
    name: id,
    avatar: '',
    machine_id: 'w1',
    machine_name: 'mb0',
    projects: [],
    runtime: 'pi',
    model: '',
    role_card: '',
    permission_preset: 'read_only',
    runtime_options: null,
    created_at: '2026-09-16T00:00:00Z',
    updated_at: '2026-09-16T00:00:00Z',
  })
  const onAgent = (agentId: string, id: string) => ({ ...member(id, id).member, agent_id: agentId })
  const started = { id: 'x1', thread_id: 'th1', started_at: '2026-09-16T09:00:00Z' }

  it('is running while a member here has a turn in flight, and idle otherwise', () => {
    const rows = machineAgents(
      [agent('fresh'), agent('idle'), agent('busy'), agent('asking'), agent('off')],
      [
        member('m1', 'm1', { member: onAgent('idle', 'm1') }),
        member('m2', 'm2', { member: onAgent('busy', 'm2'), turn: started }),
        member('m3', 'm3', { member: onAgent('asking', 'm3'), turn: started, approval: { id: 'ap1', tool: 'Bash', input: { command: 'make test' } } }),
        member('m4', 'm4', { member: { ...onAgent('off', 'm4'), enabled: false } }),
      ],
    )
    // Running first, then the order they were made.
    expect(rows.map((row) => [row.agent.id, row.state])).toEqual([
      ['busy', 'running'],
      ['asking', 'running'],
      ['fresh', 'idle'],
      ['idle', 'idle'],
      ['off', 'idle'],
    ])
    expect(rows[3].members.map((m) => m.member.id)).toEqual(['m1'])
  })

  it('puts the running member of an agent first', () => {
    const [row] = machineAgents(
      [agent('a')],
      [member('m1', 'm1', { member: onAgent('a', 'm1') }), member('m2', 'm2', { member: onAgent('a', 'm2'), turn: started })],
    )
    expect(row.members.map((m) => m.member.id)).toEqual(['m2', 'm1'])
  })
})
