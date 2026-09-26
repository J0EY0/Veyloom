package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// What the store keeps of a project's members' git worktrees
// (docs/design.md 5.21): how a new one is got ready, whether the project
// was set up for them, and each member's once made.

// SetProjectSetupThread records the project's setup topic. False says it
// had one already, which stays.
func (s *Store) SetProjectSetupThread(ctx context.Context, projectID, threadID string) (bool, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return false, err
	}
	tid, err := parseUUID(threadID)
	if err != nil {
		return false, err
	}
	n, err := s.q.SetProjectSetupThread(ctx, db.SetProjectSetupThreadParams{ID: pid, ThreadID: tid})
	if err != nil {
		return false, fmt.Errorf("record the setup topic of project %s: %w", projectID, err)
	}
	return n == 1, nil
}

// CleanWorkspaceSteps checks steps written down for new worktrees: every
// path to copy relative to the checkout and inside it, and trimmed; the
// command trimmed. Blank paths are dropped.
func CleanWorkspaceSteps(steps WorkspaceSteps) (WorkspaceSteps, error) {
	out := WorkspaceSteps{Copy: []string{}, Run: strings.TrimSpace(steps.Run)}
	for _, p := range steps.Copy {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clean := strings.TrimSuffix(strings.ReplaceAll(p, "\\", "/"), "/")
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return WorkspaceSteps{}, Invalid("copyOutside", Params{"path": p}, "%q is not a path inside the checkout", p)
		}
		out.Copy = append(out.Copy, p)
	}
	if len(out.Copy) > maxWorkspaceCopy {
		return WorkspaceSteps{}, Invalid("copyTooMany", Params{"max": fmt.Sprint(maxWorkspaceCopy)}, "at most %d paths to copy", maxWorkspaceCopy)
	}
	if len(out.Run) > maxWorkspaceRun {
		return WorkspaceSteps{}, Invalid("runTooLong", Params{"max": fmt.Sprint(maxWorkspaceRun)}, "the command is longer than %d characters", maxWorkspaceRun)
	}
	return out, nil
}

// Bounds of the steps: what a new worktree needs is a handful of paths and
// a line or two.
const (
	maxWorkspaceCopy = 50
	maxWorkspaceRun  = 2000
)

// SetWorkspaceSteps writes down how new worktrees are got ready, which
// sets the project up; steps waiting for a person to adopt are dropped.
func (s *Store) SetWorkspaceSteps(ctx context.Context, projectID string, steps WorkspaceSteps) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	if err := s.q.SetWorkspaceSteps(ctx, db.SetWorkspaceStepsParams{ID: pid, Copy: nonNil(steps.Copy), Run: steps.Run}); err != nil {
		return fmt.Errorf("write down the worktree steps of project %s: %w", projectID, err)
	}
	return nil
}

// SetWorkspacePending keeps steps for a person to adopt, with the note
// that shows them, replacing any waiting before.
func (s *Store) SetWorkspacePending(ctx context.Context, projectID string, steps WorkspaceSteps, messageID string) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	params := db.SetWorkspacePendingParams{ID: pid}
	if params.Pending, err = json.Marshal(WorkspaceSteps{Copy: nonNil(steps.Copy), Run: steps.Run}); err != nil {
		return err
	}
	if messageID != "" {
		if params.MessageID, err = parseUUID(messageID); err != nil {
			return err
		}
	}
	if err := s.q.SetWorkspacePending(ctx, params); err != nil {
		return fmt.Errorf("keep the worktree steps of project %s for a person: %w", projectID, err)
	}
	return nil
}

// AdoptWorkspacePending makes the steps waiting for a person the
// project's, which sets it up. ErrConflict when nothing waits.
func (s *Store) AdoptWorkspacePending(ctx context.Context, projectID string) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	n, err := s.q.AdoptWorkspacePending(ctx, pid)
	if err != nil {
		return fmt.Errorf("adopt the worktree steps of project %s: %w", projectID, err)
	}
	if n == 0 {
		return nothingPending()
	}
	return nil
}

// DropWorkspacePending turns the steps waiting for a person down.
// ErrConflict when nothing waits.
func (s *Store) DropWorkspacePending(ctx context.Context, projectID string) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	n, err := s.q.DropWorkspacePending(ctx, pid)
	if err != nil {
		return fmt.Errorf("drop the worktree steps of project %s: %w", projectID, err)
	}
	if n == 0 {
		return nothingPending()
	}
	return nil
}

func nothingPending() error {
	return Conflicting("nothingPending", nil, "no worktree steps wait for a person: someone adopted or dropped them already")
}

// MarkProjectInitialized records that the project is set up for its
// members' worktrees, with whatever steps it has.
func (s *Store) MarkProjectInitialized(ctx context.Context, projectID string) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	if err := s.q.MarkProjectInitialized(ctx, pid); err != nil {
		return fmt.Errorf("mark project %s set up: %w", projectID, err)
	}
	return nil
}

// SetMemberWorkspace records the git worktree made for a member, not got
// ready yet.
func (s *Store) SetMemberWorkspace(ctx context.Context, memberID, worktreeDir, workDir, branch string) (Member, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return Member{}, err
	}
	row, err := s.q.SetMemberWorkspace(ctx, db.SetMemberWorkspaceParams{ID: id, WorktreeDir: worktreeDir, WorkDir: workDir, Branch: branch})
	if err != nil {
		return Member{}, fmt.Errorf("record the worktree of member %s: %w", memberID, err)
	}
	return toMember(row), nil
}

// MarkMemberPrepared records that the member's worktree is ready for work.
func (s *Store) MarkMemberPrepared(ctx context.Context, memberID string) (Member, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return Member{}, err
	}
	row, err := s.q.MarkMemberPrepared(ctx, id)
	if err != nil {
		return Member{}, fmt.Errorf("mark the worktree of member %s ready: %w", memberID, err)
	}
	return toMember(row), nil
}

// SetMemberOverlaps records which files of a member's work the chat was
// told another member's work changed too (docs/design.md 5.21), in place
// of those it had.
func (s *Store) SetMemberOverlaps(ctx context.Context, memberID string, files []string) error {
	id, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	if err := s.q.SetMemberOverlaps(ctx, db.SetMemberOverlapsParams{ID: id, Files: nonNil(files)}); err != nil {
		return fmt.Errorf("record the overlaps of member %s: %w", memberID, err)
	}
	return nil
}

// AddMemberOverlaps adds files to those the chat was told overlap another
// member's work, for the other member of a new overlap.
func (s *Store) AddMemberOverlaps(ctx context.Context, memberID string, files []string) error {
	id, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	if err := s.q.AddMemberOverlaps(ctx, db.AddMemberOverlapsParams{ID: id, Files: nonNil(files)}); err != nil {
		return fmt.Errorf("record the overlaps of member %s: %w", memberID, err)
	}
	return nil
}

// ClearMemberWorkspace forgets a member's worktree, and the overlaps of
// the work that was in it.
func (s *Store) ClearMemberWorkspace(ctx context.Context, memberID string) error {
	id, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	if err := s.q.ClearMemberWorkspace(ctx, id); err != nil {
		return fmt.Errorf("forget the worktree of member %s: %w", memberID, err)
	}
	return nil
}
