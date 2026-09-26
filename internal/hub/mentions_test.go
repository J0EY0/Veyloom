package hub

import "testing"

// An @ in an agent's code names no one; one in its prose does.
func TestMentionsName(t *testing.T) {
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
	} {
		if got := mentionsName(c.text, "Tester"); got != c.want {
			t.Errorf("mentionsName(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
