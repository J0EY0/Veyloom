// Wire types for /api/v1. Field names match the JSON tags on the Go
// structs in internal/store and internal/api; change both together.

import type { FormAnswer, QuestionAnswers } from './types.approvals'

export interface Project {
  id: string
  name: string
  // Where the code is checked out on the machines its members run on; the
  // members a project starts with work there.
  repo_path: string
  // What the project is, in a paragraph; every agent's brief opens with it.
  // Always sent by the API; optional here until the settings UI uses it.
  description?: string
  // The project's group chat: a project is one chat (docs/webui.md §3).
  main_room_id: string
  created_at: string
}

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

export interface CreateProjectRequest {
  name: string
  repo_path?: string
  // What the project is, in a paragraph; every agent's brief opens with it.
  description?: string
  // The agents that join the chat as its first members.
  agent_ids: string[]
}

// An absent field keeps its value. Moving the checkout moves the project's
// current members with it: they work under it.
export interface UpdateProjectRequest {
  name?: string
  repo_path?: string
  description?: string
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

// A file attached to a message. The bytes are at attachmentUrl(id).
export interface Attachment {
  id: string
  room_id: string
  message_id?: string
  filename: string
  media_type: string
  size: number
  created_at: string
}

export interface AttachmentResponse {
  attachment: Attachment
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
  started_at: string
  ended_at?: string
}

// A topic with a turn in flight (GET /topics?status=running).
export interface RunningTopic {
  thread_id: string
  room_id: string
  root_message_id: string
  root_body: string
  members: string[]
  started_at: string
}

export interface TopicsResponse {
  topics: RunningTopic[]
}

export interface TurnSummary {
  id: string
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
}

// 'session' is only in transcripts: the hub keeps it to itself instead of
// sending it to the room.
export type TurnEventKind =
  'status' | 'text' | 'tool_call' | 'tool_result' | 'file_changed' | 'error' | 'approval_request' | 'notice' | 'session' | 'compaction'

// One runtime event of a running turn; the fields present depend on kind.
export interface TurnEvent {
  kind: TurnEventKind
  at?: string
  text?: string
  tool?: string
  input?: string
  path?: string
  session_ref?: string
  // On 'compaction': 'start', 'end' or 'failed'.
  phase?: string
  approval_id?: string
  // On 'approval_request': what is asked beyond permission ('question',
  // 'form', 'link'), and for a request the runtime settled itself, who
  // decided, the verdict and the reviewer's findings; text says why.
  approval_kind?: string
  reviewer?: string
  verdict?: string
  detail?: unknown
  // On 'notice': 'info', 'warning' or 'error'.
  level?: string
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
  // new one; error is what the first run failed with.
  kind: 'start' | 'restart' | 'event' | 'approval_request' | 'approval_decision' | 'done'
  at?: string
  turn_id?: string
  runtime?: string
  event?: TurnEvent
  approval?: TranscriptApproval
  result?: { output: string; session_ref?: string; usage?: TokenUsage; failure?: string }
  error?: string
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
  | (EventBase & { kind: 'turn_started' | 'turn_finished'; turn: Turn })
  | (EventBase & { kind: 'turn_event'; turn_id: string; turn_event: TurnEvent })
  | (EventBase & { kind: 'approval_requested' | 'approval_decided'; approval: Approval })

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
  // Set when the runtime's own reviewer decided rather than a person, such
  // as 'codex_auto_review'; nobody was asked (docs/design.md 4.6).
  reviewer?: string
  // What came with the decision: a question's answers, a form's content,
  // or a reviewer's findings such as {risk, authorization}.
  answer?: unknown
  created_at: string
  decided_at?: string
}

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
  message: string
  // With an allowed question: what the person answered; with a form, what
  // they filled in.
  answer?: QuestionAnswers | FormAnswer
}

// A message that mentions the current user, as the inbox lists it.
export interface InboxItem extends Message {
  room_name: string
  project_name: string
  sender_name: string
}

export interface InboxResponse {
  items: InboxItem[]
}

export * from './types.agents'
export * from './types.approvals'
