// Wire types of a turn's runtime events: the room's stream and the
// transcript carry the same (internal/runtime/turn.go).

import type { Quota } from './types.agents'

// 'session' is only in transcripts: the hub keeps it to itself instead of
// sending it to the room.
// 'steer' is what people said in the turn's topic that it was passed as it
// ran, once the agent took it in; 'steer_dropped' is such a passing that
// did not reach the agent, which waits for the next turn instead.
export type TurnEventKind =
  | 'status'
  | 'text'
  | 'tool_call'
  | 'tool_result'
  | 'file_changed'
  | 'error'
  | 'approval_request'
  | 'notice'
  | 'session'
  | 'compaction'
  | 'steer'
  | 'steer_dropped'
  | 'quota'

// One runtime event of a running turn; the fields present depend on kind.
export interface TurnEvent {
  kind: TurnEventKind
  at?: string
  text?: string
  tool?: string
  input?: string
  path?: string
  // On 'tool_call' and 'tool_result', when the runtime names its calls:
  // a result carries the id of the call it answers.
  call_id?: string
  // The hub's number for the event in its turn, from 1: the transcript
  // and the room's stream carry the same, to tell an event read in one
  // from one heard on the other.
  seq?: number
  session_ref?: string
  // On 'compaction': 'start', 'end' or 'failed'.
  phase?: string
  // On 'steer' and 'steer_dropped': the hub's id for the passing; text on
  // 'steer' is what the turn was passed, as the agent got it.
  steer_id?: string
  // On 'quota': how the account stands against its usage limits.
  quota?: Quota
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
