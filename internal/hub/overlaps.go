package hub

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// Overlaps (docs/design.md 5.21): two members changed the same file, each
// in its own worktree and its own way, so whichever of them is merged
// second may conflict. As a member's turn in its worktree ends, the files
// it changed that others changed too are worked out, uncommitted changes
// included, and the chat is told about the ones it was not told about
// before. Two members' work is set side by side from where they last
// shared history: what one merged from the other is no overlap, nor is a
// file both changed alike. An overlap that went away, the work merged or
// undone, is told about again should it come back.

// overlapTimeout bounds working out the overlaps as a turn ends.
const overlapTimeout = 2 * time.Minute

// overlapFilesNamed caps the files a note names; the rest are counted.
const overlapFilesNamed = 5

// checkOverlaps works out, in the background, the overlaps of the work of
// a member whose turn ran; nothing comes of it unless the turn ran in the
// member's worktree. The work is counted before the turn is recorded as
// over, so whoever sees it over can wait for it (overlapChecks).
func (m *TurnManager) checkOverlaps(at *activeTurn) {
	at.mu.Lock()
	member, dir, dispatched := at.member, at.dir, at.dispatched
	at.mu.Unlock()
	if m.workspace == nil || !dispatched || dir == "" {
		return
	}
	m.overlapChecks.Add(1)
	go func() {
		defer m.overlapChecks.Done()
		ctx, cancel := context.WithTimeout(context.Background(), overlapTimeout)
		defer cancel()
		if err := m.noteOverlaps(ctx, member.ID, member.RoomID, dir); err != nil {
			m.logger.Warn("work out overlapping work", "member", member.ID, "err", err)
		}
	}()
}

// noteOverlaps works out which files of a member's work other members
// changed too, and tells the chat about the new ones. dir is where the
// member's turn ran. The overlaps of a project's members are worked out
// one member at a time.
func (m *TurnManager) noteOverlaps(ctx context.Context, memberID, roomID, dir string) error {
	defer m.lockOverlaps(roomID)()
	project, err := m.store.RoomProject(ctx, roomID)
	if err != nil {
		return err
	}
	members, err := m.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return err
	}
	var member store.Member
	var others []store.Member
	for _, o := range members {
		switch {
		case o.ID == memberID:
			member = o
		case inWorktree(o, project):
			others = append(others, o)
		}
	}
	if member.ID == "" || !inWorktree(member, project) || member.WorkDir != dir {
		return nil
	}
	if len(others) == 0 {
		if len(member.OverlapsNoted) == 0 {
			return nil
		}
		return m.store.SetMemberOverlaps(ctx, member.ID, nil)
	}

	everyone := append([]store.Member{member}, others...)
	statuses := make([]*worktree.Status, len(everyone))
	var wg sync.WaitGroup
	for i, o := range everyone {
		wg.Go(func() { statuses[i], _ = worktreeStatus(ctx, m.workspace, o) })
	}
	wg.Wait()
	if statuses[0] == nil {
		return errors.New("how its worktree stands is not known")
	}
	mine := map[string]bool{}
	for _, f := range statuses[0].Files {
		mine[f.Path] = true
	}
	partners := map[string][]store.Member{}
	unknown := false
	for i, o := range others {
		st := statuses[i+1]
		if st == nil {
			unknown = true
			continue
		}
		for _, path := range sharedFiles(ctx, m.workspace, member, statuses[0], st) {
			partners[path] = append(partners[path], o)
		}
	}

	overlapping := slices.Sorted(maps.Keys(partners))
	var fresh []string
	for _, path := range overlapping {
		if !slices.Contains(member.OverlapsNoted, path) {
			fresh = append(fresh, path)
		}
	}
	keep := overlapping
	if unknown {
		// Whether a member not heard from still changes a file is not
		// known: what was told stays while the member's own work is there.
		for _, path := range member.OverlapsNoted {
			if mine[path] && partners[path] == nil {
				keep = append(keep, path)
			}
		}
	}
	if err := m.store.SetMemberOverlaps(ctx, member.ID, keep); err != nil {
		return err
	}

	// One note for each set of members the new files are shared with.
	type shared struct {
		with  []store.Member
		files []string
	}
	var notes []*shared
	by := map[string]*shared{}
	for _, path := range fresh {
		key := strings.Join(memberIDs(partners[path]), ",")
		n := by[key]
		if n == nil {
			n = &shared{with: partners[path]}
			by[key] = n
			notes = append(notes, n)
		}
		n.files = append(n.files, path)
	}
	for _, n := range notes {
		names := []string{member.DisplayName}
		for _, o := range n.with {
			// The other side was told as well.
			if err := m.store.AddMemberOverlaps(ctx, o.ID, n.files); err != nil {
				return err
			}
			names = append(names, o.DisplayName)
		}
		if _, err := m.post(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: overlapNote(names, n.files)}); err != nil {
			return err
		}
	}
	return nil
}

