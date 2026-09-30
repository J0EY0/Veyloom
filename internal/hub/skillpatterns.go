package hub

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Patterns and the skills they are about (docs/design.md 5.10, 5.15). In
// WikiSkill a skill changes by what the wiki of patterns says, and records
// the patterns it came from; here whoever changes a skill is shown the
// patterns about it, and a turn that records a pattern about a skill it
// changed notes the pattern among the skill's sources.

// patternsTold caps the patterns named with a skill.
const patternsTold = 8

// skillPatterns are the pattern pages about the skill called name, newest
// first: those linking to it, and those it cites as its sources.
func skillPatterns(b *wiki.Bundle, name string) []wiki.Summary {
	var paths []string
	for _, from := range b.Backlinks(wiki.SkillPath(name)) {
		if strings.HasPrefix(from, "/patterns/") {
			paths = append(paths, from)
		}
	}
	if page, err := b.Page(wiki.SkillPath(name)); err == nil {
		for _, src := range page.Doc.Sources() {
			if strings.HasPrefix(src.Resource, "/patterns/") && !slices.Contains(paths, src.Resource) {
				paths = append(paths, src.Resource)
			}
		}
	}
	var out []wiki.Summary
	for _, p := range paths {
		if page, err := b.Page(p); err == nil && page.Status != okf.Deprecated {
			out = append(out, page.Summary)
		}
	}
	slices.SortFunc(out, func(x, y wiki.Summary) int { return y.Modified.Compare(x.Modified) })
	return out
}

// patternsText names the patterns about a skill, for an agent about to
// change it.
func patternsText(patterns []wiki.Summary) string {
	if len(patterns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("(patterns about this skill, newest first; read the ones that bear on a change before making it:\n")
	for i, s := range patterns {
		if i == patternsTold {
			fmt.Fprintf(&b, "- and %d more\n", len(patterns)-i)
			break
		}
		fmt.Fprintf(&b, "- %s: %s", s.Path, s.Title)
		if d := strings.TrimSpace(s.Description); d != "" {
			b.WriteString(": " + excerpt(d, descriptionExcerpt))
		}
		b.WriteString("\n")
	}
	b.WriteString(")\n")
	return b.String()
}

// patternsAsSources notes, on each skill the turn changed, the pattern
// pages it wrote that link to that skill, as the skill's sources: what the
// skill draws on, WikiSkill's PURPOSE.md.
func patternsAsSources(tw *turnWiki) error {
	pending := tw.writer.Pending()
	changed := map[string]bool{}
	for _, p := range pending {
		if name := wiki.SkillOfFile(p); name != "" {
			changed[name] = true
		}
	}
	if len(changed) == 0 {
		return nil
	}
	for _, p := range pending {
		if !strings.HasPrefix(p, "/patterns/") {
			continue
		}
		page, err := tw.bundle.Page(p)
		if err != nil {
			continue
		}
		var about []string
		for _, l := range okf.Links(page.Doc.Body()) {
			to, ok := okf.Resolve(p, l.Target)
			if name := wiki.SkillOfFile(to); ok && changed[name] && !slices.Contains(about, name) {
				about = append(about, name)
			}
		}
		for _, name := range about {
			src := okf.Source{Resource: page.Path, Title: page.Title}
			if _, err := tw.writer.AddSource(wiki.SkillPath(name), src); err != nil {
				return fmt.Errorf("note %s among the sources of %s: %w", path.Base(page.Path), name, err)
			}
		}
	}
	return nil
}
