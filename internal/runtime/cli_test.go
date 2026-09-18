package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// fakeBinary writes an executable shell script called name into a fresh
// temporary directory, points PATH at that directory, and returns the script
// path. This lets tests drive CLIDetector against real processes without
// depending on any agent CLI being installed.
func fakeBinary(t *testing.T, name, script string) string {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("fake binaries are shell scripts")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func TestCLIDetector_NotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	info := NewCLIDetector("x", "definitely-missing", []string{"--version"}, nil).Detect(context.Background())

	if info.Status != StatusNotInstalled {
		t.Fatalf("status = %q, want %q (detail: %s)", info.Status, StatusNotInstalled, info.Detail)
	}
	if info.Path != "" || info.Version != "" {
		t.Fatalf("expected empty path and version, got %+v", info)
	}
}

// isolateConfig keeps the machine's own configuration out of a test: a
// fresh home and none of the variables the probes read.
func isolateConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "ANTHROPIC_AUTH_TOKEN", "CODEX_HOME", "OPENAI_API_KEY"} {
		t.Setenv(name, "")
	}
	return home
}

func TestCLIDetector_ConfigOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		probe      ConfigProbe
		wantStatus Status
		wantDetail string
	}{
		{name: "nothing to look for is ready", probe: nil, wantStatus: StatusReady},
		{name: "configured is ready", probe: func(context.Context, string) (bool, error) { return true, nil }, wantStatus: StatusReady},
		{name: "nothing configured", probe: func(context.Context, string) (bool, error) { return false, nil }, wantStatus: StatusNotConfigured},
		{
			// A check that could not run says nothing against the runtime.
			name:       "a failed check reads as ready, with the reason",
			probe:      func(context.Context, string) (bool, error) { return false, errors.New("boom") },
			wantStatus: StatusReady,
			wantDetail: "configuration check failed: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fakeBinary(t, "fakeagent", `echo "fakeagent 1.2.3"`)

			info := NewCLIDetector("fake", "fakeagent", []string{"--version"}, tt.probe).Detect(context.Background())

			if info.Status != tt.wantStatus || info.Detail != tt.wantDetail {
				t.Fatalf("status = %q, detail = %q; want %q, %q", info.Status, info.Detail, tt.wantStatus, tt.wantDetail)
			}
			if info.Path != path || info.Version != "1.2.3" {
				t.Errorf("unexpected info: %+v", info)
			}
		})
	}
}

func TestCLIDetector_VersionCommandFails(t *testing.T) {
	fakeBinary(t, "broken", `echo "config file is corrupt" >&2; exit 1`)

	info := NewCLIDetector("broken", "broken", []string{"--version"}, nil).Detect(context.Background())

	if info.Status != StatusError {
		t.Fatalf("status = %q, want %q", info.Status, StatusError)
	}
	// The stderr line is what a user needs to see to fix the problem.
	if !strings.Contains(info.Detail, "config file is corrupt") {
		t.Errorf("detail = %q, want it to include stderr", info.Detail)
	}
}

func TestCLIDetector_Timeout(t *testing.T) {
	// PATH is restricted to the fake directory, so the script must use an
	// absolute path. The shell forks sleep as a child that inherits the
	// output pipes, which is exactly what real agent CLIs do with their
	// helper processes.
	fakeBinary(t, "slow", `/bin/sleep 5`)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	info := NewCLIDetector("slow", "slow", []string{"--version"}, nil).Detect(ctx)
	elapsed := time.Since(start)

	// Detect must come back shortly after the deadline even though the
	// orphaned sleep still holds the pipes open.
	if elapsed > 3*time.Second {
		t.Errorf("Detect took %v, want it to return soon after the 200ms deadline", elapsed)
	}
	if info.Status != StatusError {
		t.Fatalf("status = %q, want %q", info.Status, StatusError)
	}
	if !strings.Contains(info.Detail, context.DeadlineExceeded.Error()) {
		t.Errorf("detail = %q, want it to mention the deadline", info.Detail)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		stdout, stderr, want string
	}{
		{"2.1.85 (Claude Code)", "", "2.1.85"},
		{"codex-cli 0.42.0", "", "0.42.0"},
		{"0.5.1", "", "0.5.1"},
		{"pi 1.0.0-beta.2\nextra line", "", "1.0.0-beta.2"},
		{"\n  weird output without version\n", "", "weird output without version"},
		{"", "", ""},
		// Pi answers --version on stderr.
		{"", "0.73.1", "0.73.1"},
		{"2.0.0", "warning: 9.9.9 is deprecated", "2.0.0"},
		// A stderr without a version is a warning, not a version.
		{"", "warning: config is old", ""},
	}

	for _, tt := range tests {
		if got := parseVersion(tt.stdout, tt.stderr); got != tt.want {
			t.Errorf("parseVersion(%q, %q) = %q, want %q", tt.stdout, tt.stderr, got, tt.want)
		}
	}
}

func TestCLIDetector_VersionOnStderr(t *testing.T) {
	fakeBinary(t, "pi", `echo "0.73.1" >&2`)

	info := NewCLIDetector("pi", "pi", []string{"--version"}, nil).Detect(context.Background())

	if info.Version != "0.73.1" {
		t.Errorf("version = %q, want 0.73.1 read from stderr", info.Version)
	}
	if info.Status != StatusReady {
		t.Errorf("status = %q, want %q", info.Status, StatusReady)
	}
}

// contains is a tiny readability helper for detail-string assertions.
func contains(s, substr string) bool { return strings.Contains(s, substr) }
