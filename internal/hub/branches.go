package hub

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// A project's branches, for the chat's branch tab (docs/design.md 5.21):
// the main line, the project's checkout on the branch it has, and each
// member's worktree against it, with what a person does to them: look at
// the changes, put them on the main line as one commit, bring the main line
// in.

// Branches is how a project's branches stand.
type Branches struct {
	Main MainLine `json:"main"`
	// Members are the members that have a worktree, or will have one the
	// first time they are asked for something, in the order they joined.
	// The leader, and the members sharing the checkout, work on the main
	// line itself.
	Members []MemberBranch `json:"members"`
	// Overlaps are the files two members or more changed, each on its own
	// branch and its own way: merging the second may conflict.
	Overlaps []Overlap `json:"overlaps,omitempty"`
}

// MainLine is the project's checkout.
type MainLine struct {
	RepoPath string `json:"repo_path"`
	// Git says the checkout is in a git repository with a commit to branch
	// off; the members have worktrees only then.
	Git bool `json:"git"`
	// Branch is the branch it has, the main line; empty while detached.
	Branch string `json:"branch,omitempty"`
	// Changed are the files changed there and not committed: the members'
	// worktrees do not have them.
	Changed []worktree.Change `json:"changed,omitempty"`
	// Error says why the checkout could not be looked at.
	Error string `json:"error,omitempty"`
}

