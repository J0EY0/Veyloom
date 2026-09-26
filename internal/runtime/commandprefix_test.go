package runtime

import (
	"reflect"
	"strings"
	"testing"
)

// Only one plain command is taken to its words, unwrapped from the shell
// it comes in: anything that could run more is not.
func TestCommandWords(t *testing.T) {
	for command, want := range map[string][]string{
		"/bin/zsh -lc 'go test ./...'":             {"go", "test", "./..."},
		"bash -c \"go vet ./...\"":                 {"go", "vet", "./..."},
		"go test ./... -run 'TestA|TestB'":         {"go", "test", "./...", "-run", "TestA|TestB"},
		`git commit -m "fix: a \"quoted\" word"`:   {"git", "commit", "-m", `fix: a "quoted" word`},
		`echo a\ b`:                                {"echo", "a b"},
		"  go   build  ":                           {"go", "build"},
		"/bin/zsh -lc 'go test ./... -count=1'":    {"go", "test", "./...", "-count=1"},
		"/usr/local/bin/bash -lc 'ls -la *.go'":    {"ls", "-la", "*.go"},
		"a#b":                                      {"a#b"},
		"/bin/zsh -lc 'go test ./... && rm -rf /'": nil,
		"cd /tmp && go test":                       nil,
		"go test; ls":                              nil,
		"go test | tee out":                        nil,
		"go test > out.txt":                        nil,
		"go test 2>&1":                             nil,
		"go test $(rm -rf /)":                      nil,
		"go test `rm -rf /`":                       nil,
		`go test "$HOME"`:                          nil,
		"go test # and more":                       nil,
		"go test\nrm -rf /":                        nil,
		"(go test)":                                nil,
		"go test 'open":                            nil,
		`go test "open`:                            nil,
		`go test \`:                                nil,
		"":                                         nil,
		"/bin/zsh -lc ''":                          nil,
	} {
		got, ok := commandWords(command)
		if ok != (want != nil) || (ok && !reflect.DeepEqual(got, want)) {
			t.Errorf("commandWords(%q) = %q, %v; want %q", command, got, ok, want)
		}
	}
}

func TestHasPrefix(t *testing.T) {
	words := []string{"go", "test", "./..."}
	for prefix, want := range map[string]bool{"go test": true, "go": true, "go test ./...": true, "go vet": false, "go test ./... -v": false, "": false} {
		if got := hasPrefix(words, strings.Fields(prefix)); got != want {
			t.Errorf("hasPrefix(%q) = %v", prefix, got)
		}
	}
}

// A runtime's rules are read as prefixes when they are one: a JSON array
// of words. Claude Code's rules are not.
func TestCommandPrefixes(t *testing.T) {
	got := commandPrefixes([]string{`["go","test"]`, "Bash(go test:*)", "[]", `["make"]`, `{"a":1}`})
	if !reflect.DeepEqual(got, [][]string{{"go", "test"}, {"make"}}) {
		t.Errorf("prefixes = %q", got)
	}
}

// What of an offer can be kept for the member: Claude Code's rules as they
// are, and Codex's words as a JSON array; not a mode, folders or the same
// request again.
func TestSimilarStanding(t *testing.T) {
	offer := &Similar{Rules: []string{"Bash(go test:*)"}, Mode: "acceptEdits", Dirs: []string{"/tmp"}, Same: true, Prefix: []string{"go", "test"}}
	if got := offer.Standing(); !reflect.DeepEqual(got, []string{"Bash(go test:*)", `["go","test"]`}) {
		t.Errorf("standing = %q", got)
	}
	if got := (&Similar{Mode: "acceptEdits", Same: true}).Standing(); len(got) != 0 {
		t.Errorf("nothing to keep, got %q", got)
	}
	var none *Similar
	if none.Standing() != nil {
		t.Error("no offer, nothing to keep")
	}
}
