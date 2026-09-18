package hub

import "testing"

func TestJoinMention(t *testing.T) {
	cases := map[string]string{
		"all done":                  "@alice all done",
		"## 结论\n\n- 一":              "@alice\n\n## 结论\n\n- 一",
		"- 补了两个用例":                  "@alice\n\n- 补了两个用例",
		"1. first":                  "@alice\n\n1. first",
		"```go\nfmt.Println()\n```": "@alice\n\n```go\nfmt.Println()\n```",
		"> 引用":                      "@alice\n\n> 引用",
		"#hashtag first":            "@alice #hashtag first",
	}
	for body, want := range cases {
		if got := joinMention("alice", body); got != want {
			t.Errorf("joinMention(%q) = %q, want %q", body, got, want)
		}
	}
}
