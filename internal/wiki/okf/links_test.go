package okf

import (
	"slices"
	"testing"
)

func TestLinks_FindsLinksButNotCode(t *testing.T) {
	body := "See [the orders table](/tables/orders.md) and [a neighbour](./other.md \"title\").\n" +
		"An image ![chart](/assets/chart.png) and [![badge](b.svg)](/badges.md).\n" +
		"Escaped \\[not a link](/no.md), a footnote[^src] and `[code](/code.md)`.\n" +
		"```go\n// [fenced](/fenced.md)\n```\n" +
		"[angle](<a b.md>) [paren](/x_(y).md) [ext](https://example.com/a.md)\n" +
		"\n[ref]: /refs/one.md\n[^src]: a footnote, not a link\n"
	var got []string
	for _, l := range Links(body) {
		if body[l.Start:l.End] != l.Target {
			t.Errorf("offsets of %q point at %q", l.Target, body[l.Start:l.End])
		}
		got = append(got, l.Target)
	}
	want := []string{"/tables/orders.md", "./other.md", "/assets/chart.png", "/badges.md", "a b.md", "/x_(y).md", "https://example.com/a.md", "/refs/one.md"}
	if !slices.Equal(got, want) {
		t.Errorf("links %q\nwant   %q", got, want)
	}
	if images := Links("![x](/a.png)"); len(images) != 1 || !images[0].Image {
		t.Errorf("images should be marked: %+v", images)
	}
}

func TestFootnotes(t *testing.T) {
	cited, defined := Footnotes("A[^a] b[^b] again[^a] `[^code]`\n\n[^a]: one\n   [^b]: two\n")
	if !slices.Equal(cited, []string{"a", "b"}) || !slices.Equal(defined, []string{"a", "b"}) {
		t.Errorf("cited %v defined %v", cited, defined)
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		from, target, want string
		ok                 bool
	}{
		{"/decisions/a.md", "/facts/b.md", "/facts/b.md", true},
		{"/decisions/a.md", "b.md", "/decisions/b.md", true},
		{"/decisions/a.md", "../facts/b.md#why", "/facts/b.md", true},
		{"/a.md", "../../escape.md", "/escape.md", true},
		{"/a.md", "sub%20dir/x.md", "/sub dir/x.md", true},
		{"/a.md", "https://example.com/x.md", "", false},
		{"/a.md", "veyloom://rooms/3/messages/812", "", false},
		{"/a.md", "mailto:someone@example.com", "", false},
		{"/a.md", "#section", "", false},
		{"/a.md", "//cdn.example.com/x", "", false},
	} {
		got, ok := Resolve(tc.from, tc.target)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Resolve(%q, %q) = %q %v, want %q %v", tc.from, tc.target, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRewriteLinks(t *testing.T) {
	body := "[old](/facts/old.md), [again](/facts/old.md#x), `[code](/facts/old.md)` and [other](/facts/other.md)"
	got := RewriteLinks(body, func(target string) (string, bool) {
		if p, ok := Resolve("/topics/t.md", target); ok && p == "/facts/old.md" {
			return "/facts/new.md", true
		}
		return "", false
	})
	want := "[old](/facts/new.md), [again](/facts/new.md), `[code](/facts/old.md)` and [other](/facts/other.md)"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