// MemberBranch is one member's worktree.
type MemberBranch struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	// Branch and Dir are its worktree's; empty before it has one. Prepared
	// says it was got ready for work.
	Branch   string `json:"branch,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Prepared bool   `json:"prepared"`
	// Busy says a turn of the member's is under way or about to start:
	// nothing is done to its worktree meanwhile.
	Busy bool `json:"busy"`
	// Status is how its worktree stands; Error why that is not known.
	Status *worktree.Status `json:"status,omitempty"`
	Error  string           `json:"error,omitempty"`
	// Draft is the message for the commit that merges its work: what its
	// own commits say, or, with none, what it was last asked in the chat.
	Draft string `json:"draft,omitempty"`
	// Contains are the members whose work its branch has in full, having
	// merged theirs: merging it brings theirs, and the two do not
	// conflict over the files both changed.
	Contains []string `json:"contains,omitempty"`
}

// Overlap is a file more than one member changed.
type Overlap struct {
	Path    string   `json:"path"`
	Members []string `json:"members"`
}

// Why an action on a member's worktree is refused.
var (
	errNoWorktree = store.Conflicting("noWorktree", nil, "the member has no worktree yet: it gets one the first time it is asked for something")
	errNoMembers  = errors.New("the project has no member whose machine could look at its checkout")
)

// Branches says how a project's branches stand.
func (h *Hub) Branches(ctx context.Context, projectID string) (Branches, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return Branches{}, err
	}
	members, err := h.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return Branches{}, err
	}
	out := Branches{Main: MainLine{RepoPath: project.RepoPath}, Members: []MemberBranch{}}
	var employees []store.Member
	for _, m := range members {
		if !m.Removed() && m.ID != project.LeaderID && m.BranchMode != store.BranchShared && m.RepoPath != "" {
			employees = append(employees, m)
		}
	}
	machine := checkoutMachine(project, members)
	if project.RepoPath == "" {
		return out, nil
	}
	if machine == "" {
		out.Main.Error = errNoMembers.Error()
		return out, nil
	}
	res, err := h.workspace(ctx, machine, protocol.WorkspaceRequest{Op: protocol.WorkspaceInspect, Checkout: project.RepoPath})
	switch {
	case err != nil:
		out.Main.Error = store.Reason(err)
		return out, nil
	case res.Code == protocol.WorkspaceNotRepo:
		return out, nil
	case res.Error != "":
		out.Main.Error = res.Error
		return out, nil
	}
	if res.Repo == nil || res.Repo.Head == "" {
		return out, nil
	}
	out.Main.Git, out.Main.Branch, out.Main.Changed = true, res.Repo.Branch, res.Repo.Changed

	out.Members = make([]MemberBranch, len(employees))
	var wg sync.WaitGroup
	for i, m := range employees {
		out.Members[i] = MemberBranch{
			MemberID: m.ID, Name: m.DisplayName, Branch: m.Branch, Dir: m.WorktreeDir, Prepared: m.PreparedAt != nil,
			Busy: h.turns.busy(m.ID), Draft: h.mergeDraft(ctx, project, m),
		}
		if m.WorktreeDir == "" {
			continue
		}
		b := &out.Members[i]
		wg.Go(func() { b.Status, b.Error = worktreeStatus(ctx, h.workspace, m) })
	}
	wg.Wait()
	for i := range out.Members {
		// What the member's own commits say beats what it was asked.
		if st := out.Members[i].Status; st != nil && len(st.Commits) > 0 {
			out.Members[i].Draft = commitsDraft(st.Commits)
		}
		for j := range out.Members {
			if i != j && holds(out.Members[i].Status, out.Members[j].Status) {
				out.Members[i].Contains = append(out.Members[i].Contains, out.Members[j].MemberID)
			}
		}
	}
	out.Overlaps = h.overlaps(ctx, employees, out.Members)
	return out, nil
}

// holds says the work of the worktree whose status is a is all in the one
// whose status is b, its latest commit merged there and nothing of it left
// uncommitted: merging b brings a's work along, and the two do not
// conflict over the files both changed. The commits named are the latest
// few only, so a merge long ago may go unseen.
func holds(b, a *worktree.Status) bool {
	if a == nil || b == nil || len(a.Commits) == 0 || a.Uncommitted > 0 {
		return false
	}
	tip := a.Commits[len(a.Commits)-1].Hash
	return slices.ContainsFunc(b.Commits, func(c worktree.Commit) bool { return c.Hash == tip })
}

// commitsDraft is the message for the commit that puts a member's work on
// the main line, from what its own commits say, oldest first: the first
// one's subject and body, the others' subjects listed below it.
func commitsDraft(commits []worktree.Commit) string {
	draft := commits[0].Subject
	if body := strings.TrimSpace(commits[0].Body); body != "" && len(commits) == 1 {
		return draft + "\n\n" + body
	}
	if len(commits) > 1 {
		draft += "\n"
		for _, c := range commits[1:] {
			draft += "\n- " + c.Subject
		}
	}
	return draft
}

// overlaps are the files more than one member changed, each its own way
// (sharedFiles), by path; branches are the members' in the order of
// members, their statuses in.
func (h *Hub) overlaps(ctx context.Context, members []store.Member, branches []MemberBranch) []Overlap {
	type pair struct {
		a, b  int
		files []string
	}
	var pairs []*pair
	for i := range branches {
		for j := i + 1; j < len(branches); j++ {
			if branches[i].Status != nil && branches[j].Status != nil {
				pairs = append(pairs, &pair{a: i, b: j})
			}
		}
	}
	var wg sync.WaitGroup
	for _, p := range pairs {
		wg.Go(func() {
			p.files = sharedFiles(ctx, h.workspace, members[p.a], branches[p.a].Status, branches[p.b].Status)
		})
	}
	wg.Wait()
	// Which branches changed each file, in the order of the branches.
	by := map[string][]bool{}
	for _, p := range pairs {
		for _, path := range p.files {
			if by[path] == nil {
				by[path] = make([]bool, len(branches))
			}
			by[path][p.a], by[path][p.b] = true, true
		}
	}
	var out []Overlap
	for path, in := range by {
		o := Overlap{Path: path}
		for i, changed := range in {
			if changed {
				o.Members = append(o.Members, branches[i].MemberID)
			}
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b Overlap) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// mergeDraft is a first line for the commit that puts a member's work on
// the main line: what it was last asked in the chat, the mention left out.
func (h *Hub) mergeDraft(ctx context.Context, project store.Project, member store.Member) string {
	turns, err := h.store.ListRoomTurns(ctx, project.MainRoomID, turnsLooked)
	if err != nil {
		return ""
	}
	for _, t := range turns {
		if t.MemberID != member.ID || t.Kind != store.TurnChat {
			continue
		}
		asked, err := h.store.GetMessage(ctx, t.TriggerMessageID)
		if err != nil {
			return ""
		}
		line, _, _ := strings.Cut(strings.TrimSpace(asked.Body), "\n")
		line = strings.TrimSpace(strings.TrimPrefix(line, "@"+member.DisplayName))
		if runes := []rune(line); len(runes) > 72 {
			line = strings.TrimSpace(string(runes[:72]))
		}
		return line
	}
	return ""
}

// workTree is a member whose worktree a person acts on: one with a
// worktree, and no turn under way.
func (h *Hub) workTree(ctx context.Context, memberID string) (store.Member, error) {
	member, err := h.store.GetMember(ctx, memberID)
	if err != nil {
		return store.Member{}, err
	}
	if member.WorktreeDir == "" {
		return store.Member{}, errNoWorktree
	}
	if h.turns.busy(member.ID) {
		return store.Member{}, store.Conflicting("memberBusy", store.Params{"name": member.DisplayName},
			"%s is at work: its worktree is left alone until the turn is over", member.DisplayName)
	}
	return member, nil
}

// maxDiff caps the patch a person reads in the branch tab.
const maxDiff = 512 << 10

// DiffOf is the patch of what a member's worktree changed since it branched
// off the main line, committed or not; cut says it was cut to its start.
func (h *Hub) DiffOf(ctx context.Context, memberID string) (patch string, cut bool, err error) {
	member, err := h.store.GetMember(ctx, memberID)
	if err != nil {
		return "", false, err
	}
	if member.WorktreeDir == "" {
		return "", false, errNoWorktree
	}
	res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceDiff, Checkout: member.RepoPath, Dir: member.WorktreeDir, MaxBytes: maxDiff})
	if err != nil {
		return "", false, err
	}
	if res.Error != "" {
		return "", false, refused(res, member.DisplayName)
	}
	return res.Text, res.Cut, nil
}

// SyncMember brings the main line into a member's worktree. Conflicts come
// back in the result, the merge given up.
func (h *Hub) SyncMember(ctx context.Context, memberID string) (worktree.SyncResult, error) {
	member, err := h.workTree(ctx, memberID)
	if err != nil {
		return worktree.SyncResult{}, err
	}
	res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceSync, Checkout: member.RepoPath, Dir: member.WorktreeDir})
	if err != nil {
		return worktree.SyncResult{}, err
	}
	if res.Error != "" {
		return worktree.SyncResult{}, refused(res, member.DisplayName)
	}
	if res.Sync == nil {
		return worktree.SyncResult{}, errors.New("the machine answered nothing")
	}
	return *res.Sync, nil
}

// AbortMerge gives up a merge a member left under way in its worktree: the
// worktree is as it was before the merge began.
func (h *Hub) AbortMerge(ctx context.Context, memberID string) error {
	member, err := h.workTree(ctx, memberID)
	if err != nil {
		return err
	}
	res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceAbortMerge, Checkout: member.RepoPath, Dir: member.WorktreeDir})
	if err != nil {
		return err
	}
	if res.Error != "" {
		return refused(res, member.DisplayName)
	}
	return nil
}

// refused is a machine's failure on the worktree of the member named as a
// problem people read: the ones Veyloom knows by name, else git's own
// words.
func refused(res protocol.WorkspaceResult, name string) error {
	switch res.Code {
	case protocol.WorkspaceMergeUnderway:
		return store.Conflicting("mergeUnderway", store.Params{"name": name},
			"%s's worktree is in the middle of a merge: its conflicts are settled, or the merge given up, first", name)
	case protocol.WorkspaceNoChanges:
		return store.Conflicting("noChanges", nil, "the worktree has nothing the main line lacks")
	case protocol.WorkspaceDetached:
		return store.Conflicting("mainDetached", nil, "the project's checkout is on no branch: check one out to merge into")
	case protocol.WorkspaceMainLineMoved:
		return store.Conflicting("mainLineMoved", nil, "the main line got a commit meanwhile: try again")
	case protocol.WorkspaceGone:
		return store.Conflicting("worktreeGone", nil, "the worktree is not there any more: the member gets a new one the next time it is asked for something")
	case protocol.WorkspaceNotNew:
		return store.Conflicting("leaveNotNew", store.Params{"files": strings.Join(res.Files, "\n")},
			"only new files, never committed, can be left out of a merge, not %s", strings.Join(res.Files, ", "))
	case protocol.WorkspaceCheckoutChanged:
		return store.Conflicting("checkoutChanged", store.Params{"files": strings.Join(res.Files, "\n")},
			"the project's checkout has changes not committed to %s, which the merge changes too: commit them first", strings.Join(res.Files, ", "))
	}
	return store.Conflicting("gitRefused", store.Params{"message": res.Error}, "git refused: %s", res.Error)
}

// busy reports whether a member has a turn under way or about to start.
func (m *TurnManager) busy(memberID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.members[memberID]
	return st != nil && (st.starting || st.running != nil)
}
