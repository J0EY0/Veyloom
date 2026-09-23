package runtime

import "slices"

// The wiki maintainer's tools (design.md 5.12, 5.15). A project's
// maintainer goes over what its chat did, and what other projects' turns
// did with the skills its team owns; its upkeep turns get tools beyond
// every turn's to list those turns, which only list what the hub lets the
// maintainer see (every turn reads one with read_turn), to roll back a
// change on trial to a skill its team owns, and to confirm a page of the
// wiki it checked and found right (5.16). A turn is given them through
// TurnSpec.ExtraTools.
const (
	UpkeepToolListTurns = "list_turns"
	UpkeepToolRollback  = "rollback_skill"
	UpkeepToolConfirm   = "confirm_wiki"
)

// UpkeepToolNames lists the maintainer's tools in the order presented.
var UpkeepToolNames = []string{UpkeepToolListTurns, UpkeepToolRollback, UpkeepToolConfirm}

// upkeepToolSpecs are the maintainer's tools as an agent sees them. Their
// arguments go to the hub as they came.
var upkeepToolSpecs = []roomToolSpec{
	{
		Name: UpkeepToolListTurns, ReadOnly: true, RawArgs: true,
		Description: "List finished turns of this project's chat, newest first: each turn's id, topic, member, result, the files it changed and the library skills it used, " +
			"and whether a wiki upkeep has gone over it. With skills true, list instead the turns of other projects that used a skill this project's team owns. " +
			"Read one with read_turn.",
		Params: []roomToolParam{
			{Name: "topic", Type: "integer", Description: "Only the turns of this topic of the project's chat, by number: 12 for #12."},
			{Name: "skills", Type: "boolean", Description: "List other projects' turns that used this team's skills instead of this project's own."},
			{Name: "before", Type: "string", Description: "Only turns that started before this turn, by its id: the last id of an earlier page."},
			paramLimit,
		},
	},
	{
		Name: UpkeepToolRollback, RawArgs: true,
		Description: "Roll a skill this project's team owns back to the version it had before its trial, when an agent's change to it made things worse: " +
			"the turns that used it since, and what people said after them, show it. It is a commit of its own in the skill library, with your reason in the log; " +
			"pattern pages stay. Leave a trial that goes well alone: it is kept once enough turns have used it.",
		Params: []roomToolParam{
			{Name: "skill", Type: "string", Required: true, Description: "The skill's name, like go-table-tests."},
			{Name: "reason", Type: "string", Required: true, Description: "What went worse with the change, in a sentence or two: people and later upkeeps read it."},
		},
	},
	{
		Name: UpkeepToolConfirm, RawArgs: true,
		Description: "Confirm that a page of this project's wiki still holds as written, once you have checked it against the repository and what the chat said since: " +
			"it is stamped as confirmed by you, which counts as checking it, and it leaves the pages to check until something calls for it again. " +
			"A page that no longer holds you set right with patch_wiki, or deprecate with deprecate_wiki, instead.",
		Params: []roomToolParam{
			{Name: "path", Type: "string", Required: true, Description: "The page's path from the wiki's root, like /decisions/approvals-payload-json.md."},
		},
	},
}

// optionalToolSpecs are the tools only some turns get, by ExtraTools: the
// memory tools while the person uses a memory, the maintainer's for its
// upkeep turns.
var optionalToolSpecs = append(append([]roomToolSpec{}, memoryToolSpecs...), upkeepToolSpecs...)

// isOptionalTool reports whether name is one of the tools only some turns
// get.
func isOptionalTool(name string) bool {
	return slices.ContainsFunc(optionalToolSpecs, func(s roomToolSpec) bool { return s.Name == name })
}

// turnToolSpecs are the tools of a turn given extra: every turn's, then
// the optional ones it names, in their order.
func turnToolSpecs(extra []string) []roomToolSpec {
	specs := append([]roomToolSpec(nil), agentToolSpecs...)
	for _, s := range optionalToolSpecs {
		if slices.Contains(extra, s.Name) {
			specs = append(specs, s)
		}
	}
	return specs
}

// turnToolNames names the tools of a turn given extra.
func turnToolNames(extra []string) []string {
	specs := turnToolSpecs(extra)
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	return names
}
