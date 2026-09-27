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
	KeyDatabaseURL          = "database.url"
	KeyStateDir             = "state.dir"
	KeyServerAddr           = "server.addr"
	KeyMachineName          = "machine.name"
	KeyMachineDetectTimeout = "machine.detect_timeout"
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
	// Attachments and avatars too.
	if cfg.Hub.AttachmentDir == "" {
		cfg.Hub.AttachmentDir = filepath.Join(cfg.State.Dir, "attachments")
	} else {
		cfg.Hub.AttachmentDir = expandHome(cfg.Hub.AttachmentDir)
	}
	if cfg.Hub.AvatarDir == "" {
		cfg.Hub.AvatarDir = filepath.Join(cfg.State.Dir, "avatars")
	} else {
		cfg.Hub.AvatarDir = expandHome(cfg.Hub.AvatarDir)
	}
	if cfg.Hub.WikiDir == "" {
		cfg.Hub.WikiDir = filepath.Join(cfg.State.Dir, "wiki")
	} else {
		cfg.Hub.WikiDir = expandHome(cfg.Hub.WikiDir)
	}
	// The machine's session files as well.
	if cfg.Machine.SessionDir == "" {
		cfg.Machine.SessionDir = filepath.Join(cfg.State.Dir, "sessions")
	} else {
		cfg.Machine.SessionDir = expandHome(cfg.Machine.SessionDir)
	}
	if cfg.Machine.ToolDir == "" {
		cfg.Machine.ToolDir = filepath.Join(cfg.State.Dir, "tools")
	} else {
		cfg.Machine.ToolDir = expandHome(cfg.Machine.ToolDir)
	}
	if cfg.Machine.WorktreeDir == "" {
		cfg.Machine.WorktreeDir = filepath.Join(cfg.State.Dir, "worktrees")
	} else {
		cfg.Machine.WorktreeDir = expandHome(cfg.Machine.WorktreeDir)
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
	v.SetDefault("hub.attachment_dir", def.Hub.AttachmentDir)
	v.SetDefault("hub.avatar_dir", def.Hub.AvatarDir)
	v.SetDefault("hub.wiki_dir", def.Hub.WikiDir)
	v.SetDefault("hub.brief_messages", def.Hub.BriefMessages)
	v.SetDefault("hub.brief_room_messages", def.Hub.BriefRoomMessages)
	v.SetDefault("hub.brief_topics", def.Hub.BriefTopics)
	v.SetDefault("hub.brief_wiki_pages", def.Hub.BriefWikiPages)
	v.SetDefault("hub.brief_resident_chars", def.Hub.BriefResidentChars)
	v.SetDefault("hub.memory_personal_chars", def.Hub.MemoryPersonalChars)
	v.SetDefault("hub.memory_project_chars", def.Hub.MemoryProjectChars)
	v.SetDefault("hub.approval_timeout", def.Hub.ApprovalTimeout)
	v.SetDefault("hub.turn_quiet_after", def.Hub.TurnQuietAfter)
	v.SetDefault("hub.upkeep_idle", def.Hub.UpkeepIdle)
	v.SetDefault("hub.upkeep_check", def.Hub.UpkeepCheck)
	v.SetDefault("hub.upkeep_turns", def.Hub.UpkeepTurns)
	v.SetDefault("hub.upkeep_runs_per_day", def.Hub.UpkeepRunsPerDay)
	v.SetDefault("hub.upkeep_offer_topics", def.Hub.UpkeepOfferTopics)
	v.SetDefault("hub.skill_trial_uses", def.Hub.SkillTrialUses)
	v.SetDefault(KeyMachineName, def.Machine.Name)
	v.SetDefault(KeyMachineDetectTimeout, def.Machine.DetectTimeout)
	v.SetDefault("machine.handshake_timeout", def.Machine.HandshakeTimeout)
	v.SetDefault("machine.heartbeat_interval", def.Machine.HeartbeatInterval)
	v.SetDefault("machine.event_flush_interval", def.Machine.EventFlushInterval)
	v.SetDefault("machine.session_dir", def.Machine.SessionDir)
	v.SetDefault("machine.tool_dir", def.Machine.ToolDir)
	v.SetDefault("machine.worktree_dir", def.Machine.WorktreeDir)
	v.SetDefault("machine.workspace_setup_timeout", def.Machine.WorkspaceSetupTimeout)
}

// expandHome replaces a leading "~/" with the home directory, which YAML
// and environment values cannot do on their own.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}
