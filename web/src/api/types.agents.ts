// Wire types for agents, members, machines and runtimes; see types.ts.

import type { TokenUsage } from './types'

export type PermissionPreset = 'read_only' | 'edit_with_approval' | 'auto_review' | 'full_auto'

// Something a member may always do without a person being asked, in its
// runtime's terms: a Claude Code permission rule such as Bash(go test:*),
// or a Codex command prefix as a JSON array such as ["go","test"]
// (GET /members/{id}/rules).
export interface MemberRule {
  id: string
  member_id: string
  runtime: string
  rule: string
  approval_id?: string
  created_by?: string
  created_at: string
}

export interface MemberRulesResponse {
  rules: MemberRule[]
}

// A member's conversation with its runtime (GET /members/{id}/session).
// Sessions look after themselves: they are resumed turn after turn,
// compacted by the runtime when they grow, and replaced when they cannot
// go on. This is for looking, and for the rare reset by hand.
export interface MemberSession {
  id: string
  member_id: string
  runtime: string
  work_dir: string
  // How many times the runtime has compacted it.
  compactions: number
  started_at: string
}

export interface MemberSessionResponse {
  // Absent before the member's first turn and after a reset.
  session?: MemberSession
  // Turns run in it.
  turns: number
}

export interface Member {
  id: string
  room_id: string
  agent_id: string
  machine_id: string
  display_name: string
  repo_path: string
  branch_mode: string
  // Empty means the agent's value applies.
  model: string
  permission_preset: PermissionPreset | ''
  enabled: boolean
  created_at: string
  // Its git worktree once made (docs/design.md 5.21): the folder, where in
  // it the member works, the branch there, and when it was got ready.
  worktree_dir?: string
  work_dir?: string
  branch?: string
  prepared_at?: string
  // Set once the member is taken out of the project. The room still lists
  // it so its messages and turns keep a name; agent_id is empty once
  // the agent it came from is deleted.
  removed_at?: string
}

export interface MembersResponse {
  members: Member[]
}

export interface MemberResponse {
  member: Member
}

// POST /rooms/{id}/members: the member runs on the machine its agent is set
// up on, so there is no machine to name.
export interface CreateMemberRequest {
  agent_id: string
  display_name?: string
  repo_path?: string
  branch_mode?: string
  model?: string
  permission_preset?: PermissionPreset | ''
}

// PATCH /members/{id}: every field optional, absent means unchanged.
export interface UpdateMemberRequest {
  display_name?: string
  model?: string
  permission_preset?: PermissionPreset | ''
  repo_path?: string
  enabled?: boolean
}

export interface Agent {
  id: string
  name: string
  // The picture uploaded for the agent, by its name under /avatars; empty
  // shows the runtime's mark.
  avatar: string
  // The machine the agent is set up on, where its members run, and that
  // machine's name whether it is connected or not.
  machine_id: string
  machine_name: string
  // The projects it is a current member of: while there are any it stays
  // on its machine and cannot be deleted.
  projects: string[]
  runtime: string
  model: string
  role_card: string
  permission_preset: PermissionPreset
  runtime_options: Record<string, unknown> | null
  // The skills of the library installed for it, by name: what its turns
  // are given (docs/design.md 5.15).
  skills: string[]
  created_at: string
  updated_at: string
}

export interface AgentsResponse {
  agents: Agent[]
}

export interface RuntimeInfo {
  name: string
  binary: string
  path?: string
  version?: string
  status: string
  detail?: string
}

export interface Machine {
  id: string
  name: string
  runtimes: RuntimeInfo[] | null
  connected_at: string
  last_seen: string
  // When the runtimes were last discovered; moves once a probe is answered.
  probed_at: string
  // How each runtime's account stands against its usage limits, by
  // runtime, as its turns last reported (docs/design.md 5.23.3).
  quotas?: Record<string, Quota>
}

// How a runtime's account stands against its usage limits: the limit
// nearest to being reached, or the one reached.
export interface Quota {
  limited?: boolean
  // The limit by its span: "5h", "7d", "7d opus".
  window?: string
  used_percent?: number
  resets_at?: string
}

// Why a pause keeps turns from starting: an account signed out, its usage
// limit reached, too many requests, its provider failing; or a member
// whose turns keep failing.
export type PauseReason = 'auth' | 'quota' | 'rate_limit' | 'server' | 'failing'

// What keeps turns from starting that would only fail (docs/design.md
// 5.23.3): an account's, its machine and runtime set, or a member's.
export interface Pause {
  id: string
  machine_id?: string
  runtime?: string
  member_id?: string
  reason: PauseReason
  // What the runtime said, the last time it failed.
  detail: string
  // When it runs out; absent: when a person resumes.
  ends_at?: string
  created_at: string
}

export interface PausesResponse {
  pauses: Pause[]
}

// A current member a machine runs, with the project it is in and what it
// is doing now: GET /machines/{id}/members.
export interface MachineMember {
  member: Member
  project_id: string
  project_name: string
  // Its turn in flight, if it has one.
  turn?: { id: string; thread_id: string; started_at: string }
  // The oldest request of its that waits for a person, if any.
  approval?: { id: string; tool: string; input: unknown }
}

export interface MachineMembersResponse {
  members: MachineMember[]
}

// How far back a machine's activity looks: the last 24 hours and 7 days
// hour by hour, the last 30 days day by day.
export type ActivityRange = '24h' | '7d' | '30d'

// One hour or day of a machine's turns: how many started in it, how many
// of those failed, and every token they spent.
export interface ActivityBucket {
  start: string
  turns: number
  failed: number
  tokens: number
}

// What a machine did over a range: GET /machines/{id}/activity.
export interface MachineActivity {
  range: ActivityRange
  step: 'hour' | 'day'
  // The range's hours or days in the browser's time zone, oldest first;
  // the last one holds now.
  buckets: ActivityBucket[]
  turns: number
  failed: number
  // Median duration of the turns that ended; 0 when none has.
  median_ms: number
  // The tokens all of them spent, part by part.
  usage: TokenUsage
}

export interface MachineActivityResponse {
  activity: MachineActivity
}

export interface MachinesResponse {
  machines: Machine[]
}

export interface AgentResponse {
  agent: Agent
}

// POST and PUT on agents take the whole agent.
export interface AgentRequest {
  name: string
  avatar: string
  machine_id: string
  runtime: string
  model: string
  role_card: string
  permission_preset: PermissionPreset
  runtime_options: Record<string, unknown>
  // Absent keeps the skills the agent has.
  skills?: string[]
}
