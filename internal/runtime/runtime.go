// Package runtime defines the abstraction over coding-agent CLIs such as
// Claude Code, Codex and Pi.
//
// Discovery finds a CLI on the local machine, reads its version and looks
// for what it runs with: a sign-in, an API key or a third-party provider set
// up. It only looks at the machine and never checks credentials with a
// server (2026-09-17): what counts is that something is configured, not that
// a login is valid, so a CLI on a third-party API reads as ready.
package runtime

import "context"

// Status summarises what the machine found out about a runtime.
type Status string

const (
	// StatusReady means the CLI is installed, answers its version check and
	// has something to run with.
	StatusReady Status = "ready"
	// StatusNotInstalled means the binary was not found on PATH.
	StatusNotInstalled Status = "not_installed"
	// StatusNotConfigured means the CLI runs but has nothing to run with that
	// it can see: no sign-in, API key or provider set up.
	StatusNotConfigured Status = "not_configured"
	// StatusError means detection failed before reaching a conclusion, for
	// example because the version command crashed or timed out.
	StatusError Status = "error"
)

// Info is the result of detecting one runtime.
type Info struct {
	// Name is the stable runtime identifier: "claude", "codex", "pi".
	Name string `json:"name"`
	// Binary is the executable name that was looked up on PATH.
	Binary string `json:"binary"`
	// Path is the resolved executable path; empty when not installed.
	Path string `json:"path,omitempty"`
	// Version is the version string reported by the CLI; empty when unknown.
	Version string `json:"version,omitempty"`
	// Status is the detection outcome.
	Status Status `json:"status"`
	// Detail is a human-readable explanation for non-ready statuses.
	Detail string `json:"detail,omitempty"`
}

// Detector knows how to detect one runtime on the local machine.
type Detector interface {
	// Name returns the stable runtime identifier.
	Name() string
	// Detect probes the runtime and always returns an Info, using the Status
	// and Detail fields to report problems instead of an error. Detect must
	// respect ctx cancellation so a hung CLI cannot block discovery.
	Detect(ctx context.Context) Info
}
