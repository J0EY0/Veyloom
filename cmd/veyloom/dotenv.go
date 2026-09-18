package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/subosito/gotenv"

	veyloom "github.com/J0EY0/veyloom"
)

// loadDotEnv reads the settings file into the environment before the
// configuration is resolved; a variable already set in the shell wins.
// serve writes the file first when it is missing, from .env.example, so
// there is a file to edit and nothing to copy. An empty --env-file means
// no file at all, and mcp-proxy, which runs inside an agent's checkout,
// never touches one.
func (a *app) loadDotEnv(cmd *cobra.Command) error {
	if a.envFile == "" || cmd.Name() == "mcp-proxy" {
		return nil
	}
	if cmd.Name() == "serve" {
		created, err := writeDotEnv(a.envFile)
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s with the default settings; edit it to change them\n", a.envFile)
		}
	}
	err := gotenv.Load(a.envFile)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("read %s: %w", a.envFile, err)
}

// writeDotEnv creates path from .env.example and reports whether it did;
// an existing file is left alone.
func writeDotEnv(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := f.Write(veyloom.EnvExample); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
