package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestDefault_IsComplete(t *testing.T) {
	c := Default()

	if c.Database.URL == "" || c.State.Dir == "" || c.Server.Addr == "" {
		t.Errorf("defaults must not be empty: %+v", c)
	}
	if c.Server.ReadHeaderTimeout <= 0 || c.Server.ShutdownTimeout <= 0 {
		t.Errorf("server timeouts must be positive: %+v", c.Server)
	}
	if c.Hub.HeartbeatInterval <= 0 || c.Machine.DetectTimeout <= 0 {
		t.Errorf("subsystem defaults must be filled in: hub %+v, machine %+v", c.Hub, c.Machine)
	}
	if c.MachineIdentityPath() != filepath.Join(c.State.Dir, "machine.json") {
		t.Errorf("identity path = %q", c.MachineIdentityPath())
	}
}

// TestDefaults_CoverEveryField fails when a field is added to Config without
// a matching SetDefault, which would make it invisible to the environment.
func TestDefaults_CoverEveryField(t *testing.T) {
	want := structKeys("", reflect.TypeOf(Config{}))
	got := NewLoader().v.AllKeys()
	sort.Strings(want)
	sort.Strings(got)

	if !reflect.DeepEqual(want, got) {
		t.Errorf("registered defaults do not match Config fields\n want %v\n got  %v", want, got)
	}
}

// structKeys lists the dotted mapstructure keys of every leaf field.
func structKeys(prefix string, typ reflect.Type) []string {
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		key := f.Tag.Get("mapstructure")
		if prefix != "" {
			key = prefix + "." + key
		}
		if f.Type.Kind() == reflect.Struct {
			keys = append(keys, structKeys(key, f.Type)...)
		} else {
			keys = append(keys, key)
		}
	}
	return keys
}

// writeConfig writes a YAML file into a temp dir and returns its path.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "veyloom.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Precedence(t *testing.T) {
	file := writeConfig(t, "server:\n  addr: from-file\nhub:\n  heartbeat_interval: 42s\n")

	t.Run("file over defaults", func(t *testing.T) {
		cfg, err := NewLoader().Load(file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.Addr != "from-file" {
			t.Errorf("Server.Addr = %q, want from-file", cfg.Server.Addr)
		}
		if cfg.Hub.HeartbeatInterval != 42*time.Second {
			t.Errorf("durations must parse from strings, got %v", cfg.Hub.HeartbeatInterval)
		}
		if cfg.Database.URL != Default().Database.URL {
			t.Errorf("keys absent from the file must keep their default, got %q", cfg.Database.URL)
		}
	})

	t.Run("environment over file", func(t *testing.T) {
		t.Setenv(EnvVar(KeyServerAddr), "from-env")
		cfg, err := NewLoader().Load(file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.Addr != "from-env" {
			t.Errorf("Server.Addr = %q, want from-env", cfg.Server.Addr)
		}
	})

	t.Run("flag over environment", func(t *testing.T) {
		t.Setenv(EnvVar(KeyServerAddr), "from-env")
		l := NewLoader()
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.String("addr", Default().Server.Addr, "")
		if err := l.BindFlag(KeyServerAddr, fs.Lookup("addr")); err != nil {
			t.Fatal(err)
		}
		if err := fs.Parse([]string{"--addr=from-flag"}); err != nil {
			t.Fatal(err)
		}

		cfg, err := l.Load(file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.Addr != "from-flag" {
			t.Errorf("Server.Addr = %q, want from-flag", cfg.Server.Addr)
		}
	})

	t.Run("unset flag does not override", func(t *testing.T) {
		l := NewLoader()
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.String("addr", Default().Server.Addr, "")
		if err := l.BindFlag(KeyServerAddr, fs.Lookup("addr")); err != nil {
			t.Fatal(err)
		}
		if err := fs.Parse(nil); err != nil {
			t.Fatal(err)
		}

		cfg, err := l.Load(file)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.Addr != "from-file" {
			t.Errorf("a flag left at its default must not win, got %q", cfg.Server.Addr)
		}
	})
}

func TestLoad_NoFileUsesDefaults(t *testing.T) {
	// Point HOME at an empty dir so no personal ~/.veyloom/veyloom.yaml
	// leaks into the test; the working directory has none either.
	t.Setenv("HOME", t.TempDir())

	l := NewLoader()
	cfg, err := l.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if l.FileUsed() != "" {
		t.Errorf("no file should be used, got %q", l.FileUsed())
	}
	if cfg.Server.Addr != Default().Server.Addr {
		t.Errorf("Server.Addr = %q, want default", cfg.Server.Addr)
	}
}

func TestLoad_FindsFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, fileDirUnderHome)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "veyloom.yaml"), []byte("server:\n  addr: from-home\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := NewLoader().Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "from-home" {
		t.Errorf("Server.Addr = %q, want from-home", cfg.Server.Addr)
	}
}

