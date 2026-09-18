package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// cliOptions describes a CLI process started for one turn.
type cliOptions struct {
	Binary string
	Args   []string
	Dir    string
	// Stdin is what the process reads; nil means an empty stream, so a CLI
	// that "reads piped stdin" sees EOF at once instead of waiting.
	Stdin io.Reader
	// StdinPipe instead keeps stdin open for the caller to write to, for
	// CLIs that hold a conversation over their standard streams. Stdin is
	// ignored when set.
	StdinPipe bool
	// Env entries are appended to the inherited environment.
	Env         []string
	StderrBytes int
	WaitDelay   time.Duration
}

// cliProcess wraps a CLI started for one turn: its stdout as a line
// stream, a bounded stderr tail and orderly cancellation. Both real runtime
// runners are built on it; they differ only in arguments and parsing.
type cliProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *tailBuffer
	// stdin is set when the process was started with StdinPipe.
	stdin io.WriteCloser
}

// startCLI launches the process under ctx. Cancelling ctx sends SIGTERM so
// the CLI can save its state; WaitDelay bounds how long it gets before it
// is killed and its pipes are abandoned.
func startCLI(ctx context.Context, opts cliOptions) (*cliProcess, error) {
	cmd := exec.CommandContext(ctx, opts.Binary, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Stdin = opts.Stdin
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	stderr := newTailBuffer(opts.StderrBytes)
	cmd.Stderr = stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = opts.WaitDelay

	var stdin io.WriteCloser
	if opts.StdinPipe {
		pipe, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("stdin: %w", err)
		}
		stdin = pipe
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	return &cliProcess{cmd: cmd, stdout: stdout, stderr: stderr, stdin: stdin}, nil
}

// lines feeds every stdout line to fn until the stream ends. A
// bufio.Reader rather than a Scanner: a single line can carry a whole file
// and must not hit a fixed limit, and a stalled read here would block the
// CLI on its stdout.
func (p *cliProcess) lines(fn func([]byte)) {
	reader := bufio.NewReader(p.stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			fn(line)
		}
		if err != nil {
			return
		}
	}
}

// wait reaps the process after stdout has been drained.
func (p *cliProcess) wait() error {
	return p.cmd.Wait()
}

// stderrTail returns what the process last wrote to stderr.
func (p *cliProcess) stderrTail() string {
	return p.stderr.String()
}

// tailBuffer keeps the last limit bytes written to it: enough for an error
// message, never unbounded.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
