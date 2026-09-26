package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PiConfig tunes the Pi runner. Zero fields take the defaults from
// DefaultPiConfig.
type PiConfig struct {
	// Binary is the executable to run; empty resolves "pi" on PATH at each
	// start.
	Binary string
	// MaxEventBytes caps tool inputs and results copied into events.
	MaxEventBytes int
	// StderrBytes is how much of the CLI's stderr is kept for errors.
	StderrBytes int
	// NoteLines is how many non-JSON stdout lines are kept: pi prints
	// problems such as a missing API key there, in plain text.
	NoteLines int
	// WaitDelay is how long a cancelled CLI gets to exit after SIGTERM
	// before it is killed.
	WaitDelay time.Duration
	// SessionDir is where the runner keeps pi's session files, one per
	// session, named after the hub's key. Empty leaves sessions where pi
	// puts them and resumes by the id pi reported. A file of our own is
	// what makes resuming dependable: pi looks ids up by working directory
	// and, finding one elsewhere, stops to ask whether to fork it.
	SessionDir string
	// ToolDir is where the runner writes the extension that gives pi the
	// room tools (see pi_extension.go). Empty means a directory under the
	// system's temporary one.
	ToolDir string
}

// DefaultPiConfig returns the defaults every PiConfig is completed with.
func DefaultPiConfig() PiConfig {
	return PiConfig{
		MaxEventBytes: 4096,
		StderrBytes:   4096,
		NoteLines:     8,
		WaitDelay:     5 * time.Second,
	}
}

func (c PiConfig) withDefaults() PiConfig {
	def := DefaultPiConfig()
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = def.MaxEventBytes
	}
	if c.StderrBytes <= 0 {
		c.StderrBytes = def.StderrBytes
	}
	if c.NoteLines <= 0 {
		c.NoteLines = def.NoteLines
	}
	if c.WaitDelay <= 0 {
		c.WaitDelay = def.WaitDelay
	}
	return c
}

// piToolSets maps the permission presets onto pi's --tools allowlist. Pi
// has no approval prompts, so edit_with_approval becomes "edit but never
// run commands", and so does auto_review, pi having no reviewer either;
// full_auto leaves the default of every tool enabled.
var piToolSets = map[string][]string{
	PermissionReadOnly:         {"read", "grep", "find", "ls"},
	PermissionEditWithApproval: {"read", "grep", "find", "ls", "edit", "write"},
	PermissionAutoReview:       {"read", "grep", "find", "ls", "edit", "write"},
}

// piEditTools are the tools whose use means a file changed.
var piEditTools = map[string]bool{"edit": true, "write": true}

// PiRunner runs turns on the pi coding agent in RPC mode: commands go in on
// stdin, pi's events and its extensions' requests to people come out on
// stdout, and the answers go back in (docs/design.md 4.6). Options honoured
// from the agent:
//
//	provider   string    passed as --provider
//	thinking   string    passed as --thinking (off, minimal, low, medium, high, xhigh)
//	extra_args []string  appended to the command line verbatim
type PiRunner struct {
	cfg   PiConfig
	tools *toolEndpoint
}

// NewPiRunner creates a runner with cfg.
func NewPiRunner(cfg PiConfig) *PiRunner {
	return &PiRunner{cfg: cfg.withDefaults(), tools: newToolEndpoint()}
}

// Name implements Runner.
func (*PiRunner) Name() string { return "pi" }

// Close releases the tool endpoint. Turns still running lose it.
func (r *PiRunner) Close() error { return r.tools.close() }

// StartTurn implements Runner. The prompt goes in as the RPC prompt
// command, after one asking for the session's id, which pi does not print
// on its own in this mode.
func (r *PiRunner) StartTurn(ctx context.Context, spec TurnSpec) (Turn, error) {
	bin := r.cfg.Binary
	if bin == "" {
		path, err := exec.LookPath("pi")
		if err != nil {
			return nil, fmt.Errorf("pi: %w", err)
		}
		bin = path
	}
	if file := r.sessionFile(spec.Session); file != "" {
		// Given a path with nothing at it pi quietly starts a new session,
		// which for a turn that was told it continues one means an agent
		// that lost its memory without anyone noticing. Better to fail and
		// let the hub start over with the whole story.
		if _, err := os.Stat(file); spec.Session.Resume && errors.Is(err, os.ErrNotExist) {
			return failedTurn(Result{Failure: FailureSessionNotFound}, fmt.Errorf("pi: session file %s is gone", file)), nil
		}
		// pi creates the file, not the directory it goes in.
		if err := os.MkdirAll(r.cfg.SessionDir, 0o700); err != nil {
			return nil, fmt.Errorf("pi: session directory: %w", err)
		}
	}

	// Startup update checks are noise for a turn; PI_OFFLINE turns only
	// those off, not model calls.
	env := append([]string{"PI_OFFLINE=1"}, spec.Env...)
	release := func() {}
	if spec.Host != nil {
		// The room tools: the extension registers them, the endpoint whose
		// URL it is given answers them.
		if _, err := ensurePiExtension(r.toolDir()); err != nil {
			return nil, fmt.Errorf("pi: room tools extension: %w", err)
		}
		ep, err := r.tools.register(spec.Host, spec.ExtraTools, nil)
		if err != nil {
			return nil, fmt.Errorf("pi: tool endpoint: %w", err)
		}
		env = append(env, piRoomURLEnv+"="+ep.Room)
		if len(spec.ExtraTools) > 0 {
			env = append(env, piExtraToolsEnv+"="+strings.Join(spec.ExtraTools, ","))
		}
		release = func() { r.tools.unregister(ep.token) }
	}

	ctx, cancel := context.WithCancel(ctx)
	proc, err := startCLI(ctx, cliOptions{
		Binary:      bin,
		Args:        r.args(spec),
		Dir:         spec.WorkDir,
		Env:         env,
		StdinPipe:   true,
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
	})
	if err != nil {
		cancel()
		release()
		return nil, fmt.Errorf("pi: %w", err)
	}

	t := &piTurn{turnBase: newTurnBase(ctx, cancel), ctx: ctx, cfg: r.cfg, release: release, in: newJSONLines(proc.stdin)}
	go t.run(proc)
	// Written while run reads; should a write fail, pi has gone, and run
	// says why.
	go func() {
		t.in.send(map[string]any{"id": "state", "type": "get_state"})
		t.in.send(map[string]any{"id": "prompt", "type": "prompt", "message": spec.Prompt})
	}()
	return t, nil
}

