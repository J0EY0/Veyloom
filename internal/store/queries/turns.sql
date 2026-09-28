-- name: CreateTurn :one
INSERT INTO turns (member_id, room_id, thread_id, trigger_message_id, machine_id, runtime, transcript_path, session_id, kind, chain_message_id, woken_by_turn_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: FinishTurn :one
UPDATE turns SET
    status             = $2,
    error              = $3,
    reply_message_id   = $4,
    transcript_path    = $5,
    input_tokens       = $6,
    cache_read_tokens  = $7,
    cache_write_tokens = $8,
    output_tokens      = $9,
    files_changed      = $10,
    skills_used        = $11,
    worked             = $12,
    wiki_pages         = sqlc.arg('wiki_pages'),
    ended_at           = now()
WHERE id = $1
RETURNING *;

-- name: GetTurn :one
SELECT * FROM turns WHERE id = $1;

-- name: RunningMembersInThread :many
-- The members with a turn running in a topic, getting ready included: a
-- person's message there that mentions nobody goes to the one running
-- (design.md 4.2).
SELECT DISTINCT member_id FROM turns WHERE thread_id = $1 AND status = 'running';

-- name: TurnAtSessionEnd :one
-- The member's latest turn by the time one of its sessions ended: the one
-- going as the session ended, which may not have got a session yet.
SELECT t.* FROM turns t JOIN member_sessions s ON s.member_id = t.member_id
WHERE s.id = sqlc.arg(session_id) AND t.member_id = sqlc.arg(member_id) AND t.started_at <= s.ended_at
ORDER BY t.started_at DESC LIMIT 1;

-- name: SetTurnSession :execrows
-- Moves a running turn to another session: the one it started in would not
-- resume, and the turn was run again in a new one.
UPDATE turns SET session_id = $2 WHERE id = $1;

-- name: ListRoomTurns :many
-- Most recent turns of a room first.
SELECT * FROM turns WHERE room_id = $1 ORDER BY started_at DESC LIMIT $2;

-- name: ListThreadTurns :many
-- Every turn of a topic, oldest first, so a thread can be read turn by turn.
SELECT * FROM turns
WHERE thread_id = $1
ORDER BY started_at;

-- name: ListRoomTurnsByStatus :many
-- Most recent turns of a room in one status first; the UI opens a room
-- with only the running ones.
SELECT * FROM turns WHERE room_id = $1 AND status = $2 ORDER BY started_at DESC LIMIT $3;

-- name: ListRunningTopics :many
-- The topics with a turn in flight, across every room: what the sidebar
-- lists under each project while its members work.
SELECT t.id AS thread_id, t.room_id, t.root_message_id, m.body AS root_body,
       array_agg(DISTINCT mb.display_name)::text[] AS members,
       min(tu.started_at)::timestamptz AS started_at,
       -- What the topic was asked: the words that set its first turn off,
       -- and the names of the room's members, whose @s lead them.
       coalesce((SELECT a.body FROM turns ft JOIN messages a ON a.id = ft.trigger_message_id
                 WHERE ft.thread_id = t.id ORDER BY ft.started_at LIMIT 1), '')::text AS ask,
       coalesce((SELECT array_agg(rm.display_name) FROM members rm WHERE rm.room_id = t.room_id), '{}')::text[] AS names
FROM turns tu
JOIN threads t ON t.id = tu.thread_id
JOIN messages m ON m.id = t.root_message_id
JOIN members mb ON mb.id = tu.member_id
WHERE tu.status = 'running'
GROUP BY t.id, t.room_id, t.root_message_id, m.body
ORDER BY started_at;

-- name: FailRunningTurns :many
-- Turns still marked running when the hub starts were cut off by its last
-- stop; nothing will ever finish them.
UPDATE turns SET status = 'failed', error = $1, ended_at = now()
WHERE status = 'running'
RETURNING *;

-- name: ListMachineTurnsSince :many
-- How a machine's turns since a moment went: when each started, how it
-- ended and when, and the tokens it spent; only one runtime's unless runtime
-- is empty. The machines page sums them into hours or days.
SELECT status, started_at, ended_at, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens
FROM turns
WHERE machine_id = sqlc.arg(machine_id)
  AND started_at >= sqlc.arg(since)
  AND (sqlc.arg(runtime)::text = '' OR runtime = sqlc.arg(runtime)::text)
ORDER BY started_at;

-- name: ListSkillUses :many
-- The turns that used a skill, newest first, with where they ran: what the
-- team that owns it looks back on.
SELECT t.id, t.status, t.runtime, t.started_at, t.ended_at, t.room_id, t.thread_id,
       th.number AS topic_number, m.display_name AS member_name, p.id AS project_id, p.name AS project_name
FROM turns t
JOIN threads th ON th.id = t.thread_id
JOIN members m ON m.id = t.member_id
JOIN rooms r ON r.id = t.room_id
JOIN projects p ON p.id = r.project_id
WHERE sqlc.arg(skill)::text = ANY (t.skills_used)
ORDER BY t.started_at DESC, t.id
LIMIT sqlc.arg(lim);

-- name: CountChainWakes :one
-- The turns agents woke in a piece of work, under way or over.
SELECT count(*) FROM turns WHERE chain_message_id = $1 AND woken_by_turn_id IS NOT NULL;

-- name: RecentChainWakes :many
-- Whether the last turns agents woke in a piece of work to end did work,
-- the latest first.
SELECT worked FROM turns
WHERE chain_message_id = $1 AND woken_by_turn_id IS NOT NULL AND ended_at IS NOT NULL
ORDER BY ended_at DESC
LIMIT $2;

-- name: SetTurnTrust :one
-- A person lets the rest of a running turn's requests through.
UPDATE turns SET trusted_by = $2, trusted_at = now()
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: ClearTurnTrust :one
-- A person takes it back. A turn that ended trusted keeps who trusted it,
-- as a record: nothing is let through once it is over.
UPDATE turns SET trusted_by = NULL, trusted_at = NULL
WHERE id = $1
RETURNING *;

-- name: QueueWake :exec
-- Keeps what a busy member was asked until its turn starts; asked twice
-- by one message, it waits once.
INSERT INTO queued_wakes (member_id, message_id, thread_id, anchor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: UnqueueWakes :exec
-- A turn started for these messages: they wait no longer.
DELETE FROM queued_wakes
WHERE member_id = sqlc.arg(member_id) AND message_id = ANY(sqlc.arg(message_ids)::uuid[]);

-- name: ListQueuedWakes :many
-- What the members running on a machine were asked and wait for, in the
-- order they were asked.
SELECT q.* FROM queued_wakes q
JOIN members m ON m.id = q.member_id
WHERE m.machine_id = $1
ORDER BY q.queued_at, q.message_id;

-- name: PauseAccount :one
-- Pauses a runtime's account on a machine, or updates why and until when:
-- it keeps the time it began.
INSERT INTO pauses (machine_id, runtime, reason, detail, ends_at)
VALUES (sqlc.arg(machine_id), sqlc.arg(runtime), sqlc.arg(reason), sqlc.arg(detail), sqlc.narg(ends_at))
ON CONFLICT (machine_id, runtime) WHERE member_id IS NULL DO UPDATE SET
    reason = EXCLUDED.reason, detail = EXCLUDED.detail, ends_at = EXCLUDED.ends_at
RETURNING *;

-- name: PauseMember :one
-- Pauses a member, or updates why and until when.
INSERT INTO pauses (member_id, reason, detail, ends_at)
VALUES (sqlc.arg(member_id), sqlc.arg(reason), sqlc.arg(detail), sqlc.narg(ends_at))
ON CONFLICT (member_id) WHERE member_id IS NOT NULL DO UPDATE SET
    reason = EXCLUDED.reason, detail = EXCLUDED.detail, ends_at = EXCLUDED.ends_at
RETURNING *;

-- name: ListPauses :many
-- The pauses in effect, the oldest first.
SELECT * FROM pauses ORDER BY created_at, id;

-- name: LiftAccountPause :execrows
DELETE FROM pauses WHERE member_id IS NULL AND machine_id = sqlc.arg(machine_id) AND runtime = sqlc.arg(runtime);

-- name: LiftMemberPause :execrows
DELETE FROM pauses WHERE member_id = sqlc.arg(member_id);

-- name: CreateReminder :one
INSERT INTO reminders (member_id, room_id, thread_id, turn_id, note, due_at)
VALUES (sqlc.arg(member_id), sqlc.arg(room_id), sqlc.arg(thread_id), sqlc.narg(turn_id), sqlc.arg(note), sqlc.arg(due_at))
RETURNING *;

-- name: SetReminderMessage :exec
-- The note that told of a reminder as it was set.
UPDATE reminders SET set_message_id = sqlc.arg(message_id) WHERE id = sqlc.arg(id);

-- name: GetReminder :one
SELECT * FROM reminders WHERE id = $1;

-- name: CountPendingReminders :one
SELECT count(*) FROM reminders WHERE member_id = $1 AND status = 'pending';

-- name: ListPendingReminders :many
-- The reminders not yet due of the members a machine runs, the soonest
-- first.
SELECT r.* FROM reminders r
JOIN members m ON m.id = r.member_id
WHERE m.machine_id = $1 AND r.status = 'pending'
ORDER BY r.due_at, r.id;

-- name: ListMemberPendingReminders :many
SELECT * FROM reminders WHERE member_id = $1 AND status = 'pending' ORDER BY due_at, id;

-- name: ListThreadReminders :many
SELECT * FROM reminders WHERE thread_id = $1 ORDER BY created_at, id;

-- name: FireReminder :one
-- A reminder comes due, once: one fired or cancelled already is left as it
-- is, and nothing comes back.
UPDATE reminders SET status = 'fired', settled_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: SetReminderFired :exec
-- The message a reminder came due as.
UPDATE reminders SET fired_message_id = sqlc.arg(message_id) WHERE id = sqlc.arg(id);

-- name: CancelReminder :one
-- Takes back a reminder not yet due, by a person when cancelled_by is set.
UPDATE reminders SET status = 'cancelled', cancelled_by = sqlc.narg(cancelled_by), settled_at = now()
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: DropReminder :one
-- A reminder whose member is gone or switched off as it comes due.
UPDATE reminders SET status = 'dropped', settled_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CreateDraft :one
INSERT INTO drafts (project_id, room_id, thread_id, member_id, turn_id, kind, target_id, subject, params, then_note)
VALUES (sqlc.arg(project_id), sqlc.arg(room_id), sqlc.arg(thread_id), sqlc.arg(member_id), sqlc.narg(turn_id), sqlc.arg(kind),
        sqlc.narg(target_id), sqlc.arg(subject), sqlc.arg(params), sqlc.arg(then_note))
RETURNING *;

-- name: LockDraftSubject :exec
-- Holds a project's drafts about a subject for the transaction: those
-- drafted at once give way one to another in turn.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(project_id)::text || ' ' || sqlc.arg(subject)::text, 0));

