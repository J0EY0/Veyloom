package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/api"
	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/worker"
)

// newDiscoverCmd builds `veyloom discover`, which detects engines once and
// prints them as a table or as JSON.
func newDiscoverCmd(a *app) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Detect agent CLIs installed on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			infos := worker.NewDiscovery(engine.Builtin(), a.cfg.Worker.DetectTimeout).Run(cmd.Context())
			if asJSON {
				return printEnginesJSON(cmd.OutOrStdout(), infos)
			}
			return printEnginesTable(cmd.OutOrStdout(), infos)
		},
	}

	a.addDetectTimeoutFlag(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON instead of a table")
	return cmd
}

// printEnginesJSON writes the same document the HTTP API returns, so scripts
// can consume either source identically.
func printEnginesJSON(w io.Writer, infos []engine.Info) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(api.EnginesResponse{Engines: infos})
}

// printEnginesTable writes a human-readable summary, one engine per line.
func printEnginesTable(w io.Writer, infos []engine.Info) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ENGINE\tVERSION\tSTATUS\tPATH\tDETAIL")
	for _, info := range infos {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Version, info.Status, info.Path, info.Detail)
	}
	return tw.Flush()
}
