package store

import (
	"context"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// BuiltinAgentTemplates are the templates veyloom ships with, so a fresh
// install has something to put in a room. They are seeded by name and never
// overwritten, so users can tune the role cards.
func BuiltinAgentTemplates() []NewAgentTemplate {
	return []NewAgentTemplate{
		{
			Name:             "Claude Architect",
			Engine:           "claude",
			PermissionPreset: PermissionEditWithApproval,
			RoleCard: `You are the architect of this project. Plan before you code, keep changes
small and reviewable, and review other agents' work critically. Reply briefly
in chat and put long content in the repository. When unsure, ask a human
instead of guessing.`,
		},
		{
			Name:             "Codex Implementer",
			Engine:           "codex",
			PermissionPreset: PermissionEditWithApproval,
			RoleCard: `You implement the tasks assigned to you in chat. Follow the project's
conventions, run the relevant tests before reporting, and end each turn with
what changed, what was verified and what is left.`,
		},
		{
			Name:             "Pi Tester",
			Engine:           "pi",
			PermissionPreset: PermissionEditWithApproval,
			RoleCard: `You write and run tests, reproduce reported bugs and fix small issues. Report
failures with the exact command and output so others can act on them.`,
		},
	}
}

// EnsureBuiltinAgentTemplates seeds any builtin template that is missing.
// It runs at every startup and is a no-op once they exist.
func (s *Store) EnsureBuiltinAgentTemplates(ctx context.Context) error {
	for _, t := range BuiltinAgentTemplates() {
		options, err := marshalOptions(t.EngineOptions)
		if err != nil {
			return err
		}
		err = s.q.EnsureBuiltinAgentTemplate(ctx, db.EnsureBuiltinAgentTemplateParams{
			Name:             t.Name,
			Engine:           t.Engine,
			Model:            t.Model,
			RoleCard:         t.RoleCard,
			PermissionPreset: string(t.PermissionPreset),
			EngineOptions:    options,
		})
		if err != nil {
			return fmt.Errorf("seed builtin agent template %q: %w", t.Name, err)
		}
	}
	return nil
}
