package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Where a member works (docs/design.md 5.21). The project's leader, a
// member set to share the checkout, and every member while the checkout is
// no git repository or has no commit yet work in the checkout itself.
// Every other member works in a git worktree of its own, on a branch of
// its own: made the first time it is asked for something, once the
// leader has set the project up, and got ready the way the leader wrote
// down. Until then its turn waits, saying so; a member does not work in a
// worktree that is not ready. Before each turn, a worktree with no work of
// its own is brought up to the main line.

// worktreeStore is what the members' worktrees need of the store.
type worktreeStore interface {
	SetProjectSetupThread(ctx context.Context, projectID, threadID string) (bool, error)
	SetWorkspaceSteps(ctx context.Context, projectID string, steps store.WorkspaceSteps) error
	SetWorkspacePending(ctx context.Context, projectID string, steps store.WorkspaceSteps, messageID string) error
	AdoptWorkspacePending(ctx context.Context, projectID string) error
	DropWorkspacePending(ctx context.Context, projectID string) error
	MarkProjectInitialized(ctx context.Context, projectID string) error
	SetMemberWorkspace(ctx context.Context, memberID, worktreeDir, workDir, branch string) (store.Member, error)
	MarkMemberPrepared(ctx context.Context, memberID string) (store.Member, error)
	ClearMemberWorkspace(ctx context.Context, memberID string) error
	SetMemberOverlaps(ctx context.Context, memberID string, files []string) error
	AddMemberOverlaps(ctx context.Context, memberID string, files []string) error
	// What became of a member's branch, for the task board (docs/webui.md
	// 4.20).
	RecordBranchEvent(ctx context.Context, e store.BranchEvent) (store.BranchEvent, error)
}

// prepareLimit bounds how long a turn waits for its member's worktree:
// the leader setting the project up, then the steps it wrote down.
const prepareLimit = 45 * time.Minute

// workspaceCall is a request to a machine about a checkout or a worktree:
// the hub's workspace, which the turns reach through their manager.
type workspaceCall func(ctx context.Context, machineID string, req protocol.WorkspaceRequest) (protocol.WorkspaceResult, error)

// works says where member works: the checkout, or its worktree once one
// is ready. It asks no machine anything and makes nothing: it is for the
// turns that do not get a worktree ready, an upkeep or the leader's setup.
func works(member store.Member, project store.Project) string {
	if member.ID != project.LeaderID && member.BranchMode != store.BranchShared && member.WorktreeDir != "" && member.PreparedAt != nil {
		return member.WorkDir
	}
	return member.RepoPath
}

// dirFor says where the member of at works this turn, making and getting
// its worktree ready first when it needs one; what it waits for goes to
// the turn as notices. The member of at is updated with its worktree.
func (m *TurnManager) dirFor(ctx context.Context, at *activeTurn) (string, error) {
	member := at.member
	if member.RepoPath == "" || m.workspace == nil {
		return member.RepoPath, nil
	}
	project, err := m.store.RoomProject(ctx, member.RoomID)
	if err != nil {
		return "", err
	}
	if member.ID == project.LeaderID || member.BranchMode == store.BranchShared {
		return member.RepoPath, nil
	}
	if member.WorktreeDir != "" && member.PreparedAt != nil {
		gone, err := m.followMainLine(ctx, member)
		if err != nil {
			return "", err
		}
		if !gone {
			return member.WorkDir, nil
		}
		// Taken away outside Veyloom: made again below.
		if err := m.store.ClearMemberWorkspace(ctx, member.ID); err != nil {
			return "", err
		}
		member.WorktreeDir, member.WorkDir, member.Branch, member.PreparedAt = "", "", "", nil
	}
	res, err := m.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceInspect, Checkout: member.RepoPath})
	if err != nil {
		return "", err
	}
	if res.Code == protocol.WorkspaceNotRepo || (res.Error == "" && (res.Repo == nil || res.Repo.Head == "")) {
		// Nothing to branch off: it works in the checkout, as everyone did.
		return member.RepoPath, nil
	}
	if res.Error != "" {
		return "", workspaceFailure(res)
	}
	if err := m.awaitSetup(ctx, at, project); err != nil {
		return "", err
	}
	if project, err = m.store.GetProject(ctx, project.ID); err != nil {
		return "", err
	}
	if member.WorktreeDir == "" {
		if member, err = m.makeWorktree(ctx, at, member, project); err != nil {
			return "", err
		}
	}
	if err := m.prepareWorktree(ctx, at, member, project); err != nil {
		return "", err
	}
	if member, err = m.store.MarkMemberPrepared(ctx, member.ID); err != nil {
		return "", err
	}
	at.mu.Lock()
	at.member = member
	at.mu.Unlock()
	return member.WorkDir, nil
}

