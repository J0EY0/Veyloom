package hub

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	veyloom "github.com/J0EY0/veyloom"
	"github.com/J0EY0/veyloom/internal/store"
)

// Every agent's turns have Veyloom's own skills, apart from those installed
// for it from the library; the brief names them apart, the standing
// instructions point to the practices, and the library takes no skill of
// the same name (docs/design.md 5.23.6).
func TestLoop_EveryAgentHasVeyloomsOwnSkills(t *testing.T) {
	sub, err := fs.Sub(veyloom.Skills, "skills")
	if err != nil {
		t.Fatal(err)
	}
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithSkills(sub))
	l.addSkill("go-testing", "Use when writing Go tests.", "Write the cases as a table.", "")
	coder := l.member("Coder", map[string]any{"reply": "Done."})
	l.install("go-testing", coder)
	l.say("@Coder go", "", coder)
	turn := l.waitTurns(1, store.TurnDone, "Coder's turn")[0]

	spec := specOf(t, turn)
	if !slices.Contains(spec.Skills.BuiltinNames(), teamPractices) || !slices.Equal(spec.Skills.LibraryNames(), []string{"go-testing"}) {
		t.Errorf("skills: builtin %v, installed %v", spec.Skills.BuiltinNames(), spec.Skills.LibraryNames())
	}
	for _, want := range []string{"Veyloom's own skills, which every agent has: team-practices.", "Installed for you from the skill library: go-testing."} {
		if !strings.Contains(spec.Prompt, want) {
			t.Errorf("the brief does not say %q:\n%s", want, spec.Prompt)
		}
	}
	if !strings.Contains(spec.SystemPrompt, "Veyloom's skill team-practices, which every agent has") {
		t.Errorf("the standing instructions point to the practices:\n%s", spec.SystemPrompt)
	}
	if got := l.h.BuiltinSkills(); len(got) == 0 || !slices.ContainsFunc(got, func(s BuiltinSkill) bool { return s.Name == teamPractices && s.Description != "" }) {
		t.Errorf("listed: %+v", got)
	}

	dir := filepath.Join(t.TempDir(), teamPractices)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: team-practices\ndescription: Ours.\n---\n\nOurs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var problem *store.Problem
	if _, err := l.h.ImportSkill(l.ctx, dir, "", l.user.ID); !errors.As(err, &problem) || problem.Code != "skillBuiltin" {
		t.Errorf("a skill of the library with the name of Veyloom's own: %v", err)
	}
}
