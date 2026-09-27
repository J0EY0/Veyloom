package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// The parts of a brief that change now and then (design.md 5.23.1): what
// the project is, the memories, who is in the chat, the others' branches,
// the member's reminders, the skills installed for it, the bundles the
// wiki mounts, the resident pages; and, for a runtime that fixes its
// system prompt when a session starts, the standing instructions. A brief shows one only when
// its session has not seen it as it is: in a new session, after a
// compaction, and when it changed. The session keeps a digest of each part
// it was shown (store.MemberSession.BriefSeen), set once a turn took the
// brief in, as its reading positions are.

// briefPart is one of those parts: key names it in the session's record,
// name in the line telling what a brief left out, gone is what a brief
// says of it when it has nothing in it any more after the session was
// shown it, and changed what goes before it when the session was shown it
// otherwise.
type briefPart struct {
	key, name, gone, changed string
}

var (
	partStanding = briefPart{key: "standing",
		changed: "What follows replaces how this chat works, as you were told it earlier in this session.\n"}
	partAbout     = briefPart{key: "about", name: "about the project", gone: "The project has no description any more.\n"}
	partMemories  = briefPart{key: "memories", name: "the memories", gone: "The memories have no entries any more: what they said no longer holds.\n"}
	partMembers   = briefPart{key: "members", name: "who is in the chat"}
	partBranches  = briefPart{key: "branches", name: "the others' branches"}
	partReminders = briefPart{key: "reminders", name: "your reminders", gone: "You have no reminders not yet due any more.\n"}
	partSkills    = briefPart{key: "skills", name: "your skills", gone: "You have no skills any more.\n"}
	partMounts    = briefPart{key: "mounts", name: "the bundles the wiki mounts", gone: "The project wiki mounts no bundle any more.\n"}
	partResident  = briefPart{key: "resident", name: "the resident pages", gone: "No page of the project wiki is resident any more.\n"}
)

// briefParts decides a brief's parts against what its session saw.
type briefParts struct {
	seen map[string]string
	// now is what the brief shows or leaves out as seen, by part: what the
	// session has seen once it takes the brief in.
	now  map[string]string
	same []string
}

func newBriefParts(seen map[string]string) *briefParts {
	return &briefParts{seen: seen, now: make(map[string]string)}
}

// put writes text, a part of the brief, unless the session saw it as it is.
func (p *briefParts) put(w *briefWriter, part briefPart, text string) {
	sum := digest(text)
	p.now[part.key] = sum
	old, had := p.seen[part.key]
	switch {
	case had && old == sum:
		if text != "" && part.name != "" {
			p.same = append(p.same, part.name)
		}
	case text != "":
		if had && old != "" && part.changed != "" {
			w.sb.WriteString("\n" + part.changed)
		}
		w.sb.WriteString(text)
	case had && part.gone != "":
		w.sb.WriteString("\n" + part.gone)
	}
}

// keep leaves a part as the session saw it, one that could not be read
// this time: failing to read it is no change, least of all its going.
func (p *briefParts) keep(part briefPart) {
	if old, had := p.seen[part.key]; had {
		p.now[part.key] = old
	}
}

// unchanged names the parts the brief left out as seen.
func (p *briefParts) unchanged(w *briefWriter) {
	if len(p.same) > 0 {
		w.sb.WriteString("\nAs you were told earlier in this session, unchanged and not repeated here: " + strings.Join(p.same, ", ") + ".\n")
	}
}

// digest tells a part's text from another's; nothing has none.
func digest(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}
