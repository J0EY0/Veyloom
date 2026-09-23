package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/wiki"
)

// runWiki runs `veyloom wiki ...` with a state dir of its own, returning
// what it wrote and whether it failed.
func runWiki(t *testing.T, state string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VEYLOOM_STATE_DIR", state)
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--env-file=", "wiki"}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestWikiCheck(t *testing.T) {
	state := t.TempDir()
	if out, err := runWiki(t, state, "check"); err != nil || !strings.Contains(out, "no wikis in") {
		t.Fatalf("no wikis yet: %v\n%s", err, out)
	}

	// The wikis this Veyloom keeps are held to its own rules.
	dir := filepath.Join(state, "wiki", "projects", "billing")
	if _, err := wiki.Open(context.Background(), dir, wiki.Options{Layout: wiki.ProjectLayout, Git: true}); err != nil {
		t.Fatal(err)
	}
	out, err := runWiki(t, state, "check")
	if err != nil || !strings.Contains(out, dir+": ") || !strings.Contains(out, "no problems") {
		t.Fatalf("a fresh project wiki: %v\n%s", err, out)
	}

	// A bundle from elsewhere: OKF's own sample passes, a broken one fails
	// with its problems listed.
	if out, err := runWiki(t, state, "check", filepath.Join("..", "..", "internal", "wiki", "okf", "testdata", "acme_retail")); err != nil || !strings.Contains(out, "no problems") {
		t.Errorf("the sample: %v\n%s", err, out)
	}
	broken := t.TempDir()
	os.MkdirAll(filepath.Join(broken, "facts"), 0o755)
	os.WriteFile(filepath.Join(broken, "facts", "x.md"), []byte("no frontmatter\n"), 0o644)
	out, err = runWiki(t, state, "check", broken)
	if err == nil || !strings.Contains(out, "1 file, 1 problem") || !strings.Contains(out, "/facts/x.md") {
		t.Errorf("a broken bundle: %v\n%s", err, out)
	}
}
