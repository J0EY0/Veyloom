// Package config gathers every tunable of a veyloom process in one place.
//
// Each subsystem owns its own Config type and defaults (hub.Config,
// machine.Config, ...). This package composes them into one value and
// resolves it from, in increasing precedence, built-in defaults, a YAML
// config file, environment variables and command-line flags (see Loader).
// The rule is: no timeout, interval or address is defined anywhere else.
package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/machine"
)

// Config is the complete configuration of a veyloom process. The
// mapstructure tags are the keys used in the config file; see
// veyloom.example.yaml at the repository root.
type Config struct {
	Database Database       `mapstructure:"database"`
	State    State          `mapstructure:"state"`
	Server   Server         `mapstructure:"server"`
	Hub      hub.Config     `mapstructure:"hub"`
	Machine  machine.Config `mapstructure:"machine"`
}

// Database configures the Postgres connection.
type Database struct {
	// URL is the connection string, e.g. postgres://user:pass@host/db.
	URL string `mapstructure:"url"`
}

// State configures what persists on the local machine between runs.
type State struct {
	// Dir is the state directory; the machine identity file lives here. A
	// leading "~/" is expanded to the home directory.
	Dir string `mapstructure:"dir"`
}

// Server configures the HTTP API.
type Server struct {
	// Addr is the listen address.
	Addr string `mapstructure:"addr"`
	// ReadHeaderTimeout bounds how long a client may take to send headers.
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	// ShutdownTimeout bounds the graceful shutdown on exit.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// WriteTimeout bounds each message written to a WebSocket client.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	// AllowedOrigins lists browser origins allowed to use the API from
	// another origin (CORS and WebSocket), as host patterns such as
	// "localhost:5173", that may open WebSocket connections from another
	// origin. Same-origin and non-browser clients are always allowed.
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// Default returns the configuration used when nothing is overridden. The
// database URL matches docker-compose.yml so a single machine works with no
// setup beyond `docker compose up`.
func Default() Config {
	return Config{
		Database: Database{
			URL: "postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable",
		},
		State: State{
			Dir: filepath.Join(homeDir(), ".veyloom"),
		},
		Server: Server{
			Addr:              "127.0.0.1:7788",
			ReadHeaderTimeout: 5 * time.Second,
			ShutdownTimeout:   5 * time.Second,
			WriteTimeout:      10 * time.Second,
			AllowedOrigins:    []string{},
		},
		Hub:     hub.DefaultConfig(),
		Machine: machine.DefaultConfig(),
	}
}

// MachineIdentityPath is where the local machine keeps its hub-assigned ID.
func (c Config) MachineIdentityPath() string {
	return filepath.Join(c.State.Dir, "machine.json")
}

// AccountPath is where the one account and its sessions are kept
// (internal/account).
func (c Config) AccountPath() string {
	return filepath.Join(c.State.Dir, "account.json")
}

// homeDir is the user's home directory, or "." when it cannot be determined.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "."
}
