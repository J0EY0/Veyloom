package hub

import (
	"context"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/protocol"
)

// concludeMerge sees to a merge a member's turn left under way in its
// worktree (docs/design.md 5.21). A member trusted with git merges the main
// line in itself when handed a conflict: settled but not committed, the
// merge is committed for it; with conflict markers left, its topic is told
// which files. It runs as the turn ends, before the turn is recorded over
// and the member's next turn may start, so nothing else works in the
// worktree meanwhile.
func (m *TurnManager) concludeMerge(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	member, dir, dispatched := at.member, at.dir, at.dispatched
	at.mu.Unlock()
	if m.workspace == nil || !dispatched || member.WorktreeDir == "" || dir != member.WorkDir {
		return
	}
	res, err := m.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceConclude, Checkout: member.RepoPath, Dir: member.WorktreeDir})
	if err == nil && res.Error != "" {
		err = workspaceFailure(res)
	}
	if err != nil {
		m.logger.Warn("see to a merge left under way", "member", member.ID, "err", err)
		return
	}
	var note string
	switch c := res.Conclude; {
	case c == nil:
		return
	case c.Commit != "":
		note = concludedNote(member.DisplayName, c.Commit)
	case len(c.Unresolved) > 0:
		note = unresolvedNote(member.DisplayName, c.Unresolved)
	default:
		return
	}
	m.postSystem(ctx, at.thread, at.turn.ID, note)
}

// concludedNote says the merge a member settled was committed for it.
func concludedNote(name, commit string) string {
	return fmt.Sprintf("%s settled the conflicts but left the merge uncommitted; Veyloom committed it as %s.", name, shortCommit(commit))
}

// unresolvedNote says the files a member's merge still has conflicts in.
func unresolvedNote(name string, files []string) string {
	verb := "has"
	if len(files) > 1 {
		verb = "have"
	}
	return fmt.Sprintf("%s's worktree is still in the middle of a merge: %s still %s conflict markers.", name, strings.Join(files, ", "), verb)
}

// shortCommit is a commit as people read it.
func shortCommit(commit string) string {
	return commit[:min(len(commit), 7)]
}
