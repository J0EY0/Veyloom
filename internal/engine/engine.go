// Package engine defines the abstraction over coding-agent CLIs such as
// Claude Code, Codex and Pi.
//
// At this stage the package only covers discovery: finding a CLI on the local
// machine, reading its version and, where the CLI allows it, checking whether
// the user is logged in. Running turns through an engine comes later.
package engine

import "context"

// Status summarises what the worker found out about an engine.
type Status string

const (
	// StatusReady means the CLI is installed and authenticated.
	StatusReady Status = "ready"
	// StatusNotInstalled means the binary was not found on PATH.
	StatusNotInstalled Status = "not_installed"
	// StatusNotLoggedIn means the CLI runs but reports no credentials.
	StatusNotLoggedIn Status = "not_logged_in"
	// StatusAuthUnknown means the CLI runs but its login state could not be
	// determined, either because the engine offers no way to check or
	// because the check itself failed.
	StatusAuthUnknown Status = "auth_unknown"
	// StatusError means detection failed before reaching a conclusion, for
	// example because the version command crashed or timed out.
	StatusError Status = "error"
)

// Info is the result of detecting one engine.
type Info struct {
	// Name is the stable engine identifier: "claude", "codex", "pi".
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

// Detector knows how to detect one engine on the local machine.
type Detector interface {
	// Name returns the stable engine identifier.
	Name() string
	// Detect probes the engine and always returns an Info, using the Status
	// and Detail fields to report problems instead of an error. Detect must
	// respect ctx cancellation so a hung CLI cannot block discovery.
	Detect(ctx context.Context) Info
}
