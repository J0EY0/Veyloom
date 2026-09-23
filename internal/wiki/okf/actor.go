package okf

import (
	"strings"
	"time"
)

// Actors name who did something (§7): an agent or tool as
// "<producer>/<version>", a person as "human:<id>", an automated process as
// "process:<id>".

// Human is the actor for a person.
func Human(id string) string { return "human:" + id }

// Process is the actor for an automated process.
func Process(id string) string { return "process:" + id }

// Agent is the actor for an agent or tool, e.g. Agent("codex", "gpt-5.5").
func Agent(producer, version string) string { return producer + "/" + version }

// IsHuman reports whether actor names a person; only such confirmations
// make a concept human-reviewed.
func IsHuman(actor string) bool { return strings.HasPrefix(actor, "human:") }

// ValidActor reports whether actor follows the convention of §7. A version
// may itself hold slashes, as model names from some providers do.
func ValidActor(actor string) bool {
	if actor == "" || strings.ContainsAny(actor, " \t\r\n") {
		return false
	}
	for _, prefix := range []string{"human:", "process:"} {
		if id, ok := strings.CutPrefix(actor, prefix); ok {
			return id != ""
		}
	}
	producer, version, ok := strings.Cut(actor, "/")
	return ok && producer != "" && version != "" && !strings.Contains(producer, ":")
}

// Tier is how far a concept has been confirmed (§5.3), lowest first.
type Tier int

const (
	Unverified Tier = iota
	MachineConfirmed
	HumanReviewed
)

// String names the tier the way the spec does.
func (t Tier) String() string {
	switch t {
	case HumanReviewed:
		return "human-reviewed"
	case MachineConfirmed:
		return "machine-confirmed"
	}
	return "unverified"
}

// TierOf derives the tier from a concept's verified entries.
func TierOf(verified []Stamp) Tier {
	tier := Unverified
	for _, v := range verified {
		if IsHuman(v.By) {
			return HumanReviewed
		}
		if v.By != "" {
			tier = MachineConfirmed
		}
	}
	return tier
}

// LastVerified is the most recent confirmation, zero when there is none.
func LastVerified(verified []Stamp) time.Time {
	var last time.Time
	for _, v := range verified {
		if v.At.After(last) {
			last = v.At
		}
	}
	return last
}