// overlapNote tells the chat that the members named changed the same
// files, the member whose turn found it first.
func overlapNote(names, files []string) string {
	who := andList(names) + " both"
	if len(names) > 2 {
		who = andList(names) + " all"
	}
	named := files
	if len(files) > overlapFilesNamed {
		named = files[:overlapFilesNamed]
	}
	list := strings.Join(named, ", ")
	if more := len(files) - len(named); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	return fmt.Sprintf("%s changed %s: whichever is merged second may conflict.", who, list)
}

// sharedFiles are the files the work of two members of a project both
// changes, each its own way, a and b their worktrees' statuses: of those
// both list as changed, the ones the machine finds changed on both sides
// since the two last shared history, and not alike. A build either side
// made and never committed, which a merge leaves out unless a person says
// otherwise, is none of them. When the machine cannot tell, the files both
// list stand.
func sharedFiles(ctx context.Context, call workspaceCall, member store.Member, a, b *worktree.Status) []string {
	built := func(f worktree.Change) bool { return f.New && f.Binary }
	listed := map[string]bool{}
	for _, f := range a.Files {
		listed[f.Path] = !built(f)
	}
	var both []string
	for _, f := range b.Files {
		if listed[f.Path] && !built(f) {
			both = append(both, f.Path)
		}
	}
	if len(both) == 0 || a.At.Head == "" || b.At.Head == "" {
		return both
	}
	res, err := call(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceOverlap, Checkout: member.RepoPath, Sides: []worktree.Side{a.At, b.At}})
	if err != nil || res.Error != "" {
		return both
	}
	return slices.DeleteFunc(both, func(path string) bool { return !slices.Contains(res.Files, path) })
}

// forgetOverlaps forgets the overlaps of a member's work that went onto
// the main line: its worktree starts over, and an overlap there is new.
func (m *TurnManager) forgetOverlaps(ctx context.Context, member store.Member) {
	defer m.lockOverlaps(member.RoomID)()
	if err := m.store.SetMemberOverlaps(ctx, member.ID, nil); err != nil {
		m.logger.Error("forget the overlaps of merged work", "member", member.ID, "err", err)
	}
}

// lockOverlaps takes the lock the overlaps of a project's members are
// worked out under, by the project's main room, and gives back its unlock.
func (m *TurnManager) lockOverlaps(roomID string) func() {
	m.overlapMu.Lock()
	if m.overlapLocks == nil {
		m.overlapLocks = map[string]*sync.Mutex{}
	}
	lock := m.overlapLocks[roomID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.overlapLocks[roomID] = lock
	}
	m.overlapMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// inWorktree says a member works in a worktree of its own, got ready: one
// with work that can overlap another's.
func inWorktree(m store.Member, project store.Project) bool {
	return !m.Removed() && m.ID != project.LeaderID && m.BranchMode != store.BranchShared && m.WorktreeDir != "" && m.PreparedAt != nil
}

// worktreeStatus asks a member's machine how its worktree stands; the
// reason says why that is not known.
func worktreeStatus(ctx context.Context, call workspaceCall, m store.Member) (*worktree.Status, string) {
	res, err := call(ctx, m.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceStatus, Checkout: m.RepoPath, Dir: m.WorktreeDir})
	switch {
	case err != nil:
		return nil, store.Reason(err)
	case res.Error != "":
		return nil, res.Error
	case res.Status == nil:
		return nil, "the machine answered nothing"
	}
	return res.Status, ""
}

func memberIDs(members []store.Member) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	return ids
}
