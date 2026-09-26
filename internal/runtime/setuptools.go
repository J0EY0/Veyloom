package runtime

// The project leader's tool (design.md 5.21). A member's git worktree
// holds only what is committed to git; the leader writes down what makes
// up the rest, the files to copy into a new worktree from the project's
// checkout and one command to run in it, and the machine does that for
// every member's new worktree. The leader's turns are given it through
// TurnSpec.ExtraTools.
const SetupToolSteps = "set_workspace_setup"

// SetupToolNames lists the leader's tools.
var SetupToolNames = []string{SetupToolSteps}

// setupToolSpecs are the leader's tools as an agent sees them. Their
// arguments go to the hub as they came.
var setupToolSpecs = []roomToolSpec{
	{
		Name: SetupToolSteps, RawArgs: true,
		Description: "Write down how a new git worktree of this project is got ready for a member to work in. A worktree holds only what is committed to git: " +
			"no installed dependencies, no local settings such as .env, none of the files git ignores or does not track. " +
			"copy names what to copy into it from the project's checkout; run is one shell command run in it afterwards, such as installing dependencies. " +
			"What you write replaces what was written before; call it with neither when a worktree needs nothing more. " +
			"A command takes effect once a person approves it, unless you run commands without asking.",
		Params: []roomToolParam{
			{
				Name: "copy", Description: "Files or folders to copy, relative to the project's checkout; patterns such as .env* allowed. What a worktree has already is left alone.",
				Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			{Name: "run", Type: "string", Description: "One shell command run in each new worktree after the copies; $VEYLOOM_REPO is the project's checkout. Leave it out for none."},
		},
	},
}
