package hub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// Two members change the same file, each in its own worktree: the chat is
// told once, as the second one's turn ends. Merging the work of one makes
// the overlap go away; it is told again when it comes back (docs/design.md
// 5.21).
func TestLoop_OverlapsAreToldOnce(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"README.md", "feature.go"}})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"README.md", "feature_test.go"}})
	notes := func() []string {
		t.Helper()
		l.h.turns.overlapChecks.Wait()
		var out []string
		for _, msg := range l.topLevel() {
			if msg.SenderKind == store.SenderSystem && strings.HasSuffix(msg.Body, "whichever is merged second may conflict.") {
				out = append(out, msg.Body)
			}
		}
		return out
	}
	noted := func(member store.Member) []string {
		t.Helper()
		m, err := l.s.GetMember(l.ctx, member.ID)
		if err != nil {
			t.Fatal(err)
		}
		return m.OverlapsNoted
	}

	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	if got := notes(); len(got) != 0 {
		t.Fatalf("told with one member at work: %q", got)
	}
	l.say("@Tester write the tests", "", tester)
	l.waitTurns(3, store.TurnDone, "Tester's turn")
	if got := notes(); !slices.Equal(got, []string{"Tester and Coder both changed README.md: whichever is merged second may conflict."}) {
		t.Fatalf("the overlap told: %q", got)
	}
	if !slices.Equal(noted(coder), []string{"README.md"}) || !slices.Equal(noted(tester), []string{"README.md"}) {
		t.Errorf("recorded as told: %v %v", noted(coder), noted(tester))
	}

	// Told once: neither member's next turn says it again. The members
	// read it in their briefs.
	l.say("@Tester more tests", "", tester)
	if next := l.waitTurns(4, store.TurnDone, "Tester's second turn")[0]; !strings.Contains(specOf(t, next).Prompt, "Tester and Coder both changed README.md") {
		t.Errorf("Tester's brief lacks the overlap:\n%s", specOf(t, next).Prompt)
	}
	l.say("@Coder more of it", "", coder)
	l.waitTurns(5, store.TurnDone, "Coder's second turn")
	if got := notes(); len(got) != 1 {
		t.Fatalf("told again: %q", got)
	}

	// Coder's work goes onto the main line, and with it the overlap; Coder
	// changing README.md again overlaps Tester's work anew.
	if _, err := l.h.Merge(l.ctx, coder.ID, "Add the feature", nil); err != nil {
		t.Fatal(err)
	}
	if got := noted(coder); len(got) != 0 {
		t.Errorf("still told after the merge: %v", got)
	}
	l.say("@Coder and the docs", "", coder)
	l.waitTurns(6, store.TurnDone, "Coder's turn after the merge")
	if got := notes(); len(got) != 2 || got[1] != "Coder and Tester both changed README.md: whichever is merged second may conflict." {
		t.Errorf("the overlap come back: %q", got)
	}
}

func TestOverlapNote(t *testing.T) {
	for _, c := range []struct {
		names, files []string
		want         string
	}{
		{[]string{"Coder", "Tester"}, []string{"a.go"}, "Coder and Tester both changed a.go: whichever is merged second may conflict."},
		{[]string{"Coder", "Tester", "Writer"}, []string{"a.go", "b.go"}, "Coder, Tester and Writer all changed a.go, b.go: whichever is merged second may conflict."},
		{[]string{"A", "B"}, []string{"1", "2", "3", "4", "5", "6", "7"}, "A and B both changed 1, 2, 3, 4, 5 and 2 more: whichever is merged second may conflict."},
	} {
		if got := overlapNote(c.names, c.files); got != c.want {
			t.Errorf("overlapNote(%v, %v) = %q", c.names, c.files, got)
		}
	}
}

// A member that merged another's work, both going on after, does not
// overlap it for the files that work changed: only what each changed since
// they last shared history counts (docs/design.md 5.21).
func TestLoop_WorkBothWentOnFromDoesNotOverlap(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature.go"}})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature_test.go"}})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	c, _ := l.s.GetMember(l.ctx, coder.ID)
	gitIn(t, c.WorkDir, "add", "-A")
	gitIn(t, c.WorkDir, "commit", "-q", "-m", "Add the feature")
	l.say("@Tester test it", "", tester)
	l.waitTurns(3, store.TurnDone, "Tester's turn")
	tt, _ := l.s.GetMember(l.ctx, tester.ID)
	gitIn(t, tt.WorkDir, "merge", "-q", "veyloom/coder")
	gitIn(t, tt.WorkDir, "add", "-A")
	gitIn(t, tt.WorkDir, "commit", "-q", "-m", "Test the feature")

	// Both built the program to try it, and left the build behind: no
	// overlap either, since a merge leaves such files out.
	for _, dir := range []string{c.WorkDir, tt.WorkDir} {
		if err := os.WriteFile(filepath.Join(dir, "app"), []byte("\x00\x01built in "+dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Coder fixes its feature: only Coder changed it since Tester took it.
	l.say("@Coder fix it", "", coder)
	l.waitTurns(4, store.TurnDone, "Coder's second turn")
	l.h.turns.overlapChecks.Wait()
	for _, msg := range l.topLevel() {
		if msg.SenderKind == store.SenderSystem && strings.HasSuffix(msg.Body, "whichever is merged second may conflict.") {
			t.Errorf("told of an overlap: %s", msg.Body)
		}
	}
	if b, err := l.h.Branches(l.ctx, l.room.ProjectID); err != nil || len(b.Overlaps) != 0 {
		t.Errorf("the branch tab's overlaps: %+v %v", b.Overlaps, err)
	}
}
