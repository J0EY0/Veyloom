package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/config"
)

// The helpers below define a flag and bind it to its configuration key in
// one step, so a value given on the command line overrides the file and
// the environment while an omitted flag changes nothing. Defaults shown in
// --help are the built-in ones; the effective value is resolved at run time.

func (a *app) addDatabaseFlag(cmd *cobra.Command) {
	a.stringFlag(cmd, "database-url", config.KeyDatabaseURL, config.Default().Database.URL, "Postgres connection URL")
}

func (a *app) addStateDirFlag(cmd *cobra.Command) {
	a.stringFlag(cmd, "state-dir", config.KeyStateDir, config.Default().State.Dir, "directory for local state")
}

func (a *app) addAddrFlag(cmd *cobra.Command) {
	a.stringFlag(cmd, "addr", config.KeyServerAddr, config.Default().Server.Addr, "address to listen on")
}

func (a *app) addWorkerNameFlag(cmd *cobra.Command) {
	a.stringFlag(cmd, "name", config.KeyWorkerName, config.Default().Worker.Name, "label of the local worker")
}

func (a *app) addDetectTimeoutFlag(cmd *cobra.Command) {
	a.durationFlag(cmd, "timeout", config.KeyWorkerDetectTimeout, config.Default().Worker.DetectTimeout, "per-engine detection timeout")
}

func (a *app) stringFlag(cmd *cobra.Command, name, key, def, usage string) {
	cmd.Flags().String(name, def, usage+" (env "+config.EnvVar(key)+")")
	a.bind(cmd, name, key)
}

func (a *app) durationFlag(cmd *cobra.Command, name, key string, def time.Duration, usage string) {
	cmd.Flags().Duration(name, def, usage+" (env "+config.EnvVar(key)+")")
	a.bind(cmd, name, key)
}

// bind records that name overrides key; the binding takes effect when cmd
// runs (see app.resolveConfig).
func (a *app) bind(cmd *cobra.Command, name, key string) {
	a.bindings[cmd] = append(a.bindings[cmd], flagBinding{flag: name, key: key})
}
