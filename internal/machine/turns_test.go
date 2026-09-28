package machine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// connectedMachine starts a machine with the fake runtime and completes the
// handshake, returning the hub's end of the pipe.
func connectedMachine(t *testing.T, cfg Config) (protocol.Conn, context.CancelFunc) {
	t.Helper()
	cfg.Name = "laptop"
	w := New(cfg, NewDiscovery(nil, time.Second), &MemoryIdentity{}, runtime.BuiltinRunners())
	hubEnd, _, cancel := startMachine(t, w)
	recvKind[protocol.Hello](t, hubEnd)
	welcome := protocol.Welcome{MachineID: "w1", HeartbeatInterval: protocol.Duration(time.Hour)}
	if err := hubEnd.Send(context.Background(), welcome); err != nil {
		t.Fatal(err)
	}
	return hubEnd, cancel
}

// collectTurn reads events for turnID until its TurnDone arrives.
func collectTurn(t *testing.T, hubEnd protocol.Conn, turnID string) ([]runtime.Event, protocol.TurnDone) {
	t.Helper()
	var events []runtime.Event
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch m := m.(type) {
		case protocol.TurnEvent:
			if m.TurnID == turnID {
				events = append(events, m.Event)
			}
		case protocol.TurnDone:
			if m.TurnID == turnID {
				return events, m
			}
		}
	}
	t.Fatalf("turn %s did not finish", turnID)
	return nil, protocol.TurnDone{}
}

func startFakeTurn(t *testing.T, hubEnd protocol.Conn, turnID string, options map[string]any) {
	t.Helper()
	req := protocol.StartTurn{TurnID: turnID, Runtime: "fake", Spec: runtime.TurnSpec{Prompt: "hello", Options: options}}
	if err := hubEnd.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestTurn_StreamsEventsAndFinishes(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"reply": "hi there", "tool": true})
	events, done := collectTurn(t, hubEnd, "t1")

	if done.Error != "" || done.Result.Output != "hi there" || done.Result.SessionRef == "" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
	var kinds []runtime.EventKind
	var text strings.Builder
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == runtime.EventText {
			text.WriteString(ev.Text)
		}
	}
	// session, status, tool_call, tool_result, then the two text chunks
	// coalesced into one event by the 50ms default window.
	if len(kinds) != 5 || kinds[0] != runtime.EventSession || kinds[1] != runtime.EventStatus || kinds[4] != runtime.EventText {
		t.Errorf("unexpected event kinds: %v", kinds)
	}
	if text.String() != "hi there" {
		t.Errorf("text = %q", text.String())
	}
}

func TestTurn_NoCoalescingWhenDisabled(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{EventFlushInterval: -1})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"reply": "ab"})
	events, _ := collectTurn(t, hubEnd, "t1")

	textEvents := 0
	for _, ev := range events {
		if ev.Kind == runtime.EventText {
			textEvents++
		}
	}
	if textEvents != 2 {
		t.Errorf("got %d text events, want the fake runtime's 2 chunks unmerged", textEvents)
	}
}

func TestTurn_UnknownRuntimeReportsFailure(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	// A runtime no runner answers to: a builtin one would start its real CLI
	// wherever that is installed.
	req := protocol.StartTurn{TurnID: "t1", Runtime: "nosuch", Spec: runtime.TurnSpec{Prompt: "x"}}
	if err := hubEnd.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	_, done := collectTurn(t, hubEnd, "t1")

	if done.Error == "" || !strings.Contains(done.Error, `"nosuch" has no runner`) {
		t.Errorf("expected an error naming the runtime, got %+v", done)
	}
}

func TestTurn_DuplicateIDIsRefused(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(300)})
	startFakeTurn(t, hubEnd, "t1", nil)

	_, first := collectTurn(t, hubEnd, "t1")
	if first.Error == "" || !strings.Contains(first.Error, "already running") {
		t.Errorf("second start should be refused, got %+v", first)
	}
	_, second := collectTurn(t, hubEnd, "t1")
	if second.Error != "" {
		t.Errorf("original turn should still finish normally, got %+v", second)
	}
}

