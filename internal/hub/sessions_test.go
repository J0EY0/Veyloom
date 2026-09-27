package hub

import (
	"testing"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

func TestStaleReason(t *testing.T) {
	open := store.MemberSession{Runtime: "claude", MachineID: "m1", WorkDir: "/repo"}
	cases := []struct {
		name   string
		member store.Member
		agent  store.Agent
		want   store.SessionEndReason
	}{
		{"nothing changed", store.Member{MachineID: "m1", RepoPath: "/repo"}, store.Agent{Runtime: "claude"}, ""},
		{"same directory, written differently", store.Member{MachineID: "m1", RepoPath: "/repo/./sub/.."}, store.Agent{Runtime: "claude"}, ""},
		{"the agent moved to another runtime", store.Member{MachineID: "m1", RepoPath: "/repo"}, store.Agent{Runtime: "pi"}, store.SessionRuntimeChanged},
		{"the member runs on another machine", store.Member{MachineID: "m2", RepoPath: "/repo"}, store.Agent{Runtime: "claude"}, store.SessionMachineChanged},
		{"the checkout moved", store.Member{MachineID: "m1", RepoPath: "/elsewhere"}, store.Agent{Runtime: "claude"}, store.SessionDirChanged},
		{"the path was cleared", store.Member{MachineID: "m1"}, store.Agent{Runtime: "claude"}, store.SessionDirChanged},
		// The runtime is what matters most: another runtime cannot even
		// read the session, wherever it runs.
		{"everything changed", store.Member{MachineID: "m2", RepoPath: "/elsewhere"}, store.Agent{Runtime: "pi"}, store.SessionRuntimeChanged},
	}
	for _, c := range cases {
		if got := staleReason(open, c.member, c.agent, c.member.RepoPath); got != c.want {
			t.Errorf("%s: staleReason = %q, want %q", c.name, got, c.want)
		}
	}
}

// Codex fixes the role card when a session starts: one that changed since
// needs a new session. The others take it with every run.
func TestStaleReason_TheRoleCardARuntimeFixed(t *testing.T) {
	member := store.Member{MachineID: "m1", RepoPath: "/repo"}
	codex := store.Agent{Runtime: "codex", RoleCard: "You review."}
	started := store.MemberSession{Runtime: "codex", MachineID: "m1", WorkDir: "/repo", Ref: "thr-1", RoleCardDigest: roleCardDigest("codex", "You review.")}
	cases := []struct {
		name  string
		open  store.MemberSession
		agent store.Agent
		want  store.SessionEndReason
	}{
		{"the same role card", started, codex, ""},
		{"a new role card", started, store.Agent{Runtime: "codex", RoleCard: "You test."}, store.SessionRoleCardChanged},
		{"the role card cleared", started, store.Agent{Runtime: "codex"}, store.SessionRoleCardChanged},
		// A session that has not started has fixed nothing: its first run
		// gives the runtime the role card as it is then.
		{"not started", store.MemberSession{Runtime: "codex", MachineID: "m1", WorkDir: "/repo"}, store.Agent{Runtime: "codex", RoleCard: "You test."}, ""},
		// Sessions from before the hub kept the role card are left be.
		{"not known", store.MemberSession{Runtime: "codex", MachineID: "m1", WorkDir: "/repo", Ref: "thr-1"}, store.Agent{Runtime: "codex", RoleCard: "You test."}, ""},
		{"moved as well", func() store.MemberSession { s := started; s.WorkDir = "/elsewhere"; return s }(), store.Agent{Runtime: "codex", RoleCard: "You test."}, store.SessionDirChanged},
		{"another runtime", store.MemberSession{Runtime: "claude", MachineID: "m1", WorkDir: "/repo", Ref: "s-1"}, store.Agent{Runtime: "claude", RoleCard: "You test."}, ""},
	}
	for _, c := range cases {
		if got := staleReason(c.open, member, c.agent, member.RepoPath); got != c.want {
			t.Errorf("%s: staleReason = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRoleCardDigest(t *testing.T) {
	if roleCardDigest("claude", "You review.") != "" || roleCardDigest("pi", "") != "" {
		t.Error("a runtime that takes its system prompt with every run fixes nothing")
	}
	review, empty := roleCardDigest("codex", "You review."), roleCardDigest("codex", "")
	if review == "" || empty == "" || review == empty || review != roleCardDigest("codex", "You review.") {
		t.Errorf("digests: %q %q", review, empty)
	}
	// A runtime the hub does not know is taken to fix it.
	if roleCardDigest("new-cli", "You review.") != review {
		t.Error("an unknown runtime")
	}
}

func TestSameDir(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"", "/repo", false},
		{"/repo", "", false},
		{"/repo", "/repo/", true},
		{"/repo/a/..", "/repo", true},
		{"/repo", "/Repo", false},
	}
	for _, c := range cases {
		if got := sameDir(c.a, c.b); got != c.want {
			t.Errorf("sameDir(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSessionSpec(t *testing.T) {
	fresh := sessionSpec(store.MemberSession{ID: "s1"})
	if fresh.Key != "s1" || fresh.Ref != "" || fresh.Resume {
		t.Errorf("a session no runtime has reported yet starts: got %+v", fresh)
	}
	started := sessionSpec(store.MemberSession{ID: "s1", Ref: "thr-9"})
	if started.Key != "s1" || started.Ref != "thr-9" || !started.Resume {
		t.Errorf("a reported session resumes: got %+v", started)
	}
}

func TestRetryFresh_OnlyForAResumeThatFailedBeforeTheAgentActed(t *testing.T) {
	failed := protocol.TurnDone{TurnID: "t1", Error: "boom"}
	cases := []struct {
		name  string
		setup func(at *activeTurn)
		done  protocol.TurnDone
	}{
		{"the turn succeeded", func(*activeTurn) {}, protocol.TurnDone{TurnID: "t1"}},
		{"a person cancelled it", func(*activeTurn) {}, protocol.TurnDone{TurnID: "t1", Error: "cancelled", Cancelled: true}},
		{"it started its session rather than resumed one", func(at *activeTurn) { at.resumed = false }, failed},
		{"the agent had said something", func(at *activeTurn) { at.output.WriteString("so far") }, failed},
		{"the agent had used a tool", func(at *activeTurn) { at.toolActivity = true }, failed},
		{"a segment was already stored", func(at *activeTurn) { at.segments = 1 }, failed},
		{"it has had its second run", func(at *activeTurn) { at.retried = true }, failed},
		{"the hub itself failed it", func(at *activeTurn) { at.noRetry = true }, failed},
		{"it is already over", func(at *activeTurn) { at.closed = true }, failed},
	}
	m := &TurnManager{}
	for _, c := range cases {
		at := &activeTurn{resumed: true, work: make(chan func(), 1)}
		c.setup(at)
		if m.retryFresh(at, c.done) {
			t.Errorf("%s: a second run was started", c.name)
		}
		if len(at.work) != 0 {
			t.Errorf("%s: work was queued", c.name)
		}
	}

	// The one case that does: resumed, failed, nothing said or done.
	at := &activeTurn{resumed: true, work: make(chan func(), 1)}
	if !m.retryFresh(at, failed) || len(at.work) != 1 || !at.retried {
		t.Errorf("a silent failure of a resumed turn should queue a second run: queued %d, retried %v", len(at.work), at.retried)
	}
	// And only once.
	if m.retryFresh(at, failed) {
		t.Error("a turn gets one second run, not two")
	}
}

func TestEndReasonOf(t *testing.T) {
	cases := map[runtime.FailureKind]store.SessionEndReason{
		runtime.FailureSessionNotFound: store.SessionNotFound,
		runtime.FailureContextOverflow: store.SessionContextOverflow,
		"":                             store.SessionResumeFailed,
		"something new":                store.SessionResumeFailed,
	}
	for kind, want := range cases {
		if got := endReasonOf(kind); got != want {
			t.Errorf("endReasonOf(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestSessionEndPhrase_EveryReasonReadsInASentence(t *testing.T) {
	reasons := []store.SessionEndReason{
		store.SessionRuntimeChanged, store.SessionMachineChanged, store.SessionDirChanged, store.SessionRoleCardChanged,
		store.SessionNotFound, store.SessionContextOverflow, store.SessionResumeFailed, store.SessionManual, store.SessionCancelled,
	}
	seen := map[string]store.SessionEndReason{}
	for _, r := range reasons {
		phrase := sessionEndPhrase(r)
		if phrase == "" {
			t.Errorf("%s has no phrase", r)
		}
		if other, dup := seen[phrase]; dup {
			t.Errorf("%s and %s read the same: %q", r, other, phrase)
		}
		seen[phrase] = r
	}
}
