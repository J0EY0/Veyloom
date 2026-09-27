package hub

import (
	"context"

	"github.com/J0EY0/veyloom/internal/store"
)

// What a member is asked while it is busy waits in its queue (wake), and
// in the store too: a hub that stops loses what it held in memory, and a
// person should not find what they asked quietly dropped. The store keeps
// a trigger from before it joins the queue until the turn that answers it
// starts. While the member's machine is away, the hub stopping or a
// machine dropping off, what waits is not started, and so not failed
// (advance); when the machine connects, what waited in memory goes on,
// and what the store kept of a hub that stopped comes back in the order
// it was asked. An upkeep or a setup is not kept: each is asked for again
// when it is due.

// keepQueued keeps t in the store while memberID is busy. A failure is
// logged: the trigger then waits in memory only.
func (m *TurnManager) keepQueued(ctx context.Context, memberID string, t trigger) {
	w := store.QueuedWake{MemberID: memberID, MessageID: t.msg.ID, ThreadID: t.thread.ID, AnchorID: t.anchor}
	if err := m.store.QueueWake(ctx, w); err != nil {
		m.logger.Error("keep what a busy member was asked", "member", memberID, "message", t.msg.ID, "err", err)
	}
}

// unqueue drops the chat triggers among taken from the store: their turn
// starts, or will not now.
func (m *TurnManager) unqueue(ctx context.Context, memberID string, taken []trigger) {
	var ids []string
	for _, t := range taken {
		if t.upkeep == nil && t.setup == nil {
			ids = append(ids, t.msg.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := m.store.UnqueueWakes(ctx, memberID, ids); err != nil {
		m.logger.Error("drop what a member was asked from the store", "member", memberID, "err", err)
	}
}

// resumeQueued takes up, as machineID connects, what its members were
// asked and had not begun: first what waited in memory while it was away,
// then what the store kept of a hub that stopped. A trigger waiting or
// taken up already is not asked again (wake).
func (m *TurnManager) resumeQueued(ctx context.Context, machineID string) {
	// What memory holds for the machine's members is taken up from memory,
	// and left out of what the store kept, which is read after: its turn
	// may get going meanwhile, before it leaves the store.
	held := map[string]bool{}
	m.mu.Lock()
	for _, st := range m.members {
		if st.machine != machineID {
			continue
		}
		for _, p := range st.pending {
			held[p.msg.ID] = true
		}
		for _, msg := range st.next {
			held[msg.ID] = true
		}
		if st.running != nil {
			for _, msg := range st.running.triggers {
				held[msg.ID] = true
			}
		}
	}
	m.mu.Unlock()
	m.resume(func(_ string, st *memberState) bool { return st.machine == machineID })

	queued, err := m.store.ListQueuedWakes(ctx, machineID)
	if err != nil {
		m.logger.Error("what members waited to be asked", "machine", machineID, "err", err)
		return
	}
	for _, q := range queued {
		if held[q.MessageID] {
			continue
		}
		t, member, err := m.queuedTrigger(ctx, q)
		if err != nil {
			m.logger.Warn("what a member waited to be asked", "member", q.MemberID, "message", q.MessageID, "err", err)
			m.unqueue(ctx, q.MemberID, []trigger{{msg: store.Message{ID: q.MessageID}}})
			continue
		}
		m.logger.Info("wake a member for what it was asked before the hub stopped", "member", member.DisplayName, "message", q.MessageID)
		m.wake(ctx, member, t, true)
	}
}

// queuedTrigger reads back what a queued wake stands for.
func (m *TurnManager) queuedTrigger(ctx context.Context, q store.QueuedWake) (trigger, store.Member, error) {
	member, err := m.store.GetMember(ctx, q.MemberID)
	if err != nil {
		return trigger{}, store.Member{}, err
	}
	msg, err := m.store.GetMessage(ctx, q.MessageID)
	if err != nil {
		return trigger{}, store.Member{}, err
	}
	t := trigger{msg: msg, anchor: q.AnchorID}
	if q.ThreadID != "" {
		if t.thread, err = m.store.GetThread(ctx, q.ThreadID); err != nil {
			return trigger{}, store.Member{}, err
		}
	}
	return t, member, nil
}
