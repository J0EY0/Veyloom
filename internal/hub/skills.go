package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The skills a person installed for an agent go with every turn of its
// (docs/design.md 5.11, 5.15): the ones its runtime may load, as Agent
// Skills has them. Which of them the turn used is read off its tool calls
// (5.10), for the teams owning them to see where their skills were used
// and how that went.

// skillSet is what a turn of the agent is given of the library: the skills
// installed for it that its runtime may load; nil when there is nothing to
// give.
func (m *TurnManager) skillSet(ctx context.Context, agent store.Agent) *runtime.SkillSet {
	if len(agent.Skills) == 0 {
		return nil
	}
	b, err := m.wikis.library(ctx)
	if err != nil {
		return nil
	}
	m.wikis.sync(ctx, b, "", "")
	set := &runtime.SkillSet{}
	hash := sha256.New()
	for _, s := range b.Skills(agent.Runtime) {
		name := wiki.SkillName(s.Path)
		if !slices.Contains(agent.Skills, name) {
			continue
		}
		projected, err := b.ProjectSkill(name)
		if err != nil {
			m.logger.Warn("project a skill", "skill", name, "err", err)
			continue
		}
		paths := make([]string, 0, len(projected.Files))
		for p, data := range projected.Files {
			// Text only: a runtime reads skills, and the set travels as JSON.
			if utf8.Valid(data) {
				paths = append(paths, p)
			}
		}
		slices.Sort(paths)
		skill := runtime.Skill{Name: name, Files: make(map[string]string, len(paths))}
		for _, p := range paths {
			skill.Files[p] = string(projected.Files[p])
			for _, part := range []string{name, p, skill.Files[p]} {
				hash.Write([]byte(strconv.Itoa(len(part)) + ":" + part))
			}
		}
		set.Skills = append(set.Skills, skill)
	}
	if len(set.Skills) == 0 {
		return nil
	}
	set.Hash = hex.EncodeToString(hash.Sum(nil))[:16]
	return set
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
			named.Skills = append(named.Skills, runtime.Skill{Name: s.Name})
		}
		spec.Skills = named
	}
	return &spec
}
