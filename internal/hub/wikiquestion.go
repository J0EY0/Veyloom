package hub

import (
	"context"

	"github.com/J0EY0/veyloom/internal/store"
)

// A person's doubt about a page of the project's wiki (docs/design.md
// 5.15): it is put in the wiki topic, to the wiki maintainer while upkeep
// is on, or else to the member that worked in the chat last, who checks
// the page against the code, or asks, and sets it right.

// WikiQuestion is where a question about a page goes: the wiki topic, and
// the member to ask there.
type WikiQuestion struct {
	ThreadID   string `json:"thread_id"`
	MemberID   string `json:"member_id"`
	MemberName string `json:"member_name"`
}

// turnsLooked caps the turns looked back over for who worked last.
const turnsLooked = 50

// WikiQuestion opens the project's wiki topic, if it is not open yet, and
// says whom to ask there.
func (h *Hub) WikiQuestion(ctx context.Context, projectID string) (WikiQuestion, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return WikiQuestion{}, err
	}
	member, err := h.whoAnswers(ctx, project)
	if err != nil {
		return WikiQuestion{}, err
	}
	thread, err := h.turns.wikiTopic(ctx, project)
	if err != nil {
		return WikiQuestion{}, err
	}
	return WikiQuestion{ThreadID: thread.ID, MemberID: member.ID, MemberName: member.DisplayName}, nil
}

// whoAnswers is the member a question about the project's wiki goes to:
// its maintainer, while upkeep is on; else the member whose turn in the
// chat was the latest; else the first member there is. Only members that
// are in and on count.
func (h *Hub) whoAnswers(ctx context.Context, project store.Project) (store.Member, error) {
	usable := func(m store.Member) bool { return m.Enabled && !m.Removed() }
	if m, err := h.keeper(ctx, project); err == nil {
		return m, nil
	}
	turns, err := h.store.ListRoomTurns(ctx, project.MainRoomID, turnsLooked)
	if err != nil {
		return store.Member{}, err
	}
	for _, t := range turns {
		if t.Kind != store.TurnChat {
			continue
		}
		if m, err := h.store.GetMember(ctx, t.MemberID); err == nil && usable(m) {
			return m, nil
		}
	}
	members, err := h.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return store.Member{}, err
	}
	for _, m := range members {
		if usable(m) {
			return m, nil
		}
	}
	return store.Member{}, store.Conflicting("noOneToAsk", nil, "the project has no member to ask: add one to its chat first")
}
