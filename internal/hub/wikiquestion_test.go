package hub

import (
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestLoop_AQuestionAboutAPage(t *testing.T) {
	l, _ := wikiLoop(t)
	var p *store.Problem
	if _, err := l.h.WikiQuestion(l.ctx, l.project().ID); !errors.Is(err, store.ErrConflict) || !errors.As(err, &p) || p.Code != "noOneToAsk" {
		t.Errorf("nobody to ask: %v", err)
	}

	// With nobody's turn yet, the first member there is.
	coder := l.member("Coder", nil)
	writer := l.member("Writer", nil)
	first, err := l.h.WikiQuestion(l.ctx, l.project().ID)
	if err != nil || first.MemberID != coder.ID || first.MemberName != "Coder" || first.ThreadID == "" {
		t.Fatalf("first %+v %v", first, err)
	}
	topic, _ := l.s.GetThread(l.ctx, first.ThreadID)
	if root := l.root(topic); root.Body != wikiTopicIntro || l.project().WikiThreadID != first.ThreadID {
		t.Errorf("the wiki topic opens: %q, the project names %q", root.Body, l.project().WikiThreadID)
	}

	// Then whoever worked last.
	l.say("@Writer hello", "", writer)
	l.waitTurns(1, store.TurnDone, "Writer's turn")
	if q, _ := l.h.WikiQuestion(l.ctx, l.project().ID); q.MemberID != writer.ID || q.ThreadID != first.ThreadID {
		t.Errorf("the one who worked last, in the same topic: %+v", q)
	}

	// The maintainer before anyone, while it is on.
	keeper := l.member("Keeper", nil)
	l.keep(keeper, store.UpkeepManual)
	if q, _ := l.h.WikiQuestion(l.ctx, l.project().ID); q.MemberID != keeper.ID {
		t.Errorf("the maintainer: %+v", q)
	}
	off := false
	if _, err := l.s.UpdateMember(l.ctx, keeper.ID, store.MemberPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if q, _ := l.h.WikiQuestion(l.ctx, l.project().ID); q.MemberID != writer.ID {
		t.Errorf("a maintainer switched off is passed over: %+v", q)
	}
}
