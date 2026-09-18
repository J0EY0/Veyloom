package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude returns the detector for Claude Code, Anthropic's coding agent CLI.
func Claude() Detector {
	return NewCLIDetector("claude", "claude", []string{"--version"}, claudeConfigured)
}

// claudeConfigured looks for what Claude Code runs with. `claude auth status`
// reads it from the machine and prints JSON such as
//
//	{"loggedIn": true, "authMethod": "api_key", "apiProvider": "firstParty"}
//
// counting a sign-in, CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY (in the
// environment or the env block of settings.json), apiKeyHelper, Bedrock and
// Vertex; its exit code mirrors loggedIn, so the JSON is what is trusted. It
// does not count ANTHROPIC_AUTH_TOKEN, which with ANTHROPIC_BASE_URL is how
// most third-party APIs are set up, so that is looked for first.
func claudeConfigured(ctx context.Context, bin string) (bool, error) {
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" || claudeSettingsEnv("ANTHROPIC_AUTH_TOKEN") != "" {
		return true, nil
	}
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

// claudeSettingsEnv reads one variable from the env block of Claude Code's
// user settings, $CLAUDE_CONFIG_DIR/settings.json or else
// ~/.claude/settings.json. Anything missing or unreadable is empty.
func claudeSettingsEnv(name string) string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		Env map[string]any `json:"env"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return ""
	}
	value, _ := settings.Env[name].(string)
	return value
}