// followMainLine brings a worktree with no work of its own up to the main
// line before the member's turn, so it starts from what the others merged.
// gone says the worktree is not there any more. A worktree the machine
// cannot bring forward is worked in as it is: the person syncs it by hand.
func (m *TurnManager) followMainLine(ctx context.Context, member store.Member) (gone bool, err error) {
	res, err := m.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
		Op: protocol.WorkspaceSync, Checkout: member.RepoPath, Dir: member.WorktreeDir, FastForwardOnly: true,
	})
	if err != nil {
		return false, err
	}
	if res.Code == protocol.WorkspaceGone {
		return true, nil
	}
	if res.Error != "" {
		m.logger.Warn("bring a worktree up to the main line", "member", member.ID, "err", res.Error)
	}
	return false, nil
}

// makeWorktree makes the member's worktree and records it.
func (m *TurnManager) makeWorktree(ctx context.Context, at *activeTurn, member store.Member, project store.Project) (store.Member, error) {
	slug := memberSlug(member)
	m.notice(at, runtime.NoticeInfo, fmt.Sprintf("Making %s's worktree, on the branch veyloom/%s.", member.DisplayName, slug))
	res, err := m.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
		Op: protocol.WorkspaceCreate, Checkout: member.RepoPath, Name: project.WikiSlug + "/" + slug, Branch: "veyloom/" + slug,
	})
	if err != nil {
		return store.Member{}, err
	}
	if res.Error != "" || res.Workspace == nil {
		return store.Member{}, workspaceFailure(res)
	}
	return m.store.SetMemberWorkspace(ctx, member.ID, res.Workspace.Dir, res.Workspace.WorkDir, res.Workspace.Branch)
}

// prepareWorktree gets the member's new worktree ready the way the
// project's steps say. A failure is told to the leader, in the project's
// setup topic, for it to set the steps right; the member is asked again
// by a person.
func (m *TurnManager) prepareWorktree(ctx context.Context, at *activeTurn, member store.Member, project store.Project) error {
	if len(project.WorkspaceCopy) == 0 && project.WorkspaceRun == "" {
		return nil
	}
	m.notice(at, runtime.NoticeInfo, "Getting the worktree ready: "+stepsLine(store.WorkspaceSteps{Copy: project.WorkspaceCopy, Run: project.WorkspaceRun})+".")
	res, err := m.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
		Op: protocol.WorkspacePrepare, Checkout: member.RepoPath, WorkDir: member.WorkDir, Copy: project.WorkspaceCopy, Run: project.WorkspaceRun,
	})
	if err != nil {
		return err
	}
	if res.Error == "" {
		return nil
	}
	m.tellLeader(project, member, res)
	return fmt.Errorf("its worktree could not be got ready (%s); the leader is told, and it is asked again once the steps are set right", res.Error)
}

// stepsLine says in words what getting a worktree ready does.
func stepsLine(steps store.WorkspaceSteps) string {
	var parts []string
	if len(steps.Copy) > 0 {
		parts = append(parts, "copy "+strings.Join(steps.Copy, ", "))
	}
	if steps.Run != "" {
		parts = append(parts, "run "+steps.Run)
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", then ")
}

// notice shows a line of the hub's in a turn that is under way, the way a
// runtime's are shown; kept for the transcript once it is open.
func (m *TurnManager) notice(at *activeTurn, level, text string) {
	m.OnEvent(at.turn.ID, runtime.Event{Kind: runtime.EventNotice, Level: level, Text: text, At: time.Now()})
}

// workspaceFailure is what a machine said went wrong, as an error.
func workspaceFailure(res protocol.WorkspaceResult) error {
	if res.Error == "" {
		return errors.New("the machine answered nothing")
	}
	return errors.New(res.Error)
}

// memberSlug names a member's worktree and branch: the words of its name
// in lower case, or its id's first letters for a name with none in ASCII.
func memberSlug(member store.Member) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(member.DisplayName) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	slug := b.String()
	if len(slug) > 30 {
		slug = strings.TrimRight(slug[:30], "-")
	}
	if slug == "" {
		id := strings.ReplaceAll(member.ID, "-", "")
		slug = "member-" + id[:min(8, len(id))]
	}
	return slug
}

// ReleaseWorktree takes the worktree of a member taken out of its project
// away (docs/design.md 5.21): what it had not committed is committed on
// its branch first, and the branch stays, so no work is lost. It goes on
// in the background; a failure is logged, the worktree left for a person.
func (h *Hub) ReleaseWorktree(member store.Member) {
	if member.WorktreeDir == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
		defer cancel()
		res, err := h.workspace(ctx, member.MachineID, protocol.WorkspaceRequest{
			Op: protocol.WorkspaceRemove, Checkout: member.RepoPath, Dir: member.WorktreeDir,
			Message: fmt.Sprintf("What %s left uncommitted when taken out of the project", member.DisplayName),
		})
		if err == nil && res.Error != "" {
			err = workspaceFailure(res)
		}
		if err != nil {
			h.logger.Error("take a removed member's worktree away", "member", member.ID, "dir", member.WorktreeDir, "err", err)
			return
		}
		if err := h.store.ClearMemberWorkspace(ctx, member.ID); err != nil {
			h.logger.Error("forget a removed member's worktree", "member", member.ID, "err", err)
		}
	}()
}

// releaseTimeout bounds taking a worktree away.
const releaseTimeout = 2 * time.Minute
