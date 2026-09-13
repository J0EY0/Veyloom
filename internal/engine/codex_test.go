package engine

import (
	"context"
	"testing"
)

// fakeCodex mimics `codex --version` and `codex login status`. The login
// subcommand exits with loginExit, mirroring the real CLI's use of the exit
// code as the logged-in signal.
func fakeCodex(t *testing.T, loginExit string) {
	t.Helper()
	fakeBinary(t, "codex", `
case "$1" in
  --version) echo "codex-cli 0.42.0" ;;
  login) echo "login status output"; exit `+loginExit+` ;;
  *) echo "unexpected args: $*" >&2; exit 2 ;;
esac
`)
}

func TestCodex_LoggedIn(t *testing.T) {
	fakeCodex(t, "0")

	info := Codex().Detect(context.Background())

	if info.Status != StatusReady {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusReady, info.Detail)
	}
	if info.Name != "codex" || info.Version != "0.42.0" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestCodex_NotLoggedIn(t *testing.T) {
	fakeCodex(t, "1")

	info := Codex().Detect(context.Background())

	if info.Status != StatusNotLoggedIn {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotLoggedIn, info.Detail)
	}
}

func TestPi_AuthUnknown(t *testing.T) {
	fakeBinary(t, "pi", `echo "0.9.3"`)

	info := Pi().Detect(context.Background())

	if info.Status != StatusAuthUnknown {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusAuthUnknown, info.Detail)
	}
	if info.Version != "0.9.3" {
		t.Errorf("version = %q, want 0.9.3", info.Version)
	}
}