func TestTurn_Cancel(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(10000)})
	// Give the turn a moment to start before cancelling it.
	time.Sleep(20 * time.Millisecond)
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, done := collectTurn(t, hubEnd, "t1")
	if !done.Cancelled || done.Error == "" {
		t.Errorf("expected a cancelled TurnDone, got %+v", done)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("cancel took too long; the 10s delay should have been cut short")
	}
	// Cancelling again, or an unknown turn, is harmless.
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t404"}); err != nil {
		t.Fatal(err)
	}
}

func TestTurn_RunConcurrently(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	// Each turn takes 200ms; run three. Sequential execution would need
	// 600ms, concurrent well under that.
	for _, id := range []string{"a", "b", "c"} {
		startFakeTurn(t, hubEnd, id, map[string]any{"delay_ms": float64(200), "reply": id})
	}
	start := time.Now()
	got := map[string]string{}
	for len(got) < 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if done, ok := m.(protocol.TurnDone); ok {
			got[done.TurnID] = done.Result.Output
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("three 200ms turns took %v; they should overlap", elapsed)
	}
	for _, id := range []string{"a", "b", "c"} {
		if got[id] != id {
			t.Errorf("turn %s replied %q", id, got[id])
		}
	}
}

func TestTurn_ShutdownCancelsRunningTurns(t *testing.T) {
	hubEnd, cancel := connectedMachine(t, Config{})
	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(10000)})
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	cancel()

	// Run must return promptly rather than wait out the 10s turn. The
	// machine closes its end of the pipe on the way out, which we observe
	// as ErrClosed on the hub's end.
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	for {
		if _, err := hubEnd.Recv(ctx); err != nil {
			break
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Error("shutdown waited for the running turn instead of cancelling it")
	}
}

// awaitApprovalRequest reads messages until the turn asks for approval.
func awaitApprovalRequest(t *testing.T, hubEnd protocol.Conn, turnID string) protocol.ApprovalRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch m := m.(type) {
		case protocol.ApprovalRequest:
			if m.TurnID == turnID {
				return m
			}
		case protocol.TurnDone:
			if m.TurnID == turnID {
				t.Fatalf("turn finished without asking for approval: %+v", m)
			}
		}
	}
	t.Fatal("no approval request arrived")
	return protocol.ApprovalRequest{}
}

func TestTurn_ApprovalRoundTrip(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true, "reply": "built"})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	if req.ApprovalID == "" || req.Tool != "Bash" || req.Input != `{"command":"make test"}` || req.At.IsZero() {
		t.Fatalf("unexpected request: %+v", req)
	}

	decision := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: req.ApprovalID, Decision: runtime.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	events, done := collectTurn(t, hubEnd, "t1")
	if done.Error != "" || done.Result.Output != "built" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
	ran := false
	for _, ev := range events {
		if ev.Kind == runtime.EventToolCall && ev.Tool == "Bash" {
			ran = true
		}
		if ev.Kind == runtime.EventApprovalRequest {
			t.Error("approval requests must not also be forwarded as TurnEvents")
		}
	}
	if !ran {
		t.Error("the allowed command should have run")
	}
}

func TestTurn_ApprovalDenied(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	decision := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: req.ApprovalID, Decision: runtime.Decision{Allow: false, Message: "no"}}
	if err := hubEnd.Send(context.Background(), decision); err != nil {
		t.Fatal(err)
	}

	_, done := collectTurn(t, hubEnd, "t1")
	if done.Result.Output != "Denied: no" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
}

func TestTurn_StrayDecisionsAreHarmless(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	// Unknown turn, then a running turn with an unknown approval id.
	stray := protocol.ApprovalDecision{TurnID: "t404", ApprovalID: "a", Decision: runtime.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), stray); err != nil {
		t.Fatal(err)
	}
	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	wrong := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: "not-" + req.ApprovalID, Decision: runtime.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), wrong); err != nil {
		t.Fatal(err)
	}
	// The real request is still pending; cancelling ends the turn.
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}
	_, done := collectTurn(t, hubEnd, "t1")
	if !done.Cancelled {
		t.Errorf("expected the turn to end by cancellation, got %+v", done)
	}
}

