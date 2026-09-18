package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Codex returns the detector for OpenAI's Codex CLI.
func Codex() Detector {
	return NewCLIDetector("codex", "codex", []string{"--version"}, codexConfigured)
}

// codexConfigured looks for what Codex runs with: OPENAI_API_KEY, a model
// provider other than OpenAI chosen in config.toml (how third-party APIs are
// set up), or the credentials `codex login` stored, which `codex login
// status` reports through its exit code.
func codexConfigured(ctx context.Context, bin string) (bool, error) {
	if os.Getenv("OPENAI_API_KEY") != "" || codexCustomProvider() {
		return true, nil
	}
	_, err := runCommand(ctx, bin, "login", "status")
	if err == nil {
		return true, nil
	}
	// A clean non-zero exit means nothing stored. Anything else, a timeout
	// included, means the check itself failed.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		return false, nil
	}
	return false, err
}

// codexCustomProvider reports whether Codex's config.toml, under $CODEX_HOME
// or else ~/.codex, picks a model provider other than OpenAI.
func codexCustomProvider() bool {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ".codex")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return false
	}
	var config struct {
		ModelProvider string `toml:"model_provider"`
	}
	if toml.Unmarshal(raw, &config) != nil {
		return false
	}
	provider := strings.TrimSpace(config.ModelProvider)
	return provider != "" && provider != "openai"
}
