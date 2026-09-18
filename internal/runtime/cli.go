package runtime

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

// ConfigProbe reports whether the CLI at bin has something to run with: a
// sign-in, an API key or a provider set up. It looks only at the machine and
// never checks the credentials with a server. An error means the check
// itself could not run; "not configured" is a normal answer, not an error.
type ConfigProbe func(ctx context.Context, bin string) (configured bool, err error)

// CLIDetector detects a runtime that ships as a command-line tool.
//
// Detection is three steps: find the binary on PATH, ask it for its version
// and look for its configuration. Runtimes differ only in the binary name,
// the version flag and the probe, so all built-in runtimes are CLIDetectors
// configured differently.
type CLIDetector struct {
	name        string
	binary      string
	versionArgs []string
	configProbe ConfigProbe
}

// NewCLIDetector builds a detector for the given runtime. probe may be nil
// for a runtime with nothing to look for.
func NewCLIDetector(name, binary string, versionArgs []string, probe ConfigProbe) CLIDetector {
	return CLIDetector{
		name:        name,
		binary:      binary,
		versionArgs: versionArgs,
		configProbe: probe,
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

	out, errOut, err := runCommandOutput(ctx, path, d.versionArgs...)
	if err != nil {
		info.Status = StatusError
		info.Detail = "version check failed: " + err.Error()
		return info
	}
	info.Version = parseVersion(out, errOut)

	if d.configProbe == nil {
		info.Status = StatusReady
		return info
	}
	configured, err := d.configProbe(ctx, path)
	switch {
	case err != nil:
		// A check that could not run says nothing against the runtime; the
		// reason is kept for whoever looks.
		info.Status = StatusReady
		info.Detail = "configuration check failed: " + err.Error()
	case configured:
		info.Status = StatusReady
	default:
		info.Status = StatusNotConfigured
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
	out, _, err := runCommandOutput(ctx, bin, args...)
	return out, err
}

// runCommandOutput is runCommand that also hands back the trimmed stderr,
// for CLIs that answer there: `pi --version` prints its version to stderr.
func runCommandOutput(ctx context.Context, bin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = pipeWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	if err == nil {
		return out, errOut, nil
	}

	label := filepath.Base(bin) + " " + strings.Join(args, " ")
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The process was killed because ctx expired; report that rather
		// than the raw "signal: killed".
		return out, errOut, fmt.Errorf("%s: %w", label, ctxErr)
	}
	if msg := firstLine(errOut); msg != "" {
		return out, errOut, fmt.Errorf("%s: %w: %s", label, err, msg)
	}
	return out, errOut, fmt.Errorf("%s: %w", label, err)
}

// versionPattern matches a semantic version with an optional pre-release or
// build suffix, e.g. "2.1.85", "0.42.0-beta.1".
var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// parseVersion extracts a version from arbitrary --version output.
//
// CLIs format this differently ("2.1.85 (Claude Code)", "codex-cli 0.42.0"),
// so we take the first thing that looks like a version and otherwise fall back
// to the first line of stdout verbatim. Some print the version to stderr
// instead; stderr is searched for a version too, but its first line is not a
// fallback, since there it is as likely to be a warning.
func parseVersion(stdout, stderr string) string {
	for _, out := range []string{stdout, stderr} {
		if m := versionPattern.FindString(out); m != "" {
			return m
		}
	}
	return firstLine(stdout)
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
