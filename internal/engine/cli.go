package engine

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AuthProbe reports whether the CLI at bin is authenticated.
//
// Implementations return an error only when the probe itself could not run or
// its output could not be understood. "Not logged in" is a normal answer, not
// an error.
type AuthProbe func(ctx context.Context, bin string) (loggedIn bool, err error)

// CLIDetector detects an engine that ships as a command-line tool.
//
// Detection is three steps: find the binary on PATH, ask it for its version,
// and optionally probe its login state. Engines differ only in the binary
// name, the version flag and the probe, so all built-in engines are
// CLIDetectors configured differently.
type CLIDetector struct {
	name        string
	binary      string
	versionArgs []string
	authProbe   AuthProbe
}

// NewCLIDetector builds a detector for the given engine.
//
// probe may be nil for engines that offer no way to check login state; such
// engines report StatusAuthUnknown once the binary and version are confirmed.
func NewCLIDetector(name, binary string, versionArgs []string, probe AuthProbe) CLIDetector {
	return CLIDetector{
		name:        name,
		binary:      binary,
		versionArgs: versionArgs,
		authProbe:   probe,
	}
}

// Name implements Detector.
func (d CLIDetector) Name() string { return d.name }

// Detect implements Detector.
func (d CLIDetector) Detect(ctx context.Context) Info {
	info := Info{Name: d.name, Binary: d.binary}

	path, err := exec.LookPath(d.binary)
	if err != nil {
		info.Status = StatusNotInstalled
		info.Detail = fmt.Sprintf("%q not found on PATH", d.binary)
		return info
	}
	info.Path = path

	out, err := runCommand(ctx, path, d.versionArgs...)
	if err != nil {
		info.Status = StatusError
		info.Detail = "version check failed: " + err.Error()
		return info
	}
	info.Version = parseVersion(out)

	if d.authProbe == nil {
		info.Status = StatusAuthUnknown
		info.Detail = "engine does not expose a login status check"
		return info
	}

	loggedIn, err := d.authProbe(ctx, path)
	switch {
	case err != nil:
		info.Status = StatusAuthUnknown
		info.Detail = "auth probe failed: " + err.Error()
	case loggedIn:
		info.Status = StatusReady
	default:
		info.Status = StatusNotLoggedIn
	}
	return info
}

// pipeWaitDelay is how long runCommand waits for a killed process's output
// pipes to close before giving up on them. Agent CLIs spawn helper processes
// that inherit the pipes; without this, a timeout would still block until
// every helper exited on its own.
const pipeWaitDelay = time.Second

// runCommand executes bin with args and returns its trimmed stdout.
//
// A non-zero exit or a context timeout is returned as an error. The error
// wraps the underlying exec error so callers can inspect the exit code, and it
// carries the first line of stderr because that is usually the useful part of
// a CLI failure. Callers that need stdout even on failure (some CLIs print a
// structured answer and then exit non-zero) get it alongside the error.
func runCommand(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = pipeWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err == nil {
		return out, nil
	}

	label := filepath.Base(bin) + " " + strings.Join(args, " ")
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The process was killed because ctx expired; report that rather
		// than the raw "signal: killed".
		return out, fmt.Errorf("%s: %w", label, ctxErr)
	}
	if msg := firstLine(stderr.String()); msg != "" {
		return out, fmt.Errorf("%s: %w: %s", label, err, msg)
	}
	return out, fmt.Errorf("%s: %w", label, err)
}

// versionPattern matches a semantic version with an optional pre-release or
// build suffix, e.g. "2.1.85", "0.42.0-beta.1".
var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// parseVersion extracts a version from arbitrary --version output.
//
// CLIs format this differently ("2.1.85 (Claude Code)", "codex-cli 0.42.0"),
// so we take the first thing that looks like a version and otherwise fall back
// to the first line verbatim.
func parseVersion(out string) string {
	if m := versionPattern.FindString(out); m != "" {
		return m
	}
	return firstLine(out)
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
