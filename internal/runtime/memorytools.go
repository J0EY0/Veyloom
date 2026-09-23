package runtime

// The memory tools keep the two memories every turn carries whole
// (design.md 5.16): the project memory, how to work in this project, and
// the personal memory, what the person wants in every project. They travel
// like the wiki tools, their arguments going to the hub as they came.
const (
	MemoryToolRemember = "remember"
	MemoryToolForget   = "forget"
)

// MemoryToolNames lists the memory tools in the order they are presented.
var MemoryToolNames = []string{MemoryToolRemember, MemoryToolForget}

// Scopes of the memory tools.
const (
	MemoryScopeProject  = "project"
	MemoryScopePersonal = "personal"
)

var (
	paramMemoryScope = roomToolParam{
		Name: "scope", Type: "string", Enum: []string{MemoryScopeProject, MemoryScopePersonal},
		Description: "Which memory: project, the default, is how to work in this project; personal is what the person wants in every project.",
	}
	paramMemoryEntries = roomToolParam{
		Name: "entries", Required: true, Description: "The entries, one short line each.",
		Schema: map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}},
	}
)

// memoryToolSpecs are the memory tools as an agent sees them.
var memoryToolSpecs = []roomToolSpec{
	{
		Name: MemoryToolRemember, RawArgs: true,
		Description: "Note in a memory that every turn of every member carries what a person wants kept for later turns: a preference, a rule, a correction of how to work. " +
			"It goes in the project memory; when they mean every project, in the personal memory. What only matters to the conversation at hand is not noted. " +
			"Each entry is one short line in the language the team works in, saying what to do rather than how it came up; the day and who noted it are added for you. " +
			"An entry the memory has already is left as it is. A memory has a budget: when it is full, fold entries together or forget ones that no longer hold first.",
		Params: []roomToolParam{
			paramMemoryEntries,
			{Name: "topic", Type: "integer", Description: "The topic of this chat a person said it in, by number: 12 for #12. Leave it out for the one you are in."},
			paramMemoryScope,
		},
	},
	{
		Name: MemoryToolForget, RawArgs: true,
		Description: "Take entries out of a memory: when a person says one no longer holds, or another entry says it better. " +
			"Give each entry as the memory has it, without the day and who noted it, or a piece of it no other entry has.",
		Params: []roomToolParam{paramMemoryEntries, paramMemoryScope},
	},
}
