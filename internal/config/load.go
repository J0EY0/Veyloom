package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Keys of the settings that commands expose as flags. Every setting is
// reachable through the config file and environment; only these also have
// flags.
const (
	KeyDatabaseURL         = "database.url"
	KeyStateDir            = "state.dir"
	KeyServerAddr          = "server.addr"
	KeyWorkerName          = "worker.name"
	KeyWorkerDetectTimeout = "worker.detect_timeout"
)

const (
	// EnvPrefix is prepended to every environment variable, so the key
	// "server.addr" is read from VEYLOOM_SERVER_ADDR.
	EnvPrefix = "VEYLOOM"
	// FileName is the config file's base name; the extension is .yaml.
	FileName = "veyloom"
	// fileDirUnderHome is where the config file is looked for after the
	// working directory.
	fileDirUnderHome = ".veyloom"
)

// EnvVar returns the environment variable that overrides key.
func EnvVar(key string) string {
	return EnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// Loader resolves a Config from every source. Precedence, highest first:
// command-line flags bound with BindFlag and actually set, environment
// variables, the config file, built-in defaults.
type Loader struct {
	v *viper.Viper
}

// NewLoader prepares a Loader with defaults and environment binding in
// place. Flags are bound afterwards; the file is read by Load.
func NewLoader() *Loader {
	v := viper.New()
	setDefaults(v, Default())
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return &Loader{v: v}
}

// BindFlag makes flag override key whenever the flag is given on the
// command line. A flag left at its default does not override anything.
// A key holds one flag at a time, so bind only the flags of the command
// that is actually running.
func (l *Loader) BindFlag(key string, flag *pflag.Flag) error {
	if flag == nil {
		return fmt.Errorf("config: no flag to bind to %q", key)
	}
	return l.v.BindPFlag(key, flag)
}

// Load reads the config file and resolves every setting.
//
// path names the file explicitly and must exist. When path is empty,
// veyloom.yaml is looked for in the working directory and then in
// ~/.veyloom, and it is fine for neither to exist.
func (l *Loader) Load(path string) (Config, error) {
	if path != "" {
		l.v.SetConfigFile(path)
	} else {
		l.v.SetConfigName(FileName)
		l.v.SetConfigType("yaml")
		l.v.AddConfigPath(".")
		l.v.AddConfigPath(filepath.Join(homeDir(), fileDirUnderHome))
	}

	if err := l.v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if path != "" || !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.State.Dir = expandHome(cfg.State.Dir)
	// Transcripts live under the state dir unless placed explicitly.
	if cfg.Hub.TranscriptDir == "" {
		cfg.Hub.TranscriptDir = filepath.Join(cfg.State.Dir, "turns")
	} else {
		cfg.Hub.TranscriptDir = expandHome(cfg.Hub.TranscriptDir)
	}
	return cfg, nil
}

// FileUsed reports the config file Load read, or "" when none was found.
func (l *Loader) FileUsed() string {
	return l.v.ConfigFileUsed()
}

// setDefaults registers every setting with viper. Registering is what makes
// a key visible to environment lookup, so each field of Config must appear
// here; TestDefaults_CoverEveryField enforces that.
func setDefaults(v *viper.Viper, def Config) {
	v.SetDefault(KeyDatabaseURL, def.Database.URL)
	v.SetDefault(KeyStateDir, def.State.Dir)
	v.SetDefault(KeyServerAddr, def.Server.Addr)
	v.SetDefault("server.read_header_timeout", def.Server.ReadHeaderTimeout)
	v.SetDefault("server.shutdown_timeout", def.Server.ShutdownTimeout)
	v.SetDefault("server.write_timeout", def.Server.WriteTimeout)
	v.SetDefault("server.allowed_origins", def.Server.AllowedOrigins)
	v.SetDefault("hub.heartbeat_interval", def.Hub.HeartbeatInterval)
	v.SetDefault("hub.handshake_timeout", def.Hub.HandshakeTimeout)
	v.SetDefault("hub.store_timeout", def.Hub.StoreTimeout)
	v.SetDefault("hub.transcript_dir", def.Hub.TranscriptDir)
	v.SetDefault("hub.brief_messages", def.Hub.BriefMessages)
	v.SetDefault("hub.approval_timeout", def.Hub.ApprovalTimeout)
	v.SetDefault(KeyWorkerName, def.Worker.Name)
	v.SetDefault(KeyWorkerDetectTimeout, def.Worker.DetectTimeout)
	v.SetDefault("worker.handshake_timeout", def.Worker.HandshakeTimeout)
	v.SetDefault("worker.heartbeat_interval", def.Worker.HeartbeatInterval)
	v.SetDefault("worker.event_flush_interval", def.Worker.EventFlushInterval)
}

// expandHome replaces a leading "~/" with the home directory, which YAML
// and environment values cannot do on their own.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}
