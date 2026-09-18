package runtime

import (
	"context"
	"testing"
)

// fakePi mimics `pi --version`, which Pi prints to stderr, and `pi
// --list-models` printing list.
func fakePi(t *testing.T, list string) {
	t.Helper()
	fakeBinary(t, "pi", `
case "$1" in
  --version) echo "0.73.1" >&2 ;;
  --list-models) printf '%s\n' "`+list+`" ;;
  *) echo "unexpected args: $*" >&2; exit 2 ;;
esac
`)
}

func TestPi_Configured(t *testing.T) {
	fakePi(t, "provider  model              context\ndeepseek  deepseek-v4-flash  1M")

	info := Pi().Detect(context.Background())

	if info.Status != StatusReady || info.Detail != "" {
		t.Fatalf("status = %q, detail = %q, want ready", info.Status, info.Detail)
	}
	if info.Version != "0.73.1" {
		t.Errorf("version = %q, want 0.73.1", info.Version)
	}
}

func TestPi_NotConfigured(t *testing.T) {
	fakePi(t, "No models available. Use /login to log into a provider via OAuth or API key. See:")

	if info := Pi().Detect(context.Background()); info.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotConfigured, info.Detail)
	}
}

func TestPi_UnreadableList(t *testing.T) {
	fakePi(t, "something else entirely")

	info := Pi().Detect(context.Background())

	// Not a list Pi is known to print: the runtime is not flagged for it.
	if info.Status != StatusReady || !contains(info.Detail, "unexpected model list") {
		t.Fatalf("status = %q, detail = %q", info.Status, info.Detail)
	}
}
