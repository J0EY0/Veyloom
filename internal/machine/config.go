package machine

import (
	"os"
	"time"
)

// Config holds the machine's tunables. Zero fields are filled from
// DefaultConfig, so callers may set only what they care about.
type Config struct {
	// Name is the human-readable label sent to the hub, typically the
	// hostname. It is not the machine's identity; see Identity.
	Name string `mapstructure:"name"`
	// DetectTimeout bounds each runtime's version and login probes. CLIs
	// built on Node can take a couple of seconds to start, so the default is
	// generous while still protecting against a hung binary.
	DetectTimeout time.Duration `mapstructure:"detect_timeout"`
	// HandshakeTimeout bounds how long the hub may take to answer Hello.
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	// HeartbeatInterval applies when the hub does not specify one.
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	// EventFlushInterval is how long streamed reply text is buffered before
	// it is sent to the hub as one event. Zero takes the default; a negative
	// value disables buffering and sends every chunk as it arrives.
	EventFlushInterval time.Duration `mapstructure:"event_flush_interval"`
	// SessionDir is where runtimes that keep session files of their own
	// (Pi) put them, one file per member session. Empty leaves sessions
	// wherever each CLI keeps them; the config loader fills in a directory
	// under the state dir.
	SessionDir string `mapstructure:"session_dir"`
	// ToolDir is where runtimes write the files that give their CLI the
	// room tools (Pi's extension), and where the skill library's skills are
	// written for the runtimes to load (under skills/). Empty means a
	// temporary directory for the one, no skills for the other; the config
	// loader fills in one under the state dir.
	ToolDir string `mapstructure:"tool_dir"`
}

// DefaultConfig returns the defaults every Config is completed with.
func DefaultConfig() Config {
	return Config{
		Name:               hostnameOrLocal(),
		DetectTimeout:      15 * time.Second,
		HandshakeTimeout:   10 * time.Second,
		HeartbeatInterval:  15 * time.Second,
		EventFlushInterval: 50 * time.Millisecond,
	}
}

// withDefaults returns c with zero fields replaced by DefaultConfig values.
func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.Name == "" {
		c.Name = def.Name
	}
	if c.DetectTimeout <= 0 {
		c.DetectTimeout = def.DetectTimeout
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = def.HandshakeTimeout
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = def.HeartbeatInterval
	}
	if c.EventFlushInterval == 0 {
		c.EventFlushInterval = def.EventFlushInterval
	}
	return c
}

// hostnameOrLocal is the default machine label.
func hostnameOrLocal() string {
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "local"
}
