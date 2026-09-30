package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The skills a person installed for an agent go with every turn of its
// (docs/design.md 5.11, 5.15), and Veyloom's own with every turn of every
// agent (5.23.6): the ones its runtime may load, as Agent Skills has them.
// Which of the library's the turn used is read off its tool calls
// (5.10), for the teams owning them to see where their skills were used
// and how that went.

// skillSet is what a turn of the agent is given of skills: Veyloom's own,
// which every agent has (design.md 5.23.6), and the library's installed
// for it (5.15) that its runtime may load; nil when there is nothing to
// give. One of the library's with the name of one of Veyloom's own cannot
// go by it too, and is left out.
func (m *TurnManager) skillSet(ctx context.Context, agent store.Agent) *runtime.SkillSet {
	installed := m.librarySkills(ctx, agent)
	if len(m.builtin)+len(installed) == 0 {
		return nil
	}
	set := &runtime.SkillSet{Skills: make([]runtime.Skill, 0, len(m.builtin)+len(installed))}
	set.Skills = append(set.Skills, m.builtin...)
	for _, s := range installed {
		if m.isBuiltin(s.Name) {
			m.logger.Warn("a skill of the library has the name of one of Veyloom's own", "skill", s.Name, "agent", agent.ID)
			continue
		}
		set.Skills = append(set.Skills, s)
	}
	set.Hash = skillHash(set.Skills)
	return set
}

// librarySkills are the library's skills installed for agent that its
// runtime may load.
func (m *TurnManager) librarySkills(ctx context.Context, agent store.Agent) []runtime.Skill {
	if len(agent.Skills) == 0 {
		return nil
	}
	b, err := m.wikis.library(ctx)
	if err != nil {
		return nil
	}
	m.wikis.sync(ctx, b, "", "")
	return m.skillsOf(b, agent.Runtime, func(name string) bool { return slices.Contains(agent.Skills, name) })
}

// skillsOf are the skills of b a runtime may load that keep keeps, as
// Agent Skills has them: their text files as text, the rest as they are.
func (m *TurnManager) skillsOf(b *wiki.Bundle, runtimeName string, keep func(string) bool) []runtime.Skill {
	var out []runtime.Skill
	for _, s := range b.Skills(runtimeName) {
		name := wiki.SkillName(s.Path)
		if !keep(name) {
			continue
		}
		projected, err := b.ProjectSkill(name)
		if err != nil {
			m.logger.Warn("project a skill", "skill", name, "err", err)
			continue
		}
		skill := runtime.Skill{Name: name, Files: map[string]string{}}
		for p, data := range projected.Files {
			if utf8.Valid(data) {
				skill.Files[p] = string(data)
				continue
			}
			if skill.Blobs == nil {
				skill.Blobs = map[string][]byte{}
			}
			skill.Blobs[p] = data
		}
		out = append(out, skill)
	}
	return out
}

// skillHash names a set by what it gives, which skill as which, each
// file and what is in it, so a machine that wrote it before need not again.
func skillHash(skills []runtime.Skill) string {
	hash := sha256.New()
	write := func(parts ...string) {
		for _, part := range parts {
			hash.Write([]byte(strconv.Itoa(len(part)) + ":" + part))
		}
	}
	for _, s := range skills {
		scope := "library"
		if s.Builtin {
			scope = "builtin"
		}
		for _, p := range slices.Sorted(maps.Keys(s.Files)) {
			write(scope, s.Name, p, s.Files[p])
		}
		for _, p := range slices.Sorted(maps.Keys(s.Blobs)) {
			write(scope+" blob", s.Name, p, string(s.Blobs[p]))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// noteSkillUse keeps what an event of at says of the skills it used,
// with at.mu held. A call reaching for a skill counts once its result
// comes back and the runtime did not refuse it: Claude Code refuses a
// skill kept from the model, and a failed read of a skill's file read
// nothing. A runtime that does not name its calls has its call count at
// once.
func (m *TurnManager) noteSkillUse(at *activeTurn, ev runtime.Event) {
	use := func(names []string) {
		for _, name := range names {
			if !slices.Contains(at.skillsUsed, name) {
				at.skillsUsed = append(at.skillsUsed, name)
			}
		}
	}
	switch ev.Kind {
	case runtime.EventToolCall:
		names := skillsIn(ev, at.spec.Skills)
		switch {
		case len(names) == 0:
		case ev.CallID == "":
			use(names)
		default:
			if at.skillCalls == nil {
				at.skillCalls = map[string][]string{}
			}
			at.skillCalls[ev.CallID] = names
		}
	case runtime.EventToolResult:
		names, ok := at.skillCalls[ev.CallID]
		if !ok {
			return
		}
		delete(at.skillCalls, ev.CallID)
		if !ev.Failed {
			use(names)
		}
	}
}

// skillsIn names the skills of set a tool call used. Claude Code loads one
// with its Skill tool, by the name the plugin gives it; the other runtimes
// read its files where the machine wrote the set, in a directory named for
// its hash. Reading any file of a skill counts, SKILL.md or what it refers
// to. A skill the machine gave another name, since a person's own skill
// has its name there, counts by that name too.
func skillsIn(ev runtime.Event, set *runtime.SkillSet) []string {
	if set == nil || ev.Kind != runtime.EventToolCall {
		return nil
	}
	var called string
	if ev.Tool == "Skill" {
		var input map[string]any
		if json.Unmarshal([]byte(ev.Input), &input) == nil {
			for _, key := range []string{"skill", "command", "name"} {
				if s, ok := input[key].(string); ok {
					called = strings.TrimPrefix(strings.TrimSpace(s), runtime.SkillPlugin+":")
					break
				}
			}
		}
	}
	inSet := afterSet(ev.Input, set.Hash)
	var used []string
	for _, s := range set.Skills {
		if s.Builtin {
			// Veyloom's own: no trial of the library's to count it for.
			continue
		}
		alias := runtime.SkillAlias(s.Name)
		if called != "" && (s.Name == called || alias == called) ||
			strings.HasPrefix(inSet, "/skills/"+s.Name+"/") || strings.HasPrefix(inSet, "/skills/"+alias+"/") {
			used = append(used, s.Name)
		}
	}
	return used
}

// afterSet is what follows a set's directory in a tool call's input: the
// directory is named for the set's hash, then more hex when the machine
// renamed some of its skills. Empty when the input names no such place.
func afterSet(input, hash string) string {
	i := strings.Index(input, hash)
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(input[i+len(hash):], "0123456789abcdef")
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	return rest
}

// transcriptSpec is a turn's spec as its transcript keeps it: the skills
// by name only, their text being the library's to keep.
func transcriptSpec(spec runtime.TurnSpec) *runtime.TurnSpec {
	if spec.Skills != nil {
		named := &runtime.SkillSet{Hash: spec.Skills.Hash}
		for _, s := range spec.Skills.Skills {
			named.Skills = append(named.Skills, runtime.Skill{Builtin: s.Builtin, Name: s.Name})
		}
		spec.Skills = named
	}
	return &spec
}
