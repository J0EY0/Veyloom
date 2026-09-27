package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// turnRunner executes turns on this machine's runtimes and streams their
// events to the hub. One runner serves one hub connection; Machine.Run owns
// it and shuts it down when the connection ends.
//
// Turns run concurrently, each in its own goroutine. Events flow runtime →
// pump → conn; the runtime blocks when the pump is slow, so a slow hub
// applies backpressure instead of growing memory.
type turnRunner struct {
	runners map[string]runtime.Runner
	conn    protocol.Conn
	flush   time.Duration
	// skillRoot is where the skills turns are given are written; empty
	// leaves turns without them.
	skillRoot string

	mu     sync.Mutex
	active map[string]runtime.Turn
	// waiting holds the room tool calls the hub has yet to answer, by turn
	// and query id; queries numbers them.
	waiting map[string]chan protocol.RoomResult
	queries atomic.Uint64
	// steers are the texts to pass to each turn, in the order the hub sent
	// them, while one of them is being passed (steer).
	steers map[string][]protocol.SteerTurn
	wg     sync.WaitGroup
}

func newTurnRunner(runners map[string]runtime.Runner, conn protocol.Conn, flush time.Duration, skillRoot string) *turnRunner {
	return &turnRunner{
		runners:   runners,
		conn:      conn,
		flush:     flush,
		skillRoot: skillRoot,
		active:    make(map[string]runtime.Turn),
		waiting:   make(map[string]chan protocol.RoomResult),
		steers:    make(map[string][]protocol.SteerTurn),
	}
}

// start launches a turn. Problems that prevent it from starting, such as
// a runtime this machine cannot run, are reported to the hub as an
// immediate TurnDone with an error rather than returned, because the hub
// is waiting for exactly that message.
func (r *turnRunner) start(ctx context.Context, req protocol.StartTurn) {
	runner, ok := r.runners[req.Runtime]
	if !ok {
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("runtime %q has no runner on this machine", req.Runtime))
		return
	}
	if !r.reserve(req.TurnID) {
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("turn %s is already running", req.TurnID))
		return
	}

	// What the turn may ask of this machine, and through it of the hub.
	req.Spec.Host = &turnHost{runner: r, turnID: req.TurnID}
	// Its git follows the squashed merges of the members' work, as the
	// machine's own does.
	req.Spec.Env = worktree.AgentEnv
	// The skills it is given, where its runtime loads them from. A turn
	// whose skills could not be written goes without, and people are told.
	if req.Spec.Skills != nil && r.skillRoot != "" {
		taken := runtime.UserSkillNames(req.Spec.WorkDir)
		dir, err := runtime.WriteSkills(r.skillRoot, req.Spec.Skills, taken)
		if err != nil {
			_ = r.conn.Send(ctx, outbound(req.TurnID, runtime.Event{
				Kind: runtime.EventNotice, Level: runtime.NoticeWarning, At: time.Now(),
				Text: "The skill library's skills could not be written on this machine, so this turn goes without them: " + err.Error(),
			}))
		} else if renamed := renamedSkills(runtime.SkillAliases(req.Spec.Skills, taken)); renamed != "" {
			_ = r.conn.Send(ctx, outbound(req.TurnID, runtime.Event{Kind: runtime.EventNotice, Level: runtime.NoticeInfo, At: time.Now(), Text: renamed}))
		}
		req.Spec.SkillDir = dir
	}
	turn, err := runner.StartTurn(ctx, req.Spec)
	if err != nil {
		r.release(req.TurnID)
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("start %s turn: %w", req.Runtime, err))
		return
	}
	r.track(req.TurnID, turn)

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.pump(ctx, req.TurnID, turn)
	}()
}

// cancel stops a running turn. Unknown ids are ignored: the turn may have
// finished while the hub's request was in flight.
func (r *turnRunner) cancel(turnID string) {
	if turn := r.lookup(turnID); turn != nil {
		turn.Cancel()
	}
}

// steer passes text to a running turn, its runtime's own way (design.md
// 5.23.2). The turn's events tell what became of it; text the turn cannot
// take, or a turn not running here, is reported dropped at once, and the
// hub puts what the text carried back in the member's queue. A runtime may
// wait on its CLI to take the text, so the loop does not; the texts for a
// turn go to it one after another, in the order they came, as the hub
// reads the topic for each on from where the one before stopped.
func (r *turnRunner) steer(ctx context.Context, req protocol.SteerTurn) {
	r.mu.Lock()
	passing := len(r.steers[req.TurnID]) > 0
	r.steers[req.TurnID] = append(r.steers[req.TurnID], req)
	r.mu.Unlock()
	if passing {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.passSteers(ctx, req.TurnID)
	}()
}

