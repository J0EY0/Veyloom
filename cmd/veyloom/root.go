package main

import (
	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/config"
)

// app carries what every command shares: the configuration loader, the
// path given with --config, the flag bindings each command declared, and
// the resolved configuration once the root's PersistentPreRunE has run.
type app struct {
	loader     *config.Loader
	configPath string
	cfg        config.Config
	// bindings maps each command to the flags it wants to override config
	// keys with. They are applied only for the command actually being run:
	// several commands define a --timeout, and viper keeps a single flag per
	// key, so binding all of them up front would let the wrong one win.
	bindings map[*cobra.Command][]flagBinding
}

// flagBinding ties a flag name to a configuration key.
type flagBinding struct {
	flag string
	key  string
}

// newRootCmd assembles the command tree. Each subcommand lives in its own
// file and is built by a constructor so tests can create isolated instances.
func newRootCmd() *cobra.Command {
	a := &app{
		loader:   config.NewLoader(),
		bindings: make(map[*cobra.Command][]flagBinding),
	}

	root := &cobra.Command{
		Use:   "veyloom",
		Short: "Run coding agents as a team on your projects",
		Long: `Veyloom lets coding agents such as Claude Code, Codex and Pi work
together on a project through a shared group chat.

Settings come from, in increasing precedence: built-in defaults, a config
file (--config, else ./veyloom.yaml, else ~/.veyloom/veyloom.yaml),
environment variables prefixed VEYLOOM_, and command-line flags.`,
		// Runtime failures should not be followed by a usage dump; the
		// error message alone is what the user needs.
		SilenceUsage: true,
		// Flags are parsed by now and cmd is the command being run, so this
		// is the earliest point at which every source of configuration is
		// available.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.resolveConfig(cmd)
		},
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "", "config file (default: ./veyloom.yaml, then ~/.veyloom/veyloom.yaml)")

	root.AddCommand(
		newDiscoverCmd(a),
		newServeCmd(a),
		newMigrateCmd(a),
		newMCPProxyCmd(),
	)
	return root
}

// resolveConfig binds the running command's flags and loads the
// configuration into a.cfg.
func (a *app) resolveConfig(cmd *cobra.Command) error {
	for _, b := range a.bindings[cmd] {
		if err := a.loader.BindFlag(b.key, cmd.Flags().Lookup(b.flag)); err != nil {
			return err
		}
	}
	cfg, err := a.loader.Load(a.configPath)
	if err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}