func TestTextBuffer_CoalescesUntilFlush(t *testing.T) {
	buf := newTextBuffer(time.Hour)
	at := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

	if out := buf.add(runtime.Event{Kind: runtime.EventText, Text: "ab", At: at}); out != nil {
		t.Errorf("first chunk should be buffered, got %v", out)
	}
	if out := buf.add(runtime.Event{Kind: runtime.EventText, Text: "cd", At: at.Add(time.Second)}); out != nil {
		t.Errorf("second chunk should be buffered, got %v", out)
	}

	// A non-text event flushes the text ahead of itself.
	out := buf.add(runtime.Event{Kind: runtime.EventStatus, Text: "done"})
	if len(out) != 2 || out[0].Kind != runtime.EventText || out[0].Text != "abcd" || !out[0].At.Equal(at) || out[1].Kind != runtime.EventStatus {
		t.Errorf("unexpected flush: %+v", out)
	}
	if _, ok := buf.take(); ok {
		t.Error("buffer should be empty after a flush")
	}
	if buf.deadline() != nil {
		t.Error("no deadline should be armed while the buffer is empty")
	}
}

// The hub answers some failures by running the turn again under the same
// id, the moment it hears the first attempt is over. The id must be free
// by then, every time.
func TestTurn_SameIDRunsAgainRightAfterItEnds(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	for attempt := range 20 {
		options := map[string]any{"fail": attempt%2 == 0, "reply": "again"}
		startFakeTurn(t, hubEnd, "t1", options)
		_, done := collectTurn(t, hubEnd, "t1")
		if strings.Contains(done.Error, "already running") {
			t.Fatalf("attempt %d: the id was still taken when the turn was reported over: %s", attempt, done.Error)
		}
		if failed := done.Error != ""; failed != (attempt%2 == 0) {
			t.Fatalf("attempt %d: TurnDone = %+v", attempt, done)
		}
	}
}

// recvRoomQuery reads until the machine asks the hub about the room.
func recvRoomQuery(t *testing.T, hubEnd protocol.Conn) protocol.RoomQuery {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if q, ok := m.(protocol.RoomQuery); ok {
			return q
		}
	}
	t.Fatal("the machine never asked about the room")
	return protocol.RoomQuery{}
}

func TestTurn_RoomToolAsksTheHubAndGetsItsAnswer(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"room_tool": runtime.RoomToolReadTopic, "room_topic": 12})
	q := recvRoomQuery(t, hubEnd)
	if q.TurnID != "t1" || q.QueryID == "" || q.Query.Tool != runtime.RoomToolReadTopic || q.Query.Topic != 12 {
		t.Fatalf("query = %+v", q)
	}
	// An answer nobody waits for is dropped without harm; the real one lands.
	for _, res := range []protocol.RoomResult{
		{TurnID: "t1", QueryID: "no-such-query", Text: "stray"},
		{TurnID: "t9", QueryID: q.QueryID, Text: "another turn's"},
		{TurnID: "t1", QueryID: q.QueryID, Text: "Topic #12, oldest first: ..."},
	} {
		if err := hubEnd.Send(context.Background(), res); err != nil {
			t.Fatal(err)
		}
	}
	_, done := collectTurn(t, hubEnd, "t1")
	if done.Error != "" || done.Result.Output != "Topic #12, oldest first: ..." {
		t.Errorf("TurnDone = %+v, want the hub's answer as the reply", done)
	}
}

func TestTurn_RoomToolHearsWhyTheHubHasNoAnswer(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"room_tool": runtime.RoomToolReadTopic, "room_topic": 99})
	q := recvRoomQuery(t, hubEnd)
	if err := hubEnd.Send(context.Background(), protocol.RoomResult{TurnID: "t1", QueryID: q.QueryID, Error: "this chat has no topic #99"}); err != nil {
		t.Fatal(err)
	}
	_, done := collectTurn(t, hubEnd, "t1")
	// The agent reads the reason and goes on; its turn does not fail.
	if done.Error != "" || done.Result.Output != "error: this chat has no topic #99" {
		t.Errorf("TurnDone = %+v", done)
	}
}