func TestLoad_ExplicitMissingFileIsAnError(t *testing.T) {
	_, err := NewLoader().Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Error("an explicitly named file that does not exist must be an error")
	}
}

func TestLoad_InvalidDurationIsAnError(t *testing.T) {
	file := writeConfig(t, "hub:\n  heartbeat_interval: soon\n")

	_, err := NewLoader().Load(file)
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("got %v, want a parse error", err)
	}
}

func TestLoad_ExpandsHomeInStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := writeConfig(t, "state:\n  dir: ~/custom\n")

	cfg, err := NewLoader().Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.State.Dir != filepath.Join(home, "custom") {
		t.Errorf("State.Dir = %q, want it under %s", cfg.State.Dir, home)
	}
}

func TestEnvVar(t *testing.T) {
	if got := EnvVar(KeyMachineDetectTimeout); got != "VEYLOOM_MACHINE_DETECT_TIMEOUT" {
		t.Errorf("EnvVar = %q", got)
	}
}

func TestLoad_TranscriptDirDerivedFromStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := NewLoader().Load(writeConfig(t, "state:\n  dir: ~/state\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hub.TranscriptDir != filepath.Join(home, "state", "turns") {
		t.Errorf("TranscriptDir = %q, want it under the state dir", cfg.Hub.TranscriptDir)
	}
	if cfg.Hub.AvatarDir != filepath.Join(home, "state", "avatars") {
		t.Errorf("AvatarDir = %q, want it under the state dir", cfg.Hub.AvatarDir)
	}
	if cfg.Hub.WikiDir != filepath.Join(home, "state", "wiki") {
		t.Errorf("WikiDir = %q, want it under the state dir", cfg.Hub.WikiDir)
	}
	if cfg.Machine.SessionDir != filepath.Join(home, "state", "sessions") {
		t.Errorf("SessionDir = %q, want it under the state dir", cfg.Machine.SessionDir)
	}
	if cfg.Machine.ToolDir != filepath.Join(home, "state", "tools") {
		t.Errorf("ToolDir = %q, want it under the state dir", cfg.Machine.ToolDir)
	}
	if cfg.Machine.WorktreeDir != filepath.Join(home, "state", "worktrees") {
		t.Errorf("WorktreeDir = %q, want it under the state dir", cfg.Machine.WorktreeDir)
	}

	cfg, err = NewLoader().Load(writeConfig(t, "hub:\n  transcript_dir: ~/elsewhere\nmachine:\n  session_dir: ~/pi-sessions\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hub.TranscriptDir != filepath.Join(home, "elsewhere") {
		t.Errorf("explicit TranscriptDir = %q", cfg.Hub.TranscriptDir)
	}
	if cfg.Machine.SessionDir != filepath.Join(home, "pi-sessions") {
		t.Errorf("explicit SessionDir = %q", cfg.Machine.SessionDir)
	}
}
