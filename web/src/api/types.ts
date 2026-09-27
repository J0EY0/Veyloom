// Wire types for /api/v1. Field names match the JSON tags on the Go
// structs in internal/store and internal/api; change both together.

import type { FormAnswer, QuestionAnswers } from './types.approvals'
import type { Pause } from './types.agents'
import type { TurnEvent } from './types.turns'
import type { Attachment } from './types.attachments'
import type { Project } from './types.projects'
import type { Reminder } from './reminders'
import type { Draft } from './drafts'

export type RoomKind = 'main' | 'topic'

export interface Room {
  id: string
  project_id: string
  name: string
  kind: RoomKind
  created_at: string
}

export interface User {
  id: string
  name: string
  created_at: string
}

export interface ProjectsResponse {
  projects: Project[]
}

export interface ProjectResponse {
  project: Project
  rooms: Room[]
}

export interface RoomsResponse {
  rooms: Room[]
}

export interface RoomResponse {
  room: Room
}

export interface UsersResponse {
  users: User[]
}

export interface UserResponse {
  user: User
}

// GET /auth/status: whether the one account still has to be created, and
// who is signed in, if anyone.
export interface AuthStatus {
  setup_required: boolean
  user: User | null
}

export interface ErrorResponse {
  error: string
}

export interface CreateRoomRequest {
  name: string
}

export interface CreateUserRequest {
  name: string
}

export type SenderKind = 'user' | 'agent' | 'system'

export type MentionKind = 'user' | 'agent'

export interface Mention {
  kind: MentionKind
  id: string
}

export interface Message {
  id: string
  seq: number
  room_id: string
  // Absent on a top-level message.
  thread_id?: string
  sender_kind: SenderKind
  user_id?: string
  member_id?: string
  body: string
  // Go encodes an empty slice as null.
  mentions: Mention[] | null
  // Files a person attached; absent or null on hub-written messages.
  attachments?: Attachment[] | null
  // Set on messages the hub wrote for a turn: a topic root once filled in,
  // the agent's later replies, system notes.
  turn_id?: string
  // What a member handing work on called that task (send_message's title).
  title?: string
  created_at: string
}

export interface MessagesResponse {
  messages: Message[]
}

export interface MessageResponse {
  message: Message
}

export interface PostMessageRequest {
  user_id: string
  body: string
  mentions: Mention[]
  // Uploads from POST /rooms/{id}/attachments to carry with the message.
  attachment_ids?: string[]
  thread_id?: string
  reply_to?: string
}

export interface TurnsResponse {
  turns: Turn[]
}

export type TurnStatus = 'running' | 'done' | 'failed' | 'cancelled'

// The tokens a turn spent as its runtime reported them, in parts that do
// not overlap. No cost is kept.
export interface TokenUsage {
  input_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  output_tokens: number
}

export interface Turn {
  id: string
  member_id: string
  room_id: string
  thread_id: string
  trigger_message_id?: string
  machine_id: string
  // The member's session the turn ran in; absent for a turn that never got one.
  session_id?: string
  status: TurnStatus
  error?: string
  reply_message_id?: string
  transcript_path?: string
  // All zero until the turn ends, and for a runtime that reports none.
  usage: TokenUsage
  // The files the turn wrote, each once, in the order first touched.
  files_changed?: string[]
  // What the turn was for: the chat, or the wiki maintainer's upkeep.
  kind?: TurnKind
  // The piece of work it is part of, and the turn whose message woke it
  // when an agent did (docs/design.md 5.22); worked says it did work.
  chain_message_id?: string
  woken_by_turn_id?: string
  worked?: boolean
  // Who let the rest of the turn's requests through, and when; kept once
  // the turn is over (docs/design.md 4.6).
  trusted_by?: string
  trusted_at?: string
  started_at: string
  ended_at?: string
  // A running turn that showed no sign of life for a while, waiting on no
  // person: since when (docs/design.md 5.23.8). The hub's to say.
  quiet_since?: string
}

// setup: the project's leader setting it up for worktrees (docs/design.md 5.21).
export type TurnKind = 'chat' | 'upkeep' | 'setup'

// A topic with a turn in flight (GET /topics?status=running).
export interface RunningTopic {
  thread_id: string
  room_id: string
  root_message_id: string
  root_body: string
  // What the topic was asked, in a line, without the @ that asked it.
  ask: string
  members: string[]
  started_at: string
}

export interface TopicsResponse {
  topics: RunningTopic[]
}

export interface TurnSummary {
  id: string
  // Whose turn it is.
  member_id?: string
  status: TurnStatus
  error?: string
  started_at: string
  ended_at?: string
}

// What the room timeline shows under a topic root without opening it.
export interface ThreadSummary {
  id: string
  // What the topic is called in its chat, written #12. Absent on the
  // summary that announces a topic the moment it opens.
  number?: number
  reply_count: number
  last_reply_at?: string
  turns: number
  last_turn?: TurnSummary
  // Under the topic a piece of work began in: that piece of work, across
  // all its topics (docs/design.md 5.22).
  work?: WorkSummary
}

// A piece of work: from what a person said, every turn it took, in
// whichever topic.
export interface WorkSummary {
  // The topic it began in, and what that topic is called.
  thread_id: string
  thread_number?: number
  // The message that began it.
  chain: string
  turns: number
  started_at: string
  // Once none of its turns runs.
  ended_at?: string
  running: boolean
  // The members who took turns in it, in the order they first did; the
  // room's summaries leave them out.
  members?: string[]
}

// A top-level message as the room lists it: the message plus its topic.
export interface RoomMessage extends Message {
  thread?: ThreadSummary
}

export interface RoomMessagesResponse {
  messages: RoomMessage[]
}

