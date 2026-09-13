package engine

import (
	"context"
	"testing"
)

// fakeClaude mimics the two subcommands the detector uses: `--version` and
// `auth status`. Like the real CLI, `auth status` prints JSON and exits 1
// when the user is not logged in.
func fakeClaude(t *testing.T, loggedIn bool) {
	t.Helper()
	authExit := "0"
	authJSON := `{"loggedIn": true, "authMethod": "oauth", "apiProvider": "firstParty"}`
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

func TestClaude_LoggedIn(t *testing.T) {
	fakeClaude(t, true)

	info := Claude().Detect(context.Background())

	if info.Status != StatusReady {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusReady, info.Detail)
	}
	if info.Name != "claude" || info.Version != "2.1.85" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestClaude_NotLoggedIn(t *testing.T) {
	fakeClaude(t, false)

	info := Claude().Detect(context.Background())

	// Exit code 1 plus loggedIn:false must read as "not logged in", not as
	// a probe failure.
	if info.Status != StatusNotLoggedIn {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotLoggedIn, info.Detail)
	}
}

func TestClaude_MalformedAuthOutput(t *testing.T) {
	fakeBinary(t, "claude", `
case "$1" in
  --version) echo "2.1.85 (Claude Code)" ;;
  auth) echo "not json at all" ;;
esac
`)

	info := Claude().Detect(context.Background())

	// The binary works, so this is an unknown login state, not a hard error.
	if info.Status != StatusAuthUnknown {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusAuthUnknown, info.Detail)
	}
}

func TestClaude_AuthCommandCrashes(t *testing.T) {
	fakeBinary(t, "claude", `
case "$1" in
  --version) echo "2.1.85 (Claude Code)" ;;
  auth) echo "keychain unavailable" >&2; exit 3 ;;
esac
`)

	info := Claude().Detect(context.Background())

	if info.Status != StatusAuthUnknown {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusAuthUnknown, info.Detail)
	}
	if want := "keychain unavailable"; !contains(info.Detail, want) {
		t.Errorf("detail = %q, want it to include %q", info.Detail, want)
	}
}
