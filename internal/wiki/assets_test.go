package wiki

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestAssetPath(t *testing.T) {
	for _, tc := range []struct{ slug, name, want string }{
		// A markdown file is kept as .markdown: an .md would be a concept.
		{"release-flow", "Release Checklist v2.MD", "/files/release-flow/release-checklist-v2.markdown"},
		{"arch", "架构图.PNG", "/files/arch/file.png"},
		{"arch", "diagram", "/files/arch/diagram"},
		{"arch", "../../etc/passwd", "/files/arch/passwd"},
		{"arch", "weird.ext-with-dash", "/files/arch/weird"},
	} {
		if got := AssetPath(tc.slug, tc.name); got != tc.want {
			t.Errorf("AssetPath(%q, %q) = %q, want %q", tc.slug, tc.name, got, tc.want)
		}
	}
}

// A page keeps a file whole, beside the pages, in the same commit; the
// index, the log and the checks leave it be.
func TestWriter_PutAsset(t *testing.T) {
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "pi/deepseek-v4-flash")
	if _, err := w.Create("/decisions/arch.md", decision("架构", "一张图说清模块边界。", "![diagram](/files/arch/diagram.png)")); err != nil {
		t.Fatal(err)
	}
	p := b.FreeAssetPath("arch", "diagram.png")
	if p != "/files/arch/diagram.png" {
		t.Fatalf("free path %q", p)
	}
	if err := w.PutAsset(p, []byte("PNG")); err != nil {
		t.Fatal(err)
	}
	if again := b.FreeAssetPath("arch", "diagram.png"); again != "/files/arch/diagram-2.png" {
		t.Errorf("the next one of the name goes to %q", again)
	}
	if _, err := w.Commit(context.Background(), "Pi in topic #3"); err != nil {
		t.Fatal(err)
	}
	if data, err := b.Asset(p); err != nil || string(data) != "PNG" {
		t.Fatalf("read back: %q %v", data, err)
	}
	// Committed with the page: nothing is left over.
	if out, err := exec.Command("git", "-C", b.Dir(), "status", "--porcelain").CombinedOutput(); err != nil || len(out) != 0 {
		t.Errorf("left uncommitted: %s %v", out, err)
	}
	if out, _ := exec.Command("git", "-C", b.Dir(), "log", "--name-only", "--format=%s", "-1").CombinedOutput(); !strings.Contains(string(out), "files/arch/diagram.png") {
		t.Errorf("the last commit:\n%s", out)
	}
	if len(b.Pages()) != 1 || strings.Contains(readFile(t, b, "/log.md"), "diagram.png") {
		t.Errorf("a file is no page: %d pages, log:\n%s", len(b.Pages()), readFile(t, b, "/log.md"))
	}
	if h := b.Health(b.opts.Now()); len(h.Broken) != 0 || len(h.Problems) != 0 {
		t.Errorf("the page's link to the file is no broken link: %+v", h)
	}

	for _, bad := range []struct {
		path string
		data []byte
	}{
		{"/decisions/x.png", []byte("x")},
		{"/files/arch/x.md", []byte("x")},
		{"/files/arch/.hidden", []byte("x")},
		{"/files/arch/big.bin", make([]byte, MaxAsset+1)},
	} {
		if err := w.PutAsset(bad.path, bad.data); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("PutAsset(%s): %v", bad.path, err)
		}
	}
	for _, missing := range []string{"/files/arch/none.png", "/decisions/arch.md"} {
		if _, err := b.Asset(missing); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Asset(%s): %v", missing, err)
		}
	}
}
