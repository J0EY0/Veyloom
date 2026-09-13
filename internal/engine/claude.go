package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// Claude returns the detector for Claude Code, Anthropic's coding agent CLI.
func Claude() Detector {
	return NewCLIDetector("claude", "claude", []string{"--version"}, claudeAuthProbe)
}

// claudeAuthProbe runs `claude auth status`, which prints a JSON document such
// as:
//
//	{"loggedIn": true, "authMethod": "oauth", "apiProvider": "firstParty"}
//
// The exit code mirrors loggedIn (0 when logged in, 1 when not), so a non-zero
// exit is expected and the JSON on stdout is what we trust. The exit code only
// matters when there is no JSON to read.
func claudeAuthProbe(ctx context.Context, bin string) (bool, error) {
	out, runErr := runCommand(ctx, bin, "auth", "status")

	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		if runErr != nil {
			return false, runErr
		}
		return false, fmt.Errorf("parse auth status: %w", err)
	}
	return status.LoggedIn, nil
}