// passSteers passes the texts for turnID in order, until none is left.
func (r *turnRunner) passSteers(ctx context.Context, turnID string) {
	for {
		r.mu.Lock()
		line := r.steers[turnID]
		if len(line) == 0 {
			delete(r.steers, turnID)
			r.mu.Unlock()
			return
		}
		req, turn := line[0], r.active[turnID]
		r.mu.Unlock()
		err := runtime.ErrSteerRefused
		if turn != nil {
			err = turn.Steer(req.SteerID, req.Text)
		}
		if err != nil {
			_ = r.conn.Send(ctx, protocol.TurnEvent{TurnID: turnID, Event: runtime.Event{
				Kind: runtime.EventSteerDropped, SteerID: req.SteerID, Text: err.Error(), At: time.Now(),
			}})
		}
		// Taken off only once passed: one that comes meanwhile waits its
		// turn behind it rather than start a pass of its own.
		r.mu.Lock()
		r.steers[turnID] = r.steers[turnID][1:]
		r.mu.Unlock()
	}
}

// answer passes the hub's decision to the turn that asked. As with cancel,
// a decision for a turn or request that is gone is dropped: the turn ended
// while the person was deciding, and nothing is waiting any more.
func (r *turnRunner) answer(turnID, approvalID string, d runtime.Decision) {
	if turn := r.lookup(turnID); turn != nil {
		_ = turn.Answer(approvalID, d)
	}
}

// lookup returns a running turn, or nil while it is starting or gone.
func (r *turnRunner) lookup(turnID string) runtime.Turn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[turnID]
}

// shutdown cancels every turn and waits for their pumps to finish.
func (r *turnRunner) shutdown() {
	r.mu.Lock()
	for _, turn := range r.active {
		if turn != nil {
			turn.Cancel()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// reserve claims a turn id before the runtime starts, so a duplicate
// StartTurn cannot launch a second runtime process.
func (r *turnRunner) reserve(turnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.active[turnID]; exists {
		return false
	}
	r.active[turnID] = nil
	return true
}

func (r *turnRunner) track(turnID string, turn runtime.Turn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[turnID] = turn
}

func (r *turnRunner) release(turnID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, turnID)
}

// pump forwards a turn's events to the hub, coalescing text, and finishes
// with a TurnDone. If the connection fails mid-turn the turn is cancelled:
// nobody is listening any more.
func (r *turnRunner) pump(ctx context.Context, turnID string, turn runtime.Turn) {
	buf := newTextBuffer(r.flush)
	send := func(ev runtime.Event) bool {
		return r.conn.Send(ctx, outbound(turnID, ev)) == nil
	}

	events := turn.Events()
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				break
			}
			for _, out := range buf.add(ev) {
				if !send(out) {
					r.abandon(turnID, turn)
					return
				}
			}
		case <-buf.deadline():
			if ev, ok := buf.take(); ok && !send(ev) {
				r.abandon(turnID, turn)
				return
			}
		}
	}
	if ev, ok := buf.take(); ok && !send(ev) {
		r.abandon(turnID, turn)
		return
	}

	res, err := turn.Result()
	// The id is given up before the hub hears the turn is over: the hub may
	// answer a failed turn by starting it again under the same id, in a new
	// session, and must not find the id still taken.
	r.release(turnID)
	done := protocol.TurnDone{TurnID: turnID, Result: res}
	if err != nil {
		done.Error = err.Error()
		done.Cancelled = errors.Is(err, runtime.ErrTurnCancelled)
	}
	_ = r.conn.Send(ctx, done)
}

// outbound is the protocol message for one runtime event. Approval requests
// travel as their own kind because the hub must act on them and reply;
// everything else is a fire-and-forget TurnEvent.
func outbound(turnID string, ev runtime.Event) protocol.Message {
	if ev.Kind == runtime.EventApprovalRequest {
		return protocol.ApprovalRequest{
			TurnID: turnID, ApprovalID: ev.ApprovalID, ApprovalKind: ev.ApprovalKind, Tool: ev.Tool, Input: ev.Input, At: ev.At, Similar: ev.Similar,
			Reviewer: ev.Reviewer, Verdict: ev.Verdict, Why: ev.Text, Detail: ev.Detail,
		}
	}
	return protocol.TurnEvent{TurnID: turnID, Event: ev}
}

