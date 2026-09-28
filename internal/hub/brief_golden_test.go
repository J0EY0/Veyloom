package hub

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// updateGolden rewrites the golden files under testdata from what the code
// writes now: go test ./internal/hub -run Golden -update.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

// Briefs as agents get them, word for word (docs/design.md 5.23.9): what a
// change of wording does to each kind shows in the diff of its golden
// file, where a test that looks for a phrase or two would pass it by.
func TestBrief_Golden(t *testing.T) {
	cases := []struct {
		name  string
		setup func(st *fakeBriefStore, in *briefInput)
	}{
		// The leader, asked in the room, in a session that has read nothing.
		{"first-turn", func(st *fakeBriefStore, in *briefInput) {
			st.project.LeaderID = "a1"
			asked := user("m1", "@Claude plan the auth refactor")
			asked.Mentions = []store.Mention{{Kind: store.MentionAgent, ID: "a1"}}
			st.messages["m1"] = asked
			st.roomNews = []store.RoomNewsItem{
				{Message: user("m0", "welcome everyone")},
				{Message: asked, TopicNumber: 7},
			}
			st.topicNews = []store.TopicNewsItem{
				{ThreadID: "t3", Number: 3, NewCount: 4, Root: user("m30", "rate limits for the public API"), Last: agent("m34", "a2", "Shipped behind a flag.")},
			}
			in.Triggers = []store.Message{st.messages["m1"]}
			in.BuiltinSkills = []string{teamPractices}
		}},
		// A member on Codex in a worktree of its own: the standing
		// instructions go in the brief, the runtime fixing its system
		// prompt when a session starts.
		{"worker-codex", func(st *fakeBriefStore, in *briefInput) {
			in.Member = st.members[1]
			in.Member.WorktreeDir, in.Member.WorkDir, in.Member.Branch = "/w/codex", "/w/codex", "veyloom/codex"
			in.Dir = in.Member.WorkDir
			in.Runtime = "codex"
			st.project.RepoPath = "/repo"
			st.messages["m1"] = user("m1", "@Codex write the token store")
			in.Triggers = []store.Message{st.messages["m1"]}
			in.Skills = []string{"go-testing"}
		}},
		// A session that read the topic before: only what is new.
		{"continued", func(st *fakeBriefStore, in *briefInput) {
			in.Session = store.MemberSession{ID: "s1", RoomSeen: 60, ThreadSeen: map[string]int64{"t7": 55}}
			st.replies = []store.Message{user("m9", "one more thing: keep the old tokens valid for a day")}
			in.Triggers = st.replies
		}},
		// Woken by another member's message, in its piece of work.
		{"woken", func(st *fakeBriefStore, in *briefInput) {
			in.Member = st.members[1]
			in.Runtime = "pi"
			st.replies = []store.Message{agent("m8", "a1", "@Codex please write the token store; the plan is above.")}
			in.Triggers = st.replies
			in.Relays = &relaysLeft{Limit: 30, Woken: 3}
			// Claude, asked by alice, handed it on itself.
			in.HandedBy = &handedBy{Asked: "Claude", Person: "alice"}
		}},
		// Summing up the work it handed on, now that it is done.
		{"summing-up", func(st *fakeBriefStore, in *briefInput) {
			in.HandedOn = []handedResult{
				{MemberID: "a2", Member: "Codex", Topic: 8, Status: store.TurnDone, Said: "The token store is in, with tests."},
			}
			st.replies = []store.Message{{ID: "m12", SenderKind: store.SenderSystem, Body: "The work Claude handed on is done (Codex); back to Claude."}}
			in.Triggers = st.replies
		}},
		// A person's word to the room that names no member, which came to
		// the leader to take on or hand on (design.md 4.2).
		{"dispatched", func(st *fakeBriefStore, in *briefInput) {
			st.project.LeaderID = "a1"
			st.messages["m1"] = user("m1", "the login page is slow, can someone look into it?")
			st.roomNews = []store.RoomNewsItem{{Message: st.messages["m1"], TopicNumber: 7}}
			in.Triggers = []store.Message{st.messages["m1"]}
		}},
		// A new session after a person cancelled its last turn with one
		// (design.md 5.23.8).
		{"after-cancel", func(st *fakeBriefStore, in *briefInput) {
			in.NewSession = store.SessionCancelled
			in.Stopped = &stoppedTurn{topic: 7, ask: "@Claude run the whole test suite"}
			st.messages["m1"] = user("m1", "@Claude what does the auth module do?")
			in.Triggers = []store.Message{st.messages["m1"]}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, in := newBriefRoom()
			c.setup(st, &in)
			b := build(t, st, in)
			roleCard := st.agents[in.Member.AgentID].RoleCard
			got := "== system prompt ==\n" + systemPrompt(roleCard, b.Standing, in.Runtime) + "\n== brief ==\n" + b.Prompt
			golden(t, filepath.Join("testdata", "briefs", c.name+".golden"), got)
		})
	}
}

// golden compares got with the golden file at path, or writes it there
// under -update.
func golden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test -run Golden -update writes it)", path, err)
	}
	if got != string(want) {
		t.Errorf("%s no longer matches; if the change is meant, rewrite it with -update:\n%s", path, firstDifference(string(want), got))
	}
}

// firstDifference shows where two texts part, line by line, with a line
// of context before.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			before := ""
			if i > 0 && i-1 < len(g) {
				before = fmt.Sprintf("   %d: %s\n", i, g[i-1])
			}
			return fmt.Sprintf("%s - %d: %s\n + %d: %s", before, i+1, wl, i+1, gl)
		}
	}
	return ""
}
