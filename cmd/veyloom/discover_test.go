package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/api"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// runCommand executes the root command with args and returns what it wrote to
// stdout. PATH is emptied so no real agent CLI is picked up.
func runCommand(t *testing.T, args ...string) string {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	// An empty HOME keeps a personal ~/.veyloom/veyloom.yaml out of the test.
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("command failed: %v\noutput:\n%s", err, out.String())
	}
	return out.String()
}

func TestDiscover_JSON(t *testing.T) {
	out := runCommand(t, "discover", "--json")

	var body api.RuntimesResponse
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(body.Runtimes) != len(runtime.Builtin()) {
		t.Fatalf("got %d runtimes, want %d", len(body.Runtimes), len(runtime.Builtin()))
	}
	for _, info := range body.Runtimes {
		// The fake runtime needs no binary and is always ready; every real
		// runtime is missing from the empty PATH.
		want := runtime.StatusNotInstalled
		if info.Name == "fake" {
			want = runtime.StatusReady
		}
		if info.Status != want {
			t.Errorf("%s: status = %q, want %q with an empty PATH", info.Name, info.Status, want)
		}
	}
}

func TestDiscover_Table(t *testing.T) {
	out := runCommand(t, "discover")

	if !strings.HasPrefix(out, "RUNTIME") {
		t.Errorf("table should start with a header, got:\n%s", out)
	}
	for _, name := range []string{"claude", "codex", "pi"} {
		if !strings.Contains(out, name) {
			t.Errorf("table should list %s, got:\n%s", name, out)
		}
	}
}

func TestRoot_ExplicitMissingConfigFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"discover", "--config", filepath.Join(t.TempDir(), "absent.yaml")})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Error("a missing --config file must be an error")
	}
}

func TestDiscover_TimeoutFlagOverridesConfigFile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "veyloom.yaml")
	if err := os.WriteFile(file, []byte("machine:\n  detect_timeout: 1ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A fake runtime that sleeps longer than the file's 1ms but shorter than
	// the flag's value shows which timeout actually applied.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n/bin/sleep 0.2\necho 1.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"discover", "--json", "--config", file, "--timeout", "5s"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("command failed: %v\n%s", err, out.String())
	}

	var body api.RuntimesResponse
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	for _, info := range body.Runtimes {
		if info.Name == "claude" && info.Status == runtime.StatusError {
			t.Errorf("flag timeout should have won over the file's 1ms, got %+v", info)
		}
	}
}