// toolDir is where the room tools extension is kept.
func (r *PiRunner) toolDir() string {
	if r.cfg.ToolDir != "" {
		return r.cfg.ToolDir
	}
	return filepath.Join(os.TempDir(), "veyloom-tools")
}

// args builds the command line for spec; the prompt is no part of it.
func (r *PiRunner) args(spec TurnSpec) []string {
	args := []string{"--mode", "rpc"}
	if spec.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.SystemPrompt)
	}
	if provider := optString(spec.Options, "provider"); provider != "" {
		args = append(args, "--provider", provider)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if thinking := optString(spec.Options, "thinking"); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	// A path starts the session when the file is missing and continues it
	// when it is there, so first and later turns look the same.
	if path := r.sessionFile(spec.Session); path != "" {
		args = append(args, "--session", path)
	} else if spec.Session.Resume && spec.Session.Ref != "" {
		args = append(args, "--session", spec.Session.Ref)
	}
	if spec.Host != nil {
		args = append(args, "-e", piExtensionFile(r.toolDir()))
	}
	// The skill library's skills, one folder each.
	for _, dir := range SkillDirs(spec.SkillDir) {
		args = append(args, "--skill", dir)
	}
	if tools, ok := piToolSets[spec.Permission]; ok {
		// The whitelist covers extension tools too. Veyloom's own tools
		// are in every preset: the room tools only read the turn's own
		// room, and the hub decides which wiki changes wait for a person.
		if spec.Host != nil {
			tools = append(append([]string{}, tools...), turnToolNames(spec.ExtraTools)...)
		}
		args = append(args, "--tools", strings.Join(tools, ","))
	}
	return append(args, optStrings(spec.Options, "extra_args")...)
}

// sessionFile is the file pi keeps the session in, or empty when the
// runner has no session directory or the session no usable key. Only keys
// shaped like the hub's UUIDs become file names.
func (r *PiRunner) sessionFile(s Session) string {
	if r.cfg.SessionDir == "" || !isSessionKey(s.Key) {
		return ""
	}
	return filepath.Join(r.cfg.SessionDir, s.Key+".jsonl")
}

// isSessionKey reports whether key is safe to name a file after: hex
// digits and dashes, as in a UUID, and nothing else.
func isSessionKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, c := range key {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F', c == '-':
		default:
			return false
		}
	}
	return true
}

type piTurn struct {
	*turnBase
	// ctx is the turn's lifetime; requests to people wait on it.
	ctx context.Context
	cfg PiConfig
	// release gives the turn's tool endpoint back.
	release func()
	// in carries the commands and the answers to pi's requests. It is
	// closed once pi is done (see piSettle): pi then exits.
	in *jsonLines
}

// run reads pi's output until it ends, then reconciles the exit status
// with what was parsed. Requests to people are answered as they come; pi
// being done, or refusing the prompt, closes the input, which ends pi.
func (t *piTurn) run(proc *cliProcess) {
	parser := newPiParser(t.cfg, func(ev Event) { t.emit(t.ctx, ev) })
	settle := &piSettle{
		ask:  func() { t.in.send(map[string]any{"id": piSettleID, "type": "get_state"}) },
		done: t.in.close,
	}
	proc.lines(func(raw []byte) {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &head) != nil || head.Type == "" {
			// pi prints some problems, such as a missing API key, as text.
			parser.note(string(raw))
			return
		}
		if head.Type == "extension_ui_request" {
			t.serveUI(raw)
			return
		}
		var ev piEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			parser.note(string(raw))
			return
		}
		parser.handle(ev)
		settle.handle(ev)
	})
	t.in.close()
	waitErr := proc.wait()

	res, err := parser.finish(waitErr, proc.stderrTail())
	if t.release != nil {
		t.release()
	}
	t.finish(t.ctx, res, err)
}
