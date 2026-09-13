package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
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

func TestCLIDetector_AuthOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		probe      AuthProbe
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "no probe reports auth unknown",
			probe:      nil,
			wantStatus: StatusAuthUnknown,
			wantDetail: "does not expose",
		},
		{
			name:       "logged in reports ready",
			probe:      func(context.Context, string) (bool, error) { return true, nil },
			wantStatus: StatusReady,
		},
		{
			name:       "not logged in",
			probe:      func(context.Context, string) (bool, error) { return false, nil },
			wantStatus: StatusNotLoggedIn,
		},
		{
			name:       "probe failure reports auth unknown with reason",
			probe:      func(context.Context, string) (bool, error) { return false, errors.New("boom") },
			wantStatus: StatusAuthUnknown,
			wantDetail: "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fakeBinary(t, "fakeagent", `echo "fakeagent 1.2.3"`)

			info := NewCLIDetector("fake", "fakeagent", []string{"--version"}, tt.probe).Detect(context.Background())

			if info.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q (detail: %s)", info.Status, tt.wantStatus, info.Detail)
			}
			if info.Path != path {
				t.Errorf("path = %q, want %q", info.Path, path)
			}
			if info.Version != "1.2.3" {
				t.Errorf("version = %q, want 1.2.3", info.Version)
			}
			if !strings.Contains(info.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", info.Detail, tt.wantDetail)
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
		in, want string
	}{
		{"2.1.85 (Claude Code)", "2.1.85"},
		{"codex-cli 0.42.0", "0.42.0"},
		{"0.5.1", "0.5.1"},
		{"pi 1.0.0-beta.2\nextra line", "1.0.0-beta.2"},
		{"\n  weird output without version\n", "weird output without version"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := parseVersion(tt.in); got != tt.want {
			t.Errorf("parseVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// contains is a tiny readability helper for detail-string assertions.
func contains(s, substr string) bool { return strings.Contains(s, substr) }
