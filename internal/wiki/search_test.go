package wiki

import (
	"strings"
	"testing"
)

// A search result's snippet reads as prose: the marks markdown writes a
// page with are for the page, not for a line under a result.
func TestSnippet_ReadsAsProse(t *testing.T) {
	body := strings.Join([]string{
		"# 标签一律小写",
		"",
		"## 决定",
		"笔记的标签在**存入时**统一转为小写（`strings.ToLower`），见 [代码地图](/modules/code-map.md)[^1]。",
		"",
		"- `notes add --tag Work` → 存为 `work`",
		"",
		"| 方法 | 作用 |",
		"|---|---|",
		"| `Open(path)` | 读 JSON |",
		"",
		"```bash",
		"git add main.go",
		"```",
	}, "\n")
	for _, tc := range []struct {
		word string
		want string
	}{
		// Around the word, a Latin word at either end whole, and not
		// starting with the stop of the sentence before.
		{"存入时", "…签一律小写 决定 笔记的标签在存入时统一转为小写（strings.ToLower），见 代码地图。 notes add --tag Work…"},
		{"方法", "…notes add --tag Work → 存为 work 方法 作用 Open(path) 读 JSON git add main.go"},
		// Only in a link's target: nothing to show.
		{"code-map", ""},
	} {
		got := snippet(body, tc.word)
		if got != tc.want {
			t.Errorf("snippet of %q:\n got %q\nwant %q", tc.word, got, tc.want)
		}
		for _, mark := range []string{"#", "**", "`", "](", "|", "---", "[^"} {
			if strings.Contains(got, mark) {
				t.Errorf("snippet of %q keeps %q: %q", tc.word, mark, got)
			}
		}
	}
}

// A result shows its title above its snippet: the heading a page opens
// with, saying the title again, is not in the snippet, as it is not above
// the page's text.
func TestSearch_SnippetLeavesTheTitleOut(t *testing.T) {
	b := openTest(t, Options{Layout: ProjectLayout})
	w := writer(t, b, "codex/default")
	graphPage(t, w, "/decisions/tags.md", "Decision", "标签一律小写", "# 标签一律小写\n\n## 决定\n笔记的标签在存入时统一转为小写。")
	hits := b.Search("标签", 5)
	if len(hits) != 1 || hits[0].Snippet != "决定 笔记的标签在存入时统一转为小写。" {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Title != "标签一律小写" {
		t.Errorf("title = %q", hits[0].Title)
	}
}
