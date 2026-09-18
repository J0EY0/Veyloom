import type { Agent, RuntimeInfo, Machine, MachineMember } from '@/api/types'
import { runtimeRank, visibleRuntimes } from '@/lib/runtimes'

// What a detected runtime's state means to someone who wants an agent to
// run on it: ready when it has something to run with (a sign-in, a key or a
// third-party provider, looked for on the machine only), unconfigured when
// it has none, failed when it did not answer its version check. Sign-in
// statuses an older hub stored read as ready: they were never a verdict on
// configuration.
export type RuntimeState = 'ready' | 'unconfigured' | 'failed'

export function runtimeState(status: string): RuntimeState {
  switch (status) {
    case 'not_configured':
      return 'unconfigured'
    case 'error':
      return 'failed'
    default:
      return 'ready'
  }
}

// Nothing to run with, or found but not answering: something for you to do.
export function needsYou(state: RuntimeState): boolean {
  return state !== 'ready'
}

export interface DetectedRuntime {
  info: RuntimeInfo
  state: RuntimeState
}

// The runtimes a machine actually found. What is not installed there is left
// out, as is the test runtime; the ones that need you come first.
export function detectedRuntimes(machine: Machine): DetectedRuntime[] {
  return visibleRuntimes(machine.runtimes)
    .filter((info) => info.status !== 'not_installed')
    .map((info) => ({ info, state: runtimeState(info.status) }))
    .sort((a, b) => Number(needsYou(b.state)) - Number(needsYou(a.state)) || runtimeRank(a.info.name) - runtimeRank(b.info.name))
}

// How a machine reads in the machine list: how many runtimes it found and
// how many of those need you.
export function runtimeCounts(machine: Machine): { detected: number; attention: number } {
  const runtimes = detectedRuntimes(machine)
  return { detected: runtimes.length, attention: runtimes.filter((runtime) => needsYou(runtime.state)).length }
}

// Whether an agent is at work on one machine: running while a member of it
// here has a turn in flight, a turn waiting for a person included; idle
// otherwise.
export type AgentRunState = 'running' | 'idle'

export interface MachineAgent {
  agent: Agent
  state: AgentRunState
  // Its current members on this machine, the running ones first.
  members: MachineMember[]
}

// Every agent as it stands on this machine, the running ones first,
// otherwise in the order they were made.
export function machineAgents(agents: Agent[], members: MachineMember[]): MachineAgent[] {
  return agents
    .map((agent, order) => {
      const own = members.filter((m) => m.member.agent_id === agent.id).sort((a, b) => Number(Boolean(b.turn)) - Number(Boolean(a.turn)))
      const state: AgentRunState = own.some((m) => m.turn) ? 'running' : 'idle'
      return { row: { agent, state, members: own }, order }
    })
    .sort((a, b) => Number(b.row.state === 'running') - Number(a.row.state === 'running') || a.order - b.order)
    .map(({ row }) => row)
}