func TestTurn_SaysWhichSkillsGoByAnotherName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()
	own := filepath.Join(work, ".agents", "skills", "release-notes")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("---\nname: release-notes\ndescription: mine\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hubEnd, _ := connectedMachine(t, Config{ToolDir: t.TempDir()})
	skill := func(name string) runtime.Skill {
		return runtime.Skill{Name: name, Files: map[string]string{"SKILL.md": "---\nname: " + name + "\ndescription: the library's\n---\n"}}
	}
	for _, c := range []struct {
		turn, dir string
		want      string
	}{
		{"t1", work, "In this turn the skill library's release-notes goes by veyloom-release-notes, as a skill of your own on this machine has its name."},
		{"t2", t.TempDir(), ""},
	} {
		req := protocol.StartTurn{TurnID: c.turn, Runtime: "fake", Spec: runtime.TurnSpec{
			Prompt: "hello", WorkDir: c.dir,
			Skills: &runtime.SkillSet{Hash: "0123456789abcdef", Skills: []runtime.Skill{skill("release-notes"), skill("go-table-tests")}},
		}}
		if err := hubEnd.Send(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		events, _ := collectTurn(t, hubEnd, c.turn)
		var notices []string
		for _, ev := range events {
			if ev.Kind == runtime.EventNotice {
				notices = append(notices, ev.Text)
			}
		}
		if c.want == "" && len(notices) != 0 || c.want != "" && (len(notices) != 1 || notices[0] != c.want) {
			t.Errorf("%s: notices %q, want %q", c.turn, notices, c.want)
		}
	}
}

func TestRenamedSkills(t *testing.T) {
	if got := renamedSkills(nil); got != "" {
		t.Errorf("none renamed: %q", got)
	}
	got := renamedSkills(map[string]string{"b": "veyloom-b", "a": "veyloom-a"})
	if want := "In this turn the skill library's a goes by veyloom-a and b goes by veyloom-b, as skills of your own on this machine have their names."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// nextTurnEvent reads until the next event of turnID arrives.
func nextTurnEvent(t *testing.T, hubEnd protocol.Conn, turnID string) runtime.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		m, err := hubEnd.Recv(ctx)
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if ev, ok := m.(protocol.TurnEvent); ok && ev.TurnID == turnID {
			return ev.Event
		}
	}
}

// What the hub passes a running turn reaches its runtime, whose events say
// it was taken in; a turn not running here reports it dropped at once.
func TestTurn_SteerReachesTheRunningTurn(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(300)})
	nextTurnEvent(t, hubEnd, "t1")
	if err := hubEnd.Send(context.Background(), protocol.SteerTurn{TurnID: "t1", SteerID: "s1", Text: "New in topic #3:\n>> [Alice] use blue"}); err != nil {
		t.Fatal(err)
	}
	events, done := collectTurn(t, hubEnd, "t1")
	took := false
	for _, ev := range events {
		took = took || ev.Kind == runtime.EventSteer && ev.SteerID == "s1"
	}
	if !took || done.Error != "" || !strings.HasSuffix(done.Result.Output, "Steered: >> [Alice] use blue") {
		t.Errorf("steer taken %v, done %+v", took, done)
	}

	if err := hubEnd.Send(context.Background(), protocol.SteerTurn{TurnID: "t404", SteerID: "s2", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if ev := nextTurnEvent(t, hubEnd, "t404"); ev.Kind != runtime.EventSteerDropped || ev.SteerID != "s2" || ev.At.IsZero() {
		t.Errorf("a turn not running here: %+v", ev)
	}
}

// The texts passed to a turn reach it in the order the hub sent them: the
// hub reads the topic for each on from where the one before stopped.
func TestTurn_SteersReachTheTurnInOrder(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(500)})
	nextTurnEvent(t, hubEnd, "t1")
	var want []string
	for i := range 20 {
		id := fmt.Sprintf("s%d", i)
		want = append(want, id)
		if err := hubEnd.Send(context.Background(), protocol.SteerTurn{TurnID: "t1", SteerID: id, Text: "more " + id}); err != nil {
			t.Fatal(err)
		}
	}
	events, done := collectTurn(t, hubEnd, "t1")
	var got []string
	for _, ev := range events {
		if ev.Kind == runtime.EventSteer {
			got = append(got, ev.SteerID)
		}
	}
	if !slices.Equal(got, want) || done.Error != "" {
		t.Errorf("taken in the order %v, want %v (done %+v)", got, want, done)
	}
}

