package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	veyloom "github.com/J0EY0/veyloom"
)

func TestLoadDotEnv_ServeWritesTheFileOnceAndReadsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	a := &app{envFile: path}
	var stderr bytes.Buffer
	serve := &cobra.Command{Use: "serve"}
	serve.SetErr(&stderr)

	// The marker rides along in the file so the load is observable without
	// touching a real setting; t.Setenv puts the variable back afterwards.
	t.Setenv("VEYLOOM_DOTENV_MARKER", "")
	_ = os.Unsetenv("VEYLOOM_DOTENV_MARKER")

	if err := a.loadDotEnv(serve); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("serve should have written the file: %v", err)
	}
	if !bytes.Equal(got, veyloom.EnvExample) {
		t.Error("the file should be .env.example verbatim")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", info.Mode().Perm())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("wrote "+path)) {
		t.Errorf("serve should say it wrote the file, got %q", stderr.String())
	}

	// An edited file is read, and never overwritten.
	if err := os.WriteFile(path, []byte("VEYLOOM_DOTENV_MARKER=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if err := a.loadDotEnv(serve); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Errorf("an existing file should not be announced: %q", stderr.String())
	}
	if got := os.Getenv("VEYLOOM_DOTENV_MARKER"); got != "from-file" {
		t.Errorf("marker = %q, want from-file", got)
	}
	if raw, _ := os.ReadFile(path); !bytes.Contains(raw, []byte("from-file")) {
		t.Error("the edited file should survive")
	}
}

func TestLoadDotEnv_OtherCommandsOnlyRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	a := &app{envFile: path}
	if err := a.loadDotEnv(&cobra.Command{Use: "discover"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("discover should not create the file")
	}
	if err := os.WriteFile(path, []byte("not = valid = env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// mcp-proxy runs inside an agent's checkout: it ignores the file.
	if err := a.loadDotEnv(&cobra.Command{Use: "mcp-proxy"}); err != nil {
		t.Errorf("mcp-proxy should not read the file: %v", err)
	}
	// And so does everyone when the flag is empty.
	if err := (&app{}).loadDotEnv(&cobra.Command{Use: "serve"}); err != nil {
		t.Errorf("an empty --env-file should mean none: %v", err)
	}
}
