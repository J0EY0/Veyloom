package hub

import (
	"context"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
)

// The project's checkout (docs/design.md 5.21): what was changed there and
// not committed, which the members' worktrees lack and which stops merging
// work that changes the same files. A person reads it and commits it from
// the branch tab.

// CheckoutDiff is the patch of what the project's checkout changed and did
// not commit, new files too; cut says it was cut to its start.
func (h *Hub) CheckoutDiff(ctx context.Context, projectID string) (patch string, cut bool, err error) {
	project, machine, err := h.checkout(ctx, projectID)
	if err != nil {
		return "", false, err
	}
	res, err := h.workspace(ctx, machine, protocol.WorkspaceRequest{Op: protocol.WorkspaceCheckoutDiff, Checkout: project.RepoPath, MaxBytes: maxDiff})
	if err != nil {
		return "", false, err
	}
	if res.Error != "" {
		return "", false, refused(res, "")
	}
	return res.Text, res.Cut, nil
}

// CommitCheckout commits, on the checkout's branch, the changes to the
// files a person picked, paths from the top of the repository, with the
// message they wrote, and says so in the chat. It is the new commit. It
// waits for the members working in the checkout to finish their turns.
func (h *Hub) CommitCheckout(ctx context.Context, projectID, message string, paths []string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", store.Invalid("commitMessage", nil, "a commit needs a message, a line saying what the change does")
	}
	if len(paths) == 0 {
		return "", store.Invalid("commitFiles", nil, "a commit needs the files it takes")
	}
	project, machine, err := h.checkout(ctx, projectID)
	if err != nil {
		return "", err
	}
	members, err := h.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return "", err
	}
	for _, m := range members {
		inCheckout := m.ID == project.LeaderID || m.BranchMode == store.BranchShared
		if !m.Removed() && inCheckout && h.turns.busy(m.ID) {
			return "", store.Conflicting("checkoutBusy", store.Params{"name": m.DisplayName},
				"%s is at work in the checkout: commit once its turn is over", m.DisplayName)
		}
	}
	res, err := h.workspace(ctx, machine, protocol.WorkspaceRequest{Op: protocol.WorkspaceCheckoutCommit, Checkout: project.RepoPath, Message: message, Paths: paths})
	if err != nil {
		return "", err
	}
	if res.Error != "" {
		if res.Code == protocol.WorkspaceNoChanges {
			return "", store.Conflicting("checkoutClean", nil, "none of those files has changes to commit")
		}
		return "", refused(res, "")
	}
	body := fmt.Sprintf("Committed the changes in the project's checkout as %s: %s", shortCommit(res.Commit), message)
	if _, err := h.turns.post(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: body}); err != nil {
		h.logger.Error("note a commit in the checkout", "project", project.ID, "err", err)
	}
	return res.Commit, nil
}

// checkout is a project that has a checkout, and the machine that looks
// at it.
func (h *Hub) checkout(ctx context.Context, projectID string) (store.Project, string, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return store.Project{}, "", err
	}
	if project.RepoPath == "" {
		return store.Project{}, "", store.Conflicting("noRepoPath", nil, "the project has no checkout")
	}
	members, err := h.store.ListRoomMembers(ctx, project.MainRoomID)
	if err != nil {
		return store.Project{}, "", err
	}
	machine := checkoutMachine(project, members)
	if machine == "" {
		return store.Project{}, "", store.Conflicting("noCheckoutMachine", nil, "%s", errNoMembers.Error())
	}
	return project, machine, nil
}

// checkoutMachine is the machine that looks at a project's checkout: its
// leader's, else the first of its members'.
func checkoutMachine(project store.Project, members []store.Member) string {
	machine := ""
	for _, m := range members {
		if m.Removed() {
			continue
		}
		if machine == "" || m.ID == project.LeaderID {
			machine = m.MachineID
		}
	}
	return machine
}
