package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func TestCheckDir(t *testing.T) {
	// OKF's own sample conforms, and says nothing of Veyloom's rules.
	sample, err := CheckDir(filepath.Join("okf", "testdata", "acme_retail"), okf.Conformance)
	if err != nil || sample.Files == 0 || len(sample.Problems) != 0 {
		t.Fatalf("the sample: %+v %v", sample, err)
	}

	// What Veyloom writes passes its own rules, git and all.
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/default")
	if _, err := w.Create("/facts/port.md", fact("/facts/port.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(context.Background(), "Turn 1"); err != nil {
		t.Fatal(err)
	}
	own, err := CheckDir(b.Dir(), okf.Strict)
	if err != nil || len(own.Problems) != 0 || own.Files < 3 {
		t.Fatalf("a bundle Veyloom wrote: %+v %v", own, err)
	}

	// A file written by hand without frontmatter, and a concept that only
	// Veyloom's rules turn down: a relative link.
	os.WriteFile(filepath.Join(b.Dir(), "facts", "bare.md"), []byte("no frontmatter\n"), 0o644)
	os.WriteFile(filepath.Join(b.Dir(), "facts", "relative.md"), []byte("---\ntype: Fact\ntitle: Relative\n---\n\nSee [the port](port.md).\n"), 0o644)
	loose, _ := CheckDir(b.Dir(), okf.Conformance)
	strict, _ := CheckDir(b.Dir(), okf.Strict)
	if len(loose.Problems) != 1 || loose.Problems[0].Path != "/facts/bare.md" {
		t.Errorf("OKF alone: %+v", loose.Problems)
	}
	if len(strict.Problems) <= len(loose.Problems) {
		t.Errorf("Veyloom's rules find more: %+v", strict.Problems)
	}

	if _, err := CheckDir(filepath.Join(b.Dir(), "facts", "port.md"), okf.Strict); err == nil {
		t.Error("a file is no bundle")
	}
}
