package hub

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A merge a member left under way in its worktree (docs/design.md 5.21):
// nothing goes onto the main line or comes in meanwhile; settled but not
// committed, it is committed for the member as its turn ends; with
// conflict markers left, its topic is told which files, and a person can
// give it up.
func TestLoop_AMergeLeftUnderwayIsSeenTo(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"README.md"}})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"notes.md"}})
	git := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		return cmd.Run()
	}
	worktreeOf := func(m store.Member) string {
		t.Helper()
		got, err := l.s.GetMember(l.ctx, m.ID)
		if err != nil || got.WorkDir == "" {
			t.Fatalf("%s has no worktree: %+v %v", m.DisplayName, got, err)
		}
		return got.WorkDir
	}
	// clash has the member's work and the main line change the same line
	// of README.md, and the member merge the main line in, stopping on it.
	clash := func(dir, line string) {
		t.Helper()
		for _, side := range []struct{ dir, body string }{{dir, line}, {repo, "main: " + line}} {
			if err := os.WriteFile(filepath.Join(side.dir, "README.md"), []byte("# app\n\n"+side.body+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := git(side.dir, "commit", "-qam", side.body); err != nil {
				t.Fatalf("commit %q: %v", side.body, err)
			}
		}
		if git(dir, "merge", "-q", "main") == nil {
			t.Fatal("the merge should stop on a conflict")
		}
	}
	lastNote := func(turns []store.Turn, m store.Member) string {
		t.Helper()
		for _, turn := range turns {
			if turn.MemberID == m.ID {
				notes := l.notes(turn.ThreadID)
				if len(notes) == 0 {
					return ""
				}
				return notes[len(notes)-1]
			}
		}
		return ""
	}

	l.say("@Coder start", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	l.say("@Tester start", "", tester)
	l.waitTurns(3, store.TurnDone, "Tester's turn")
	coderDir, testerDir := worktreeOf(coder), worktreeOf(tester)

	// Coder stops on a conflict: its work is held back.
	if err := git(coderDir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	clash(coderDir, "Coder's line")
	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if st := b.Members[0].Status; st == nil || !st.Merging || !slices.Equal(st.Conflicts, []string{"README.md"}) {
		t.Errorf("Coder's worktree mid-merge: %+v", st)
	}
	if _, err := l.h.Merge(l.ctx, coder.ID, "Coder's work", nil); !errors.Is(err, store.ErrConflict) || !strings.Contains(err.Error(), "middle of a merge") {
		t.Errorf("a merge while one is under way: %v", err)
	}
	if _, err := l.h.SyncMember(l.ctx, coder.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a sync while a merge is under way: %v", err)
	}

	// Its next turn writes README.md afresh, markers gone, and commits
	// nothing: Veyloom commits the merge, and says so.
	l.say("@Coder settle it", "", coder)
	turns := l.waitTurns(4, store.TurnDone, "Coder's settling turn")
	if note := lastNote(turns, coder); !strings.HasPrefix(note, "Coder settled the conflicts but left the merge uncommitted; Veyloom committed it as ") {
		t.Errorf("the note in Coder's topic: %q", note)
	}
	if git(coderDir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == nil {
		t.Error("the merge is still under way")
	}
	if merged, err := l.h.Merge(l.ctx, coder.ID, "Coder's work", nil); err != nil || merged.Commit == "" {
		t.Errorf("the settled work onto the main line: %+v %v", merged, err)
	}

	// Tester stops on a conflict, and its next turn leaves the markers:
	// its topic names the file, and a person gives the merge up.
	clash(testerDir, "Tester's line")
	l.say("@Tester carry on", "", tester)
	turns = l.waitTurns(5, store.TurnDone, "Tester's turn")
	if note := lastNote(turns, tester); note != "Tester's worktree is still in the middle of a merge: README.md still has conflict markers." {
		t.Errorf("the note in Tester's topic: %q", note)
	}
	if err := l.h.AbortMerge(l.ctx, tester.ID); err != nil {
		t.Fatal(err)
	}
	if git(testerDir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == nil {
		t.Error("the merge was not given up")
	}
	l.h.turns.overlapChecks.Wait()
}
