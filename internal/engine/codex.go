package engine

import (
	"context"
	"errors"
	"os/exec"
)

// Codex returns the detector for OpenAI's Codex CLI.
func Codex() Detector {
	return NewCLIDetector("codex", "codex", []string{"--version"}, codexAuthProbe)
}

// codexAuthProbe runs `codex login status`.
//
// The command exits 0 when credentials exist and non-zero otherwise, so the
// exit code is the signal. The printed text is human-oriented and not parsed.
func codexAuthProbe(ctx context.Context, bin string) (bool, error) {
	_, err := runCommand(ctx, bin, "login", "status")
	if err == nil {
		return true, nil
	}

	// A clean non-zero exit means "not logged in". Anything else, including a
	// timeout, means the probe itself failed.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		return false, nil
	}
	return false, err
}
