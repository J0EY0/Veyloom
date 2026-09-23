package wiki

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// CheckReport is what a check of a whole bundle found (design.md 5.13):
// every markdown file held to a profile of OKF, the way a write is before
// it happens.
type CheckReport struct {
	// Dir is the bundle checked.
	Dir string
	// Files counts the markdown files looked at: concepts, indexes, logs.
	Files int
	// Problems are what breaks the profile, by path.
	Problems []okf.Problem
}

// CheckDir checks every markdown file of the bundle in dir against
// profile. Hidden files and folders, git's among them, are no part of a
// bundle and are skipped; so is any file that is not markdown.
func CheckDir(dir string, profile okf.Profile) (CheckReport, error) {
	report := CheckReport{Dir: dir}
	info, err := os.Stat(dir)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, fmt.Errorf("%s is not a folder", dir)
	}
	err = filepath.WalkDir(dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, fp)
		p := "/" + filepath.ToSlash(rel)
		report.Files++
		report.Problems = append(report.Problems, okf.CheckFile(p, data, strings.Count(p, "/") == 1, profile)...)
		return nil
	})
	slices.SortStableFunc(report.Problems, func(x, y okf.Problem) int { return strings.Compare(x.Path, y.Path) })
	return report, err
}
