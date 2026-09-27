import type { Approval, Attachment, AttachmentKind, Message, Project, Room, RoomAttachment, RoomKind, ThreadSummary, Turn, User } from '@/api/types'

export function project(id: string, name: string, repo_path = '', main_room_id = `${id}-main`): Project {
  return { id, name, repo_path, main_room_id, created_at: '2026-09-14T00:00:00Z' }
}

export function room(id: string, project_id: string, name: string, kind: RoomKind = 'main'): Room {
  return { id, project_id, name, kind, created_at: '2026-09-14T00:00:00Z' }
}

export function user(id: string, name: string): User {
  return { id, name, created_at: '2026-09-14T00:00:00Z' }
}

export function message(id: string, seq: number, overrides: Partial<Message> = {}): Message {
  return {
    id,
    seq,
    room_id: 'r1',
    sender_kind: overrides.member_id ? 'agent' : 'user',
    user_id: 'u1',
    body: `message ${seq}`,
    mentions: null,
    created_at: '2026-09-14T02:00:00Z',
    ...overrides,
  }
}

export function turn(id: string, thread_id: string, overrides: Partial<Turn> = {}): Turn {
  return {
    id,
    member_id: 'a1',
    room_id: 'r1',
    thread_id,
    machine_id: 'w1',
    status: 'done',
    usage: { input_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, output_tokens: 0 },
    started_at: '2026-09-14T02:00:00Z',
    ended_at: '2026-09-14T02:00:42Z',
    ...overrides,
  }
}

export function summary(id: string, overrides: Partial<ThreadSummary> = {}): ThreadSummary {
  return { id, reply_count: 0, turns: 0, ...overrides }
}

export function approval(id: string, overrides: Partial<Approval> = {}): Approval {
  return {
    id,
    turn_id: 'x1',
    room_id: 'r1',
    thread_id: 't1',
    member_id: 'a1',
    request_id: 'req-' + id,
    kind: 'tool_use',
    tool: 'Bash',
    input: { command: 'make test' },
    status: 'pending',
    message_id: 'note-' + id,
    created_at: '2026-09-14T02:00:05Z',
    ...overrides,
  }
}

export function attachment(id: string, filename: string, kind: AttachmentKind, overrides: Partial<Attachment> = {}): Attachment {
  return {
    id,
    room_id: 'r1',
    message_id: `m-${id}`,
    filename,
    media_type: 'application/octet-stream',
    kind,
    size: 2048,
    created_at: '2026-09-14T02:00:00Z',
    ...overrides,
  }
}

// An attachment as the room's list has it: sent by the person u1 in the
// room itself, unless overrides say otherwise.
export function roomAttachment(id: string, filename: string, kind: AttachmentKind, overrides: Partial<RoomAttachment> = {}): RoomAttachment {
  return { ...attachment(id, filename, kind), sender_kind: 'user', user_id: 'u1', sender_name: 'Alice', message_seq: 1, ...overrides }
}

// runtimeTraits is the hub's table of how each runtime takes its turns
// (docs/design.md 5.23.9), as GET /runtime-traits answers.
export const runtimeTraits = {
  traits: {
    claude: { system_prompt_each_run: true, steer: true, wiki_when_read_only: true },
    codex: { system_prompt_each_run: false, steer: true, wiki_when_read_only: false },
    pi: { system_prompt_each_run: true, steer: true, wiki_when_read_only: true },
    fake: { system_prompt_each_run: true, steer: true, wiki_when_read_only: true },
  },
}
