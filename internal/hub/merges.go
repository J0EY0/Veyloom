package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// Putting a member's work on the main line (docs/design.md 5.21): squashed
// into one commit with the message a person wrote, and said in the chat,
// which the members read in their briefs. What the member had merged of
// the others' work goes on with it; a member whose work was all in it, and
// any other with no work of its own, follows the main line at once rather
// than at its next turn.

// Merge puts everything a member's worktree changed on the main line as
// one commit with the message a person wrote, but for the new files a
// person left out, which stay in the worktree. Conflicts come back in the
// result, nothing changed.
func (h *Hub) Merge(ctx context.Context, memberID, message string, leave []string) (worktree.MergeResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return worktree.MergeResult{}, store.Invalid("mergeMessage", nil, "a merge needs a message, a line saying what the work does")
	}
	member, err := h.workTree(ctx, memberID)
	if err != nil {
		return worktree.MergeResult{}, err
	}
	project, err := h.store.RoomProject(ctx, member.RoomID)
	if err != nil {
		return worktree.MergeResult{}, err
	}
	others, err := h.otherWorktrees(ctx, project, member.ID)
	if err != nil {
		return worktree.MergeResult{}, err
	}
	held := h.heldBy(ctx, member, others)
	res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
		Op: protocol.WorkspaceSquash, Checkout: member.RepoPath, Dir: member.WorktreeDir, Message: message, Leave: leave,
	})
	if err != nil {
		return worktree.MergeResult{}, err
	}
	if res.Error != "" {
		return worktree.MergeResult{}, refused(res, member.DisplayName)
	}
	if res.Merge == nil {
		return worktree.MergeResult{}, errors.New("the machine answered nothing")
	}
	if res.Merge.Commit != "" {
		for _, m := range append([]store.Member{member}, held...) {
			h.turns.forgetOverlaps(ctx, m)
		}
		h.noteMerge(ctx, project, member, held, res.Merge.Commit, message)
		h.followMerge(ctx, others)
	}
	return *res.Merge, nil
}

// otherWorktrees are the members of a project, but the one named, that
// work in worktrees of their own.
func (h *Hub) otherWorktrees(ctx context.Context, project store.Project, but string) ([]store.Member, error) {
	members, err := h.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return nil, err
	}
	var out []store.Member
	for _, m := range members {
		if m.ID != but && inWorktree(m, project) {
			out = append(out, m)
		}
	}
	return out, nil
}

// heldBy are the others whose work member's branch has in full: merged
// with it, theirs goes on the main line too. A worktree whose status is
// not known holds nothing and is held by nothing.
func (h *Hub) heldBy(ctx context.Context, member store.Member, others []store.Member) []store.Member {
	if len(others) == 0 {
		return nil
	}
	everyone := append([]store.Member{member}, others...)
	statuses := make([]*worktree.Status, len(everyone))
	var wg sync.WaitGroup
	for i, m := range everyone {
		wg.Go(func() { statuses[i], _ = worktreeStatus(ctx, h.workspace, m) })
	}
	wg.Wait()
	var held []store.Member
	for i, o := range others {
		if holds(statuses[0], statuses[i+1]) {
			held = append(held, o)
		}
	}
	return held
}

// followMerge brings the members with no work of their own, now those
// whose work was merged, up to the new main line; one at work is brought
// up before its next turn, as always.
func (h *Hub) followMerge(ctx context.Context, members []store.Member) {
	for _, m := range members {
		if h.turns.busy(m.ID) {
			continue
		}
		if _, err := h.turns.followMainLine(ctx, m); err != nil {
			h.logger.Warn("bring a worktree up to a merge", "member", m.ID, "err", err)
		}
	}
}

// noteMerge says in the project's chat that a member's work went on the
// main line, and whose it had in it, with the first line of the message.
func (h *Hub) noteMerge(ctx context.Context, project store.Project, member store.Member, held []store.Member, commit, message string) {
	work := member.DisplayName + "'s work"
	if len(held) > 0 {
		names := make([]string, len(held))
		for i, m := range held {
			names[i] = m.DisplayName + "'s"
		}
		work += ", which had " + andList(names) + " in it,"
	}
	body := fmt.Sprintf("Merged %s into the main line as %s: %s", work, shortCommit(commit), firstLine(message))
	if _, err := h.turns.post(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: body}); err != nil {
		h.logger.Error("note a merge", "member", member.ID, "err", err)
	}
}

// SetAside gives up what a member's worktree has, keeping it in git under
// a ref of Veyloom's, and starts the worktree over from the main line
// (docs/design.md 5.21); the chat is told where the work is kept. It is
// that ref.
func (h *Hub) SetAside(ctx context.Context, memberID string) (string, error) {
	member, err := h.workTree(ctx, memberID)
	if err != nil {
		return "", err
	}
	res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
		Op: protocol.WorkspaceSetAside, Checkout: member.RepoPath, Dir: member.WorktreeDir,
		Message: fmt.Sprintf("What %s had when a person set it aside", member.DisplayName),
	})
	if err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", refused(res, member.DisplayName)
	}
	h.turns.forgetOverlaps(ctx, member)
	project, err := h.store.RoomProject(ctx, member.RoomID)
	if err != nil {
		return res.Ref, nil
	}
	body := resetNote(member.DisplayName, member.Branch, res.Ref)
	if _, err := h.turns.post(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: body}); err != nil {
		h.logger.Error("note work set aside", "member", member.ID, "err", err)
	}
	return res.Ref, nil
}

// resetNote is the chat's line for a member's branch reset to the main
// line, what it had archived first under a ref of Veyloom's.
func resetNote(member, branch, ref string) string {
	if branch == "" {
		branch = "worktree"
	}
	return fmt.Sprintf("Reset %s's branch %s to the main line; its work is archived as %s.", member, branch, ref)
}

// andList names things the way a sentence does: "A", "A and B", "A, B and C".
func andList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
