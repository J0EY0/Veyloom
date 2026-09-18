import type { ActivityBucket, MachineActivity, MachineMember, Machine } from '@/api/types'

// A machine with what its discovery typically finds: Claude Code installed
// with nothing to run with yet, Codex missing, Pi set up, and the test
// runtime.
export const claude = { name: 'claude', binary: 'claude', path: '/opt/homebrew/bin/claude', version: '2.1.85', status: 'not_configured' }
export const codex = { name: 'codex', binary: 'codex', status: 'not_installed', detail: '"codex" not found on PATH' }
export const pi = { name: 'pi', binary: 'pi', path: '/usr/local/bin/pi', version: '0.73.1', status: 'ready' }

export function laptop(overrides: Partial<Machine> = {}): Machine {
  return {
    id: 'w1',
    name: 'laptop',
    runtimes: [claude, codex, pi, { name: 'fake', binary: '-', version: 'builtin', status: 'ready' }],
    connected_at: '2026-09-15T09:13:00Z',
    last_seen: new Date(Date.now() - 15_000).toISOString(),
    probed_at: new Date(Date.now() - 180_000).toISOString(),
    ...overrides,
  }
}

// A current member on the machine, idle unless told otherwise.
export function member(id: string, name: string, overrides: Partial<MachineMember> = {}): MachineMember {
  return {
    member: {
      id,
      room_id: 'r1',
      agent_id: 't1',
      machine_id: 'w1',
      display_name: name,
      repo_path: '/src/veyloom',
      branch_mode: 'worktree',
      model: '',
      permission_preset: '',
      enabled: true,
      created_at: '2026-09-15T09:00:00Z',
    },
    project_id: 'p1',
    project_name: 'Veyloom',
    ...overrides,
  }
}

// A day of activity: every hour empty except those given, by how many
// hours before now they started.
// activity is a machine's activity over a range, nothing in it but what
// busy puts in the buckets it names by how many ago: 0 is the one now.
export function activity(busy: Record<number, Partial<ActivityBucket>> = {}, overrides: Partial<MachineActivity> = {}): MachineActivity {
  const range = overrides.range ?? '24h'
  const count = { '24h': 24, '7d': 7, '30d': 30 }[range]
  const step = range === '24h' ? 'hour' : 'day'
  const length = step === 'day' ? 86_400_000 : 3_600_000
  const now = Date.now()
  const buckets = Array.from({ length: count }, (_, index) => ({
    start: new Date(now - (count - 1 - index) * length).toISOString(),
    turns: 0,
    failed: 0,
    tokens: 0,
    ...busy[count - 1 - index],
  }))
  return {
    range,
    step,
    buckets,
    turns: buckets.reduce((sum, bucket) => sum + bucket.turns, 0),
    failed: buckets.reduce((sum, bucket) => sum + bucket.failed, 0),
    median_ms: 0,
    usage: { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 },
    ...overrides,
  }
}
