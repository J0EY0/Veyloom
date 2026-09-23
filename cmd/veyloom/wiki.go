package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// newWikiCmd builds `veyloom wiki`: what can be done to the wikis on disk
// without a hub running.
func newWikiCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiki",
		Short: "Work with the wikis on disk",
	}
	cmd.AddCommand(newWikiCheckCmd(a))
	return cmd
}

// newWikiCheckCmd builds `veyloom wiki check`, the whole-bundle check of
// docs/design.md 5.13. Every write is checked before it happens; this is
// for what was not written that way, by hand or by another tool.
func newWikiCheckCmd(a *app) *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check [bundle folder...]",
		Short: "Check that wikis follow OKF v0.2",
		Long: "With no folder, checks every wiki this Veyloom keeps, each project's and the skill library, " +
			"against the rules Veyloom writes them by. With folders, checks those bundles against OKF v0.2 itself; " +
			"--strict holds them to Veyloom's rules as well. Exits with status 1 when anything breaks them.",
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := okf.Conformance
			dirs := args
			if len(dirs) == 0 {
				profile = okf.Strict
				var err error
				if dirs, err = ownBundles(a.cfg.Hub.WikiDir); err != nil {
					return err
				}
				if len(dirs) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "no wikis in %s yet\n", a.cfg.Hub.WikiDir)
					return nil
				}
			} else if strict {
				profile = okf.Strict
			}
			found := 0
			for _, dir := range dirs {
				report, err := wiki.CheckDir(dir, profile)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s, %s\n", dir, plural(report.Files, "file"), problemCount(len(report.Problems)))
				for _, p := range report.Problems {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", p)
				}
				found += len(report.Problems)
			}
			if found > 0 {
				cmd.SilenceUsage = true
				return fmt.Errorf("%s found", problemCount(found))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "hold the folders given to Veyloom's own rules too, not only OKF's")
	return cmd
}

// ownBundles lists the wikis under the hub's wiki folder: each project's,
// then the skill library. Archived ones are left alone.
func ownBundles(root string) ([]string, error) {
	var dirs []string
	entries, err := os.ReadDir(filepath.Join(root, "projects"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, "projects", e.Name()))
		}
	}
	slices.Sort(dirs)
	if info, err := os.Stat(filepath.Join(root, "library")); err == nil && info.IsDir() {
		dirs = append(dirs, filepath.Join(root, "library"))
	}
	return dirs, nil
}

func problemCount(n int) string {
	if n == 0 {
		return "no problems"
	}
	return plural(n, "problem")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
