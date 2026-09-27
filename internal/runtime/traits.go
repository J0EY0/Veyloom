package runtime

import "sync"

// Traits are what the hub and the web client need to know of how a runtime
// takes its turns, beyond what every runtime does alike: the one table of
// it (design.md 5.23.9). What only its runner needs, such as how it names
// sessions or takes skills, stays in the runner; design.md has the whole
// table.
type Traits struct {
	// SystemPromptEachRun says the system prompt is given with every run of
	// a session: it may change from one turn to the next, and a compaction
	// of the session does not touch it. Claude Code and Pi append it anew
	// on every start; Codex fixes a thread's when the thread starts and
	// ignores what a resume passes (0.155.1), so what must reach every turn
	// of a Codex session goes in its briefs instead, and a changed role
	// card takes a new session (design.md 5.6).
	SystemPromptEachRun bool `json:"system_prompt_each_run"`
	// Steer says the runtime takes input while a turn runs (Turn.Steer):
	// Claude Code as a user message on its input, Codex with turn/steer,
	// Pi with its steer command (design.md 5.23.2).
	Steer bool `json:"steer"`
	// WikiWhenReadOnly says the runtime writes the wiki with the wiki tools
	// in the read-only preset too: Claude Code in plan mode and Pi do;
	// Codex in its read-only sandbox reads the turns and writes nothing it
	// is not told to in so many words (design.md 5.12, tried 2026-09-22),
	// so it keeps no wiki.
	WikiWhenReadOnly bool `json:"wiki_when_read_only"`
}

// TraitsOf are the traits of the runtime named name. One not known is
// taken to fix its system prompt when a session starts and to keep no wiki
// read-only, which is the side that loses nothing.
func TraitsOf(name string) Traits {
	switch name {
	case "claude", "pi", "fake":
		return Traits{SystemPromptEachRun: true, Steer: true, WikiWhenReadOnly: true}
	case "codex":
		return Traits{Steer: true}
	}
	return Traits{}
}

// AllTraits are the traits of every runtime there is a runner for, by name.
func AllTraits() map[string]Traits {
	out := make(map[string]Traits)
	for _, name := range runnerNames() {
		out[name] = TraitsOf(name)
	}
	return out
}

// runnerNames are the names of the runtimes there is a runner for, found
// once.
var runnerNames = sync.OnceValue(func() []string {
	var names []string
	for name := range BuiltinRunners() {
		names = append(names, name)
	}
	return names
})