export interface Thread {
  id: string
  room_id: string
  // What the topic is called in its chat, written #12: people say it and
  // agents read topics by it. Optional only for test fixtures.
  number?: number
  root_message_id: string
  created_at: string
}

export interface ThreadResponse {
  thread: Thread
  root: Message
  turns: Turn[]
  // The piece of work the topic's latest turn is part of, which may have
  // begun in another topic.
  work?: WorkSummary
}

export interface TranscriptApproval {
  id: string
  request_id: string
  status: string
  message?: string
  decided_by?: string
}

// One line of a turn's JSONL transcript (internal/hub/transcript.go).
export interface TranscriptLine {
  // 'restart': the session would not resume and the turn was run again in a
  // new one; error is what the first run failed with. 'reply_asked': the
  // turn said nothing in the chat and was asked for its reply, in the same
  // session (docs/design.md 5.24).
  kind: 'start' | 'restart' | 'reply_asked' | 'event' | 'approval_request' | 'approval_decision' | 'done'
  at?: string
  turn_id?: string
  runtime?: string
  event?: TurnEvent
  approval?: TranscriptApproval
  result?: { output: string; session_ref?: string; usage?: TokenUsage; failure?: string }
  error?: string
  // On start, restart and reply_asked: the run's spec, prompt being the
  // brief the hub composed for it (or what it was asked) and system_prompt
  // what the run was given as its system prompt: the role card and, for a
  // runtime that takes one with every run, the standing instructions.
  spec?: { prompt?: string; system_prompt?: string }
}

export interface TurnResponse {
  turn: Turn
}

interface EventBase {
  room_id: string
  at: string
}

// The room's live stream, one JSON object per WebSocket frame.
export type RoomEvent =
  | (EventBase & { kind: 'message'; message: Message; thread?: ThreadSummary })
  | (EventBase & { kind: 'turn_started' | 'turn_finished'; turn: Turn; work?: WorkSummary })
  // A person let the rest of a running turn's requests through, or took it back.
  | (EventBase & { kind: 'turn_trust'; turn: Turn })
  | (EventBase & { kind: 'turn_event'; turn_id: string; turn_event: TurnEvent })
  | (EventBase & { kind: 'approval_requested' | 'approval_decided'; approval: Approval })
  | (EventBase & { kind: 'wiki_changed'; project_id?: string; scope?: 'project' | 'library' })
  // The person read some of their inbox, here or in another tab.
  | (EventBase & { kind: 'inbox_read'; user_id: string })
  // A pause came into effect, or was lifted (docs/design.md 5.23.3).
  | (EventBase & { kind: 'pause'; pause: Pause; lifted?: boolean; user_id?: string })
  // A member's reminder was set, came due, or was taken back (5.23.4).
  | (EventBase & { kind: 'reminder'; reminder: Reminder })
  // A draft for a person to run was drafted, run, turned down or replaced
  // (5.23.5).
  | (EventBase & { kind: 'draft'; draft: Draft })
  // A running turn went quiet, since quiet_since, or stirred again, without
  // it (5.23.8).
  | (EventBase & { kind: 'turn_quiet'; turn_id: string; quiet_since?: string })

export type ApprovalStatus = 'pending' | 'allowed' | 'denied' | 'expired' | 'cancelled'

// A permission request an agent raised during a turn.
export interface Approval {
  id: string
  turn_id: string
  room_id: string
  thread_id: string
  member_id: string
  request_id: string
  kind: string
  tool: string
  // The tool's full input, as the runtime sent it (JSON).
  input: unknown
  status: ApprovalStatus
  message?: string
  // The thread post that presents the request; the card replaces it.
  message_id?: string
  decided_by?: string
  // Set when nobody was asked: the runtime's own reviewer, such as
  // 'codex_auto_review', a rule of the member's ('rule'), or the hub for
  // the person who let the rest of the turn through ('turn', decided_by
  // being that person) (docs/design.md 4.6).
  reviewer?: string
  // What came with the decision: a question's answers, a form's content,
  // or a reviewer's findings such as {risk, authorization}.
  answer?: unknown
  // What an allow can take in besides, as the runtime offered, and how far
  // the person's allow went.
  similar?: SimilarOffer
  scope?: AllowScope
  created_at: string
  decided_at?: string
}

// What allowing a request can take in besides: Claude Code's rules
// (Bash(go test *)), a permission mode, folders; or, for Codex, the same
// request again and commands starting with the words it proposes. Rules
// and the prefix can also be kept for the member (docs/design.md 4.6).
export interface SimilarOffer {
  rules?: string[]
  mode?: string
  dirs?: string[]
  same?: boolean
  prefix?: string[]
}

// How far an allow goes: the request alone; the like of it for the rest of
// the turn, or from now on; or whatever else the turn asks for.
export type AllowScope = 'once' | 'similar' | 'always' | 'turn'

export interface ApprovalResponse {
  approval: Approval
}

// GET /approvals?status=pending: a request with the names the inbox shows
// next to it.
export interface PendingApproval extends Approval {
  member_name: string
  project_name: string
}

export interface PendingApprovalsResponse {
  approvals: PendingApproval[]
}

export interface ApprovalsResponse {
  approvals: Approval[]
}

export interface DecideApprovalRequest {
  user_id: string
  allow: boolean
  // With an allow: how far it goes; the request alone when left out.
  scope?: AllowScope
  message: string
  // With an allowed question: what the person answered; with a form, what
  // they filled in.
  answer?: QuestionAnswers | FormAnswer
}

export * from './types.agents'
export * from './types.attachments'
export * from './types.inbox'
export * from './types.projects'
export * from './types.branches'
export * from './types.approvals'
export * from './types.work'
export * from './types.wiki'
export * from './types.turns'
