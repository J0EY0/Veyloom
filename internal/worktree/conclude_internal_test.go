package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		body string
		want bool
	}{
		"conflict":  {"a\n<<<<<<< HEAD\nb\n=======\nc\n>>>>>>> main\n", true},
		"diff3":     {"a\n||||||| base\nb\n", true},
		"bare":      {"a\n>>>>>>>\n", true},
		"crlf":      {"a\r\n<<<<<<< HEAD\r\n", true},
		"heading":   {"Title\n=======\n\ntext\n", false},
		"longer":    {"<<<<<<<< not a marker\n", false},
		"in a line": {"x <<<<<<< HEAD\n", false},
		"binary":    {"\x00<<<<<<< HEAD\n", false},
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := hasConflictMarkers(path); err != nil || got != c.want {
			t.Errorf("%s: %v %v, want %v", name, got, err, c.want)
		}
	}
	if got, err := hasConflictMarkers(filepath.Join(dir, "missing")); err != nil || got {
		t.Errorf("a file not there: %v %v", got, err)
	}
}
