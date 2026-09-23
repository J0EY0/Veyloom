package okf

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse_KeepsWhatItDoesNotKnow(t *testing.T) {
	src := "---\n" +
		"type: Metric\n" +
		"title: Gross Margin\n" +
		"# why the legacy term is wrong\n" +
		"not:\n" +
		"- term: revenue minus product cost only\n" +
		"  why: legacy\n" +
		"stale_after: 2026-12-31T00:00:00Z\n" +
		"---\n" +
		"\n# Definition\n\nBody.\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Bytes(); string(got) != src {
		t.Errorf("an unchanged document should render as read:\n%s", got)
	}
	if d.Type() != "Metric" || d.Title() != "Gross Margin" || d.Body() != "# Definition\n\nBody.\n" {
		t.Errorf("type %q title %q body %q", d.Type(), d.Title(), d.Body())
	}
	if want := []string{"type", "title", "not", "stale_after"}; !slices.Equal(d.Keys(), want) {
		t.Errorf("keys %v, want %v", d.Keys(), want)
	}

	d.SetStatus(Deprecated)
	got, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type: Metric\ntitle: Gross Margin\nstatus: deprecated\n# why the legacy term is wrong\nnot:\n",
		"why: legacy",
		"stale_after: 2026-12-31T00:00:00Z\n---\n\n# Definition",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("rewritten file should contain %q:\n%s", want, got)
		}
	}
	again, err := Parse(got)
	if err != nil || again.Status() != Deprecated || again.Metadata() != nil {
		t.Fatalf("reparse: %v %v", again, err)
	}
}

func TestNew_WritesTheCanonicalShape(t *testing.T) {
	at := time.Date(2026, 9, 19, 7, 12, 0, 0, time.UTC)
	d := New("Decision")
	d.SetBody("# 结论\n\n用 json 原样保存。[^m812]\n\n[^m812]: 话题 #41 里的讨论")
	d.AddSource(Source{ID: "m812", Resource: "veyloom://rooms/3/messages/812", Title: "话题 #41 里的讨论"})
	d.SetGenerated(Stamp{By: "claude-code/claude-sonnet-5", At: at})
	d.SetTags([]string{"store", "approvals"})
	d.SetString(KeyDescription, "jsonb 会重排键，表单字段要按服务端给的顺序显示。")
	d.SetString(KeyTitle, "审批的 payload 用 json 不用 jsonb")
	d.AddVerified(Stamp{By: Human("owner"), At: at.Add(18 * time.Minute)})
	d.SetStatus(Stable)
	got, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := `---
type: Decision
title: 审批的 payload 用 json 不用 jsonb
description: jsonb 会重排键，表单字段要按服务端给的顺序显示。
tags: [store, approvals]
status: stable
generated:
  by: claude-code/claude-sonnet-5
  at: 2026-09-19T07:12:00Z
verified:
  - by: human:owner
    at: 2026-09-19T07:30:00Z
sources:
  - id: m812
    resource: veyloom://rooms/3/messages/812
    title: '话题 #41 里的讨论'
---

# 结论

用 json 原样保存。[^m812]

[^m812]: 话题 #41 里的讨论
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if problems := CheckConcept("/decisions/approvals-payload-json.md", mustParse(t, got), Strict); len(problems) > 0 {
		t.Errorf("a document built this way should pass the strict check: %v", problems)
	}
}

func TestParse_RejectsWhatIsNotAConcept(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      error
	}{
		{"no frontmatter", "# Just markdown\n", ErrNoFrontmatter},
		{"empty file", "", ErrNoFrontmatter},
		{"not closed", "---\ntype: Metric\n", ErrBadFrontmatter},
		{"not a mapping", "---\n- a\n- b\n---\n", ErrBadFrontmatter},
		{"bad yaml", "---\ntype: [unclosed\n---\n", ErrBadFrontmatter},
	} {
		if _, err := Parse([]byte(tc.src)); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	d, err := Parse([]byte("---\n---\nbody\n"))
	if err != nil || len(d.Keys()) != 0 || d.Body() != "body\n" {
		t.Errorf("empty frontmatter should parse to no keys: %v %v", d, err)
	}
}

func TestParse_ByteOrderMarkAndCRLF(t *testing.T) {
	src := "\xEF\xBB\xBF---\r\ntype: Fact\r\n---\r\n\r\nLine one.\r\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if d.Type() != "Fact" || d.Body() != "Line one.\r\n" {
		t.Errorf("type %q body %q", d.Type(), d.Body())
	}
	if got, _ := d.Bytes(); string(got) != src {
		t.Errorf("unchanged document should keep its bytes, got %q", got)
	}
}

func TestDocument_CloneIsIndependent(t *testing.T) {
	d := New("Fact")
	d.SetTags([]string{"a"})
	c := d.Clone()
	c.SetTags([]string{"b"})
	c.AddVerified(Stamp{By: Human("owner"), At: time.Now()})
	if !slices.Equal(d.Tags(), []string{"a"}) || d.Has(KeyVerified) {
		t.Errorf("changing the clone changed the original: %v", d.Keys())
	}
	if !d.Delete(KeyTags) || d.Delete(KeyTags) || d.Has(KeyTags) {
		t.Error("delete should remove the key once")
	}
}

func TestParse_OfficialSamplesRoundTrip(t *testing.T) {
	root := filepath.Join("testdata", "acme_retail")
	n := 0
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || filepath.Ext(p) != ".md" || e.Name() == IndexFile || e.Name() == LogFile {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		d, err := Parse(data)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		n++
		// Change a field and write it out: every key, known or not, and the
		// body must come back.
		keys := d.Keys()
		d.SetString(KeyTitle, d.Title()+" (copy)")
		out, err := d.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		back := mustParse(t, out)
		if !slices.Equal(back.Keys(), keys) && !slices.Equal(back.Keys(), append(slices.Clone(keys), KeyTitle)) {
			t.Errorf("%s: keys %v became %v", p, keys, back.Keys())
		}
		if back.Body() != d.Body() || back.Type() != d.Type() || !bytes.Contains(out, []byte("(copy)")) {
			t.Errorf("%s: round trip lost content:\n%s", p, out)
		}
		return nil
	})
	if err != nil || n < 9 {
		t.Fatalf("walked %d samples: %v", n, err)
	}
}

func mustParse(t *testing.T, data []byte) *Document {
	t.Helper()
	d, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, data)
	}
	return d
}