// steerRecorder is a turn that takes each text at once, noting its id.
type steerRecorder struct {
	mu  sync.Mutex
	ids []string
}

func (s *steerRecorder) Events() <-chan runtime.Event          { return nil }
func (s *steerRecorder) Result() (runtime.Result, error)       { return runtime.Result{}, nil }
func (s *steerRecorder) Cancel()                               {}
func (s *steerRecorder) Answer(string, runtime.Decision) error { return nil }
func (s *steerRecorder) Steer(id, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = append(s.ids, id)
	return nil
}

// Each text for a turn reaches it once, in order, wherever the next falls
// against the pass of the ones before: one that comes as the line empties
// starts a pass of its own, and no two passes take the same text.
func TestTurn_EachSteerIsPassedOnce(t *testing.T) {
	r := newTurnRunner(nil, nil, 0, "")
	turn := &steerRecorder{}
	r.active["t1"] = turn
	want := make([]string, 0, 5000)
	for i := range cap(want) {
		id := fmt.Sprintf("s%d", i)
		want = append(want, id)
		r.steer(context.Background(), protocol.SteerTurn{TurnID: "t1", SteerID: id, Text: id})
	}
	r.wg.Wait()
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if !slices.Equal(turn.ids, want) {
		i := 0
		for i < len(turn.ids) && i < len(want) && turn.ids[i] == want[i] {
			i++
		}
		t.Errorf("%d texts taken for %d sent, first astray at %d: %v", len(turn.ids), len(want), i, turn.ids[i:min(i+5, len(turn.ids))])
	}
}

func TestInWorkDir(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "main.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, dir, path, want string
	}{
		{"inside", real, filepath.Join(real, "main.go"), "main.go"},
		{"deeper inside", real, filepath.Join(real, "internal", "store.go"), "internal/store.go"},
		{"relative", real, "./main.go", "main.go"},
		{"relative, outside", real, "../other/main.go", "../other/main.go"},
		{"outside", real, "/elsewhere/main.go", "/elsewhere/main.go"},
		{"the folder itself", real, real, real},
		{"no folder", "", filepath.Join(real, "main.go"), filepath.Join(real, "main.go")},
		{"folder through a link", link, filepath.Join(real, "main.go"), "main.go"},
		{"file through a link", real, filepath.Join(link, "main.go"), "main.go"},
		{"deleted, through a link", link, filepath.Join(real, "gone.go"), "gone.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inWorkDir(c.dir, c.path); got != c.want {
				t.Errorf("inWorkDir(%q, %q) = %q, want %q", c.dir, c.path, got, c.want)
			}
		})
	}
}

func TestTurn_NamesChangedFilesWhereItWorks(t *testing.T) {
	hubEnd, _ := connectedMachine(t, Config{})
	dir := t.TempDir()
	req := protocol.StartTurn{TurnID: "t1", Runtime: "fake", Spec: runtime.TurnSpec{
		Prompt: "hello", WorkDir: dir,
		Options: map[string]any{"changes": []any{filepath.Join(dir, "main.go"), "/elsewhere/notes.txt"}},
	}}
	if err := hubEnd.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	events, done := collectTurn(t, hubEnd, "t1")
	if done.Error != "" {
		t.Fatalf("turn failed: %s", done.Error)
	}
	var files []string
	for _, ev := range events {
		if ev.Kind == runtime.EventFileChanged {
			files = append(files, ev.Path)
		}
	}
	if want := []string{"main.go", "/elsewhere/notes.txt"}; !slices.Equal(files, want) {
		t.Errorf("files changed = %q, want %q", files, want)
	}
}
