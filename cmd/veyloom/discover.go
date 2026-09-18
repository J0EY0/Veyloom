package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/api"
	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// newDiscoverCmd builds `veyloom discover`, which detects runtimes once and
// prints them as a table or as JSON.
func newDiscoverCmd(a *app) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Detect agent CLIs installed on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			infos := machine.NewDiscovery(runtime.Builtin(), a.cfg.Machine.DetectTimeout).Run(cmd.Context())
			if asJSON {
				return printRuntimesJSON(cmd.OutOrStdout(), infos)
			}
			return printRuntimesTable(cmd.OutOrStdout(), infos)
		},
	}

	a.addDetectTimeoutFlag(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON instead of a table")
	return cmd
}

// printRuntimesJSON writes the same document the HTTP API returns, so scripts
// can consume either source identically.
func printRuntimesJSON(w io.Writer, infos []runtime.Info) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(api.RuntimesResponse{Runtimes: infos})
}

// printRuntimesTable writes a human-readable summary, one runtime per line.
func printRuntimesTable(w io.Writer, infos []runtime.Info) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RUNTIME\tVERSION\tSTATUS\tPATH\tDETAIL")
	for _, info := range infos {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Version, info.Status, info.Path, info.Detail)
	}
	return tw.Flush()
}
