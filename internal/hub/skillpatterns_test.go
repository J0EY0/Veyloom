package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// Patterns and the skills they are about: a turn that changes a skill and
// records a pattern about it notes the pattern among the skill's sources;
// whoever reads the skill next is shown the patterns about it, and so is
// its team's maintainer. A pattern written in a turn that left the skill
// alone is shown too, but is not one the skill drew on.
func TestLoop_PatternsAboutSkills(t *testing.T) {
	l, _ := wikiLoop(t)
	home := l.project()
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", home.ID)
	pattern := func(slug, description string) map[string]any {
		return call(runtime.WikiToolWrite, map[string]any{
			"scope": "library", "type": "Pattern", "slug": slug, "title": strings.ReplaceAll(slug, "-", " "), "description": description,
			"body": "Symptom, root cause, the commands, the fix. About [the skill](/skills/go-table-tests/SKILL.md).",
		})
	}
	coder := l.member("Coder", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolPatch, map[string]any{
			"scope": "library", "path": "/skills/go-table-tests/SKILL.md", "reason": "the cases drifted",
			"edits": []any{map[string]any{"op": "append", "content": "Name each case."}},
		}),
		pattern("unnamed-cases-drift", "Unnamed table cases drift apart because failures cannot be told apart; name each case."),
	}})
	l.install("go-table-tests", coder)
	l.say("@Coder write the tests", "", coder)
	l.settled(1, "Coder's turn")
	page, err := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests"))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sources) != 1 || page.Sources[0].Resource != "/patterns/unnamed-cases-drift.md" {
		t.Errorf("the pattern is among the skill's sources: %+v", page.Sources)
	}

	// A pattern about the skill, the skill itself left alone.
	scribe := l.member("Scribe", map[string]any{"tool_calls": []any{
		pattern("slow-table-setup", "Table tests run slowly when each case builds its own fixture; share one."),
	}})
	l.say("@Scribe note it", "", scribe)
	l.settled(2, "Scribe's turn")
	page, _ = l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests"))
	if len(page.Sources) != 1 {
		t.Errorf("a pattern the skill did not draw on is not its source: %+v", page.Sources)
	}

	// Whoever reads the skill next sees both, newest first.
	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolRead, map[string]any{"scope": "library", "path": "/skills/go-table-tests/SKILL.md"}),
	}})
	asked := l.say("@Reader read the skill", "", reader)
	l.settled(3, "Reader's turn")
	answer := l.root(l.topic(asked)).Body
	slow, unnamed := strings.Index(answer, "/patterns/slow-table-setup.md"), strings.Index(answer, "/patterns/unnamed-cases-drift.md: unnamed cases drift: Unnamed table cases drift apart")
	if !strings.Contains(answer, "patterns about this skill") || slow < 0 || unnamed < 0 || slow > unnamed {
		t.Errorf("the patterns shown:\n%s", answer)
	}

	// And its team's maintainer.
	keeper := l.member("Keeper", nil)
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, home.ID); err != nil {
		t.Fatal(err)
	}
	if brief := promptOf(t, l.upkeeps(1)[0]); !strings.Contains(brief, "Patterns about those skills, newest first:\n- go-table-tests: /patterns/slow-table-setup.md") ||
		!strings.Contains(brief, "in the light of the patterns about it listed below") {
		t.Errorf("the upkeep's brief:\n%s", brief)
	}
}
