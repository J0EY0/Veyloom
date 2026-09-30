package hub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The changes to a skill that were rolled back (docs/design.md 5.15), for
// whoever changes the skill next. WikiSkill keeps the proposals it refused
// so that none is made again; here an agent improving a skill is told the
// changes people, or its team's maintainer, rolled back, why, and what
// they were, read from the library's history.

// Limits of what is told of a skill's rollbacks.
const (
	rollbacksTold = 3  // the latest ones
	undoneLines   = 40 // of the latest one's diff
)

// skillRollback is one change rolled back: when, by whom and why, and the
// change itself, as a diff of the skill's folder.
type skillRollback struct {
	at         time.Time
	by, reason string
	undone     string
}

// skillRollbacks lists the latest rollbacks of the skill called name,
// newest first, at most limit; with undone, the newest one's change undone
// too.
func skillRollbacks(ctx context.Context, b *wiki.Bundle, name string, limit int, undone bool) []skillRollback {
	commits, err := b.SkillHistory(ctx, name, 200)
	if err != nil {
		return nil
	}
	prefix := "Roll back " + name + " to "
	var out []skillRollback
	for _, c := range commits {
		base, ok := strings.CutPrefix(c.Subject, prefix)
		if !ok {
			continue
		}
		r := skillRollback{at: c.At, by: c.Author}
		for _, ch := range c.Changes {
			if _, why, ok := strings.Cut(ch.Text, " by "+c.Author+": "); ok && ch.Kind == okf.LogRevert {
				r.reason = why
			}
		}
		if undone && len(out) == 0 {
			r.undone, _ = b.SkillDiff(ctx, name, base, c.SHA+"^")
		}
		if out = append(out, r); len(out) == limit {
			break
		}
	}
	return out
}

// rollbacksText tells an agent about to change a skill what was rolled
// back of it before: each rollback on a line, the latest change undone
// under it, cut short.
func rollbacksText(rollbacks []skillRollback) string {
	if len(rollbacks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("(changes to this skill were rolled back before, newest first; do not make one of them again unless something new calls for it:\n")
	for i, r := range rollbacks {
		fmt.Fprintf(&b, "- %s by %s", r.at.Local().Format("2006-01-02"), r.by)
		if r.reason != "" {
			b.WriteString(": " + r.reason)
		}
		b.WriteString("\n")
		if i == 0 && strings.TrimSpace(r.undone) != "" {
			b.WriteString("  the change rolled back:\n")
			b.WriteString(indentDiff(r.undone, undoneLines))
		}
	}
	b.WriteString(")\n")
	return b.String()
}

// indentDiff is a diff indented under a list item, without the lines that
// only name blobs, at most max lines.
func indentDiff(diff string, max int) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(line, "index ") {
			continue
		}
		lines = append(lines, line)
	}
	var b strings.Builder
	for i, line := range lines {
		if i == max {
			fmt.Fprintf(&b, "    (%d more lines)\n", len(lines)-i)
			break
		}
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}
