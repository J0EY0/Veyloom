package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeCodex mimics `codex --version` and `codex login status`, whose exit
// code is the answer.
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

func TestCodex_SignedIn(t *testing.T) {
	isolateConfig(t)
	fakeCodex(t, "0")

	info := Codex().Detect(context.Background())

	if info.Status != StatusReady {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusReady, info.Detail)
	}
	if info.Name != "codex" || info.Version != "0.42.0" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestCodex_NotConfigured(t *testing.T) {
	isolateConfig(t)
	fakeCodex(t, "1")

	if info := Codex().Detect(context.Background()); info.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotConfigured, info.Detail)
	}
}

func TestCodex_KeyOrProvider(t *testing.T) {
	t.Run("OPENAI_API_KEY", func(t *testing.T) {
		isolateConfig(t)
		fakeCodex(t, "1")
		t.Setenv("OPENAI_API_KEY", "sk-test")

		if info := Codex().Detect(context.Background()); info.Status != StatusReady {
			t.Fatalf("status = %q, want ready", info.Status)
		}
	})
	for provider, want := range map[string]Status{"deepseek": StatusReady, "openai": StatusNotConfigured} {
		t.Run("model_provider "+provider, func(t *testing.T) {
			home := isolateConfig(t)
			fakeCodex(t, "1")
			dir := filepath.Join(home, ".codex")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			config := "model = \"some-model\"\nmodel_provider = \"" + provider + "\"\n\n[model_providers.deepseek]\nname = \"DeepSeek\"\nbase_url = \"https://api.example.com/v1\"\nenv_key = \"DEEPSEEK_API_KEY\"\n"
			if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}

			if info := Codex().Detect(context.Background()); info.Status != want {
				t.Fatalf("status = %q, want %q", info.Status, want)
			}
		})
	}
}