// abandon stops a turn whose events can no longer be delivered, drains it
// so the runtime goroutine can exit, and gives up its id.
func (r *turnRunner) abandon(turnID string, turn runtime.Turn) {
	turn.Cancel()
	for range turn.Events() {
	}
	r.release(turnID)
}

func (r *turnRunner) reportFailure(ctx context.Context, turnID string, err error) {
	_ = r.conn.Send(ctx, protocol.TurnDone{TurnID: turnID, Error: err.Error()})
}

// textBuffer coalesces consecutive text events into one message per flush
// window. Runtimes stream replies in small chunks; without this every chunk
// would cost a protocol message. Non-text events flush the buffer first so
// ordering is preserved.
type textBuffer struct {
	window time.Duration
	text   strings.Builder
	first  time.Time
	timer  *time.Timer
}

func newTextBuffer(window time.Duration) *textBuffer {
	return &textBuffer{window: window}
}

// add absorbs ev and returns whatever must be sent now.
func (b *textBuffer) add(ev runtime.Event) []runtime.Event {
	if ev.Kind != runtime.EventText || b.window <= 0 {
		out := make([]runtime.Event, 0, 2)
		if pending, ok := b.take(); ok {
			out = append(out, pending)
		}
		return append(out, ev)
	}
	if b.text.Len() == 0 {
		b.first = ev.At
		b.timer = time.NewTimer(b.window)
	}
	b.text.WriteString(ev.Text)
	return nil
}

// deadline is the channel that fires when buffered text has waited long
// enough; nil (never fires) when nothing is buffered.
func (b *textBuffer) deadline() <-chan time.Time {
	if b.timer == nil {
		return nil
	}
	return b.timer.C
}

// take returns the buffered text as one event and resets the buffer.
func (b *textBuffer) take() (runtime.Event, bool) {
	if b.text.Len() == 0 {
		return runtime.Event{}, false
	}
	ev := runtime.Event{Kind: runtime.EventText, At: b.first, Text: b.text.String()}
	b.text.Reset()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	return ev, true
}

// turnHost is what a turn's runtime asks the machine for: the agent's room
// tools end here, and from here go to the hub, which alone knows the room.
type turnHost struct {
	runner *turnRunner
	turnID string
}

// roomQueryTimeout bounds how long a room tool call waits for the hub. The
// agent's CLI waits on it, so it is kept well under the CLIs' own limits
// for a tool call.
const roomQueryTimeout = 30 * time.Second

// QueryRoom implements runtime.TurnHost.
func (h *turnHost) QueryRoom(ctx context.Context, q runtime.RoomQuery) (string, error) {
	return h.runner.queryRoom(ctx, h.turnID, q)
}

// queryRoom sends a room tool call to the hub and waits for its answer.
func (r *turnRunner) queryRoom(ctx context.Context, turnID string, q runtime.RoomQuery) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, roomQueryTimeout)
	defer cancel()

	id := strconv.FormatUint(r.queries.Add(1), 10)
	ch := make(chan protocol.RoomResult, 1)
	key := turnID + "/" + id
	r.mu.Lock()
	r.waiting[key] = ch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.waiting, key)
		r.mu.Unlock()
	}()

	if err := r.conn.Send(ctx, protocol.RoomQuery{TurnID: turnID, QueryID: id, Query: q}); err != nil {
		return "", fmt.Errorf("ask the hub: %w", err)
	}
	select {
	case res := <-ch:
		if res.Error != "" {
			return "", errors.New(res.Error)
		}
		return res.Text, nil
	case <-ctx.Done():
		return "", errors.New("the hub did not answer in time")
	}
}

// roomResult hands the hub's answer to the call waiting for it. An answer
// nobody waits for any more (the call timed out, the turn ended) is dropped.
func (r *turnRunner) roomResult(res protocol.RoomResult) {
	r.mu.Lock()
	ch := r.waiting[res.TurnID+"/"+res.QueryID]
	r.mu.Unlock()
	if ch != nil {
		ch <- res
	}
}

// renamedSkills tells people which of the library's skills go by another
// name in a turn, because skills of their own on this machine have theirs
// (docs/design.md 5.11); empty when none do.
func renamedSkills(aliases map[string]string) string {
	if len(aliases) == 0 {
		return ""
	}
	names := slices.Sorted(maps.Keys(aliases))
	for i, name := range names {
		names[i] = name + " goes by " + aliases[name]
	}
	whose := "as a skill of your own on this machine has its name"
	if len(names) > 1 {
		whose = "as skills of your own on this machine have their names"
	}
	return "In this turn the skill library's " + strings.Join(names, " and ") + ", " + whose + "."
}
