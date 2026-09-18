package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeClaude mimics the two subcommands the detector uses: `--version` and
// `auth status`, which prints JSON and, like the real CLI, exits 1 when it
// finds nothing to run with.
func fakeClaude(t *testing.T, loggedIn bool) {
	t.Helper()
	authExit := "0"
	authJSON := `{"loggedIn": true, "authMethod": "api_key", "apiProvider": "firstParty"}`
	if !loggedIn {
		authExit = "1"
		authJSON = `{"loggedIn": false, "authMethod": "none", "apiProvider": "firstParty"}`
	}
	fakeBinary(t, "claude", `
case "$1" in
  --version) echo "2.1.85 (Claude Code)" ;;
  auth) echo '`+authJSON+`'; exit `+authExit+` ;;
  *) echo "unexpected args: $*" >&2; exit 2 ;;
esac
`)
}

func TestClaude_Configured(t *testing.T) {
	isolateConfig(t)
	fakeClaude(t, true)

	info := Claude().Detect(context.Background())

	if info.Status != StatusReady || info.Detail != "" {
		t.Fatalf("status = %q, detail = %q, want ready", info.Status, info.Detail)
	}
	if info.Name != "claude" || info.Version != "2.1.85" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestClaude_NotConfigured(t *testing.T) {
	isolateConfig(t)
	fakeClaude(t, false)

	// Exit code 1 with loggedIn:false is nothing to run with, not a failed check.
	if info := Claude().Detect(context.Background()); info.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotConfigured, info.Detail)
	}
}

func TestClaude_ThirdPartyAPI(t *testing.T) {
	// `auth status` says nothing is configured for ANTHROPIC_AUTH_TOKEN, the
	// usual third-party setup; the token alone counts, in the environment or
	// in the env block of settings.json.
	t.Run("environment", func(t *testing.T) {
		isolateConfig(t)
		fakeClaude(t, false)
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "sk-third-party")

		if info := Claude().Detect(context.Background()); info.Status != StatusReady {
			t.Fatalf("status = %q, want ready (detail: %s)", info.Status, info.Detail)
		}
	})
	t.Run("settings", func(t *testing.T) {
		home := isolateConfig(t)
		fakeClaude(t, false)
		dir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		settings := `{"env": {"ANTHROPIC_BASE_URL": "https://api.example.com", "ANTHROPIC_AUTH_TOKEN": "sk-third-party"}}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}

		if info := Claude().Detect(context.Background()); info.Status != StatusReady {
			t.Fatalf("status = %q, want ready (detail: %s)", info.Status, info.Detail)
		}
	})
}

func TestClaude_AuthCommandCrashes(t *testing.T) {
	isolateConfig(t)
	fakeBinary(t, "claude", `
case "$1" in
  --version) echo "2.1.85 (Claude Code)" ;;
  auth) echo "keychain unavailable" >&2; exit 3 ;;
esac
`)

	info := Claude().Detect(context.Background())

	// The binary works; a check that could not run does not flag it.
	if info.Status != StatusReady || !contains(info.Detail, "keychain unavailable") {
		t.Fatalf("status = %q, detail = %q", info.Status, info.Detail)
	}
}
