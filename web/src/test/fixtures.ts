import type { Approval, Message, Project, Room, RoomKind, ThreadSummary, Turn, User } from '@/api/types'

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
