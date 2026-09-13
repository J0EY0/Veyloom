package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newMigrateCmd builds `veyloom migrate`, which applies pending schema
// migrations and exits. serve does the same on startup; this command exists
// for operators who want to migrate ahead of a deploy.
func newMigrateCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore(cmd.Context(), a.cfg.Database.URL)
			if err != nil {
				return err
			}
			defer s.Close()
			fmt.Fprintln(cmd.OutOrStdout(), "database schema is up to date")
			return nil
		},
	}
	a.addDatabaseFlag(cmd)
	return cmd
}