-- name: SupersedeDrafts :many
-- The project's pending drafts about subject give way to a new one.
UPDATE drafts SET status = 'superseded', settled_at = now()
WHERE project_id = sqlc.arg(project_id) AND subject = sqlc.arg(subject) AND status = 'pending'
RETURNING *;

-- name: OpenDraft :one
-- The project's draft about subject that is not settled yet.
SELECT * FROM drafts
WHERE project_id = sqlc.arg(project_id) AND subject = sqlc.arg(subject) AND status IN ('pending', 'running')
ORDER BY created_at DESC
LIMIT 1;

-- name: SetDraftMessage :exec
-- The card that shows a draft.
UPDATE drafts SET message_id = sqlc.arg(message_id) WHERE id = sqlc.arg(id);

-- name: GetDraft :one
SELECT * FROM drafts WHERE id = $1;

-- name: GetDraftByMessage :one
-- The draft a message is the card or the outcome of.
SELECT * FROM drafts WHERE message_id = $1 OR result_message_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: ListThreadDrafts :many
SELECT * FROM drafts WHERE thread_id = $1 ORDER BY created_at, id;

-- name: CountTurnDrafts :one
SELECT count(*) FROM drafts WHERE turn_id = $1;

-- name: ClaimDraft :one
-- A person runs a pending draft: one run at a time, and none once it is
-- settled.
UPDATE drafts SET status = 'running' WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: ReleaseDraft :one
-- A run that could not be done for now: the draft waits again.
UPDATE drafts SET status = 'pending' WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: ReleaseRunningDrafts :many
-- Runs a stopped hub left under way wait again; what they did shows when
-- run again.
UPDATE drafts SET status = 'pending' WHERE status = 'running'
RETURNING *;

-- name: SettleDraft :one
-- What came of a run: done, or conflicted.
UPDATE drafts SET status = sqlc.arg(status), result = sqlc.arg(result), decided_by = sqlc.narg(decided_by), settled_at = now()
WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING *;

-- name: DeclineDraft :one
-- A person turns a pending draft down.
UPDATE drafts SET status = 'declined', decided_by = sqlc.narg(decided_by), settled_at = now()
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: SetDraftResultMessage :exec
-- The message that told the member what came of its draft.
UPDATE drafts SET result_message_id = sqlc.arg(message_id) WHERE id = sqlc.arg(id);
