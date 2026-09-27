package hub

import (
	"maps"
	"slices"
	"testing"
)

// An @ in an agent's code names no one; one in its prose does.
func TestNamedAtLeavesCodeOut(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"@Tester please run it", true},
		{"ask `@Tester` to run it", false},
		{"run ``@Tester`` then @Coder", false},
		{"```\n@Tester run\n```\nthen tell me", false},
		{"~~~go\n// @Tester\n~~~\n@Tester go", true},
		{"```\nunclosed @Tester", false},
		{"`code` and @Tester", true},
		{"@Testers", true},
		{"@Tester请跑一下", true},
	} {
		if got := namedAt(prose(c.text), []string{"Tester", "Coder"})["Tester"]; got != c.want {
			t.Errorf("namedAt(%q) names Tester: %v, want %v", c.text, got, c.want)
		}
	}
}

// At each @ the longest name that follows is the one meant, so a name that
// begins another's is not named by an @ of the other.
func TestNamedAtTakesTheLongestName(t *testing.T) {
	names := []string{"Coder", "Coder2", "Codex Implementer", "alice"}
	for _, c := range []struct {
		text  string
		names []string
		want  []string
	}{
		{"@Coder2 看一下", names, []string{"Coder2"}},
		{"@Coder 和 @Coder2", names, []string{"Coder", "Coder2"}},
		{"@Coder2@Coder", names, []string{"Coder", "Coder2"}},
		{"@Codex Implementer 跑一遍", names, []string{"Codex Implementer"}},
		{"@alice, @Coder2 done", names, []string{"Coder2", "alice"}},
		// With no Coder2 about, "@Coder2" can only mean Coder.
		{"@Coder2 看一下", []string{"Coder"}, []string{"Coder"}},
		{"no one here, mail@example.com", names, nil},
		{"@", names, nil},
	} {
		got := slices.Sorted(maps.Keys(namedAt(c.text, c.names)))
		if !slices.Equal(got, c.want) {
			t.Errorf("namedAt(%q, %v) = %v, want %v", c.text, c.names, got, c.want)
		}
	}
}
