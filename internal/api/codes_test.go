package api

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// Every failure the server names by code has words in both of the web
// client's lists (web/src/i18n): a code without them is told in English.
func TestProblemCodesHaveWords(t *testing.T) {
	named := regexp.MustCompile(`(?:Invalid|Conflicting|Missing)\("(\w+)"|writeCoded\(w, [^,]+, "(\w+)"|Code: "(\w+)"`)
	codes := map[string]string{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range named.FindAllStringSubmatch(string(data), -1) {
			codes[m[1]+m[2]+m[3]] = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// An approval settled already is named by how it was.
	for _, s := range []store.ApprovalStatus{store.ApprovalAllowed, store.ApprovalDenied, store.ApprovalExpired, store.ApprovalCancelled} {
		codes["approval"+strings.ToUpper(string(s[:1]))+string(s[1:])] = "store/approvals.go"
	}
	if len(codes) < 30 {
		t.Fatalf("found only %d codes; the pattern no longer matches how they are written", len(codes))
	}
	for _, list := range []string{"zh-CN.ts", "en.ts"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "i18n", list))
		if err != nil {
			t.Fatal(err)
		}
		for code, where := range codes {
			if !strings.Contains(string(data), "'error."+code+"'") {
				t.Errorf("%s has no words for %s, named in %s", list, code, where)
			}
		}
	}
}
