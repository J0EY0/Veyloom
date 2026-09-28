package hub

import (
	"fmt"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A member's standing instructions (design.md 5.23.1): how the chat works,
// what its tools are for, how it hands work on, keeps the wiki and notes
// what people want kept, and where it works. They change only with who the
// member is and how it works, never from one turn to the next, and so hold
// no count and no time. A runtime that takes its system prompt with every
// run is given them there, after the role card: a compaction cannot take
// them, and they do not pile up in the session's history. One that fixes
// its system prompt when a session starts (Codex) is given them in a brief
// whenever its session has not seen them as they are.

// standing renders the member's standing instructions.
func (b *briefBuilder) standing(in briefInput, project store.Project) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %q, an agent in the team chat of the project %q. Each turn brings you a brief of what is new in the chat; the lines marked with >> are addressed to you, and you reply to them. "+
		"Write to the chat in the language its people write in, what you say as you work included, whatever language these instructions and the briefs are in.\n", in.Member.DisplayName, project.Name)
	sb.WriteString("\nA brief shows what is new since you last looked; what you were told earlier in this session and has not changed is not told again. " +
		"For anything else in this chat, such as earlier messages, another topic, or what an agent did in a turn, the commands it ran and what came of them, use your veyloom tools: " +
		strings.Join(runtime.RoomToolNames, ", ") + ". Topics are numbered; #12 is read with read_topic, which names each agent turn for read_turn. " +
		"A long message a brief or a tool cuts short is read whole with " + runtime.RoomToolReadMessage + ".\n")
	sb.WriteString(relayRules(project.RelayLimit))
	sb.WriteString(dispatchRules(in, project))
	sb.WriteString(practicesLine(in.BuiltinSkills))
	sb.WriteString(reminderRules(project.RelayLimit))
	sb.WriteString(draftRules)
	if b.wikis != nil {
		sb.WriteString(wikiRules(b.wikis.memoryPrefs(), b.trialUses))
	}
	sb.WriteString(workplaceRules(in, project))
	return sb.String()
}

// relayRules tells a member how it talks and hands work on as it goes
// (design.md 5.22). How many more turns agents may wake in the piece of
// work at hand changes by the turn, and is the brief's to say.
func relayRules(limit int) string {
	line := "\nYou can post while you work with " + runtime.MessageToolSend + ": in this topic, or in the room to start something new. "
	if limit < 0 {
		return line + "Writing @ and the person's name reaches their inbox; in this project a member you name is not woken by it, since only people wake members here.\n"
	}
	limits := fmt.Sprintf("%d turns in a row that agents woke and that only talked, doing no work, stop agents waking one another in that piece of work.", idleWakes)
	if limit > 0 {
		limits = fmt.Sprintf("Agents waking one another stop after so many turns of one piece of work, which the brief tells, or sooner after %d turns in a row that only talked, doing no work.", idleWakes)
	}
	return line + "Writing @Name of a member there wakes it at once to work alongside you; writing @ and the person's name reaches their inbox. " +
		"When a person asked you and you hand work on, you are woken once with what the members you handed it to came to, to see to what they need or sum it up for the person: do not wait for them or ask them to report back. " +
		"When a member handed the work to you, what you come to goes back to that member instead, as the brief says. " +
		limits + "\n"
}

// practicesLine points a member to how members split, hand on, check and
// report back work, which is Veyloom's own skill team-practices
// (design.md 5.23.6), when builtin, the skills of Veyloom's the turn has,
// hold it.
func practicesLine(builtin []string) string {
	if !slices.Contains(builtin, teamPractices) {
		return ""
	}
	return "\nHow members split, hand on, check and report back work is Veyloom's skill " + teamPractices + ", which every agent has: " +
		"load it when you hand work on, get work that names no member or may not be yours, take up work handed to you, check another's work, report back, or are stuck.\n"
}

// reminderRules tells a member it can have the hub wake it later
// (design.md 5.23.4). Which reminders it has changes by the turn, and is
// the brief's to say.
func reminderRules(limit int) string {
	line := "\nWhen something is to be done later, such as seeing how CI went or looking at a job left running, " + runtime.MessageToolRemind +
		" has the hub wake you in this topic then, with a note to yourself of what to do: what your runtime times itself ends with your turn. "
	if limit < 0 {
		line += "Since only people wake members here, the person is asked then to let it wake you. "
	} else {
		line += "Its waking you counts as agents waking one another, in the piece of work of the turn that set it. "
	}
	return line + "The brief lists your reminders not yet due, which " + runtime.MessageToolCancelReminder + " takes back; for something to be done every so often, set the next one each time.\n"
}

// draftRules tells a member to leave what only a person does ready for
// one press (design.md 5.23.5).
var draftRules = "\nSome things only a person does: putting a member's work on the main line, giving a member's work up, installing a skill of the library for a member. " +
	runtime.MessageToolDraft + " drafts one on a card for the person to run with one press; when something follows once it is done, say what with then, and you are woken with what came of it.\n"

// wikiRules tells a member how it uses the project's wiki, the skill
// library and the memories. Which skills are installed for it is the
// brief's to say: installing one does not change these.
func wikiRules(prefs store.MemoryPrefs, trialUses int) string {
	return "\nThe project keeps a wiki of what the team has learned: decisions, conventions, facts, pitfalls, what modules are for, what finished topics came to. Look things up in it with search_wiki and read_wiki. " +
		"Write to it only when a person asks you to, now or as a standing rule of this chat: then write_wiki a new page, patch_wiki the page that has it, or deprecate_wiki one that no longer holds; " +
		"the change takes effect at once, and a person can undo it. When a person says a page is wrong, check it against the code, or ask them, set it right and say what you changed. " +
		"related_wiki shows how pages bear on each other, and, given a path of the repository, which pages name it: look before you change a file. " +
		"Every project also shares a skill library, the same tools with scope library: patterns of how tasks went wrong or right, and skills, which people add and install for agents; your runtime loads the ones installed for you when a task calls for them. " +
		fmt.Sprintf("The skills installed for you, which the brief names, you improve as you use them, without being asked: when one proves wrong or short in your task, or you find a better way, "+
			"set it right with patch_wiki (scope library), one focused change to its SKILL.md or a page of its folder, and record what happened as a Pattern page. "+
			"The change reaches every agent the skill is installed for from its next turn, on trial until %d turns have used it and ended well; a person or the skill's team can roll it back.", trialUses) +
		memoryLine(prefs) + "\n"
}

// dispatchRules tells the leader what comes to it that names no one
// (design.md 4.2): nothing for the other members.
func dispatchRules(in briefInput, project store.Project) string {
	if in.Member.ID != project.LeaderID {
		return ""
	}
	return "\nAs the project's leader, you get a person's message to the room that names no member, and one in a topic whose member is gone or turned off: " +
		"take it on yourself, or hand it on with " + runtime.MessageToolSend + ".\n"
}

// workplaceRules says where the member works (design.md 5.21): the leader
// in the project's checkout, and the others, once they have one, each in a
// git worktree of its own. Which branches the others work on changes with
// them, and is the brief's to say.
func workplaceRules(in briefInput, project store.Project) string {
	switch {
	case in.Member.ID == project.LeaderID:
		return "\nYou are the project's leader. You work in the project's checkout itself, where people work too; " +
			"when the others work in git worktrees of their own, you write down with " + runtime.SetupToolSteps + " how a new one is got ready, whenever a person asks you to change it. " +
			"Each of them commits on a branch of its own, veyloom/ and its name, which a person merges onto the main line, from the card you draft with " + runtime.MessageToolDraft + " once its work is done and checked: do not ask them for other branches. " +
			"When they do, commit the files you changed in the checkout yourself, and only those, before your turn ends, with a message saying what the change does: " +
			"changes left there uncommitted are missing from their worktrees, and keep a person from merging work that changes the same files. " +
			"What documents work still on a member's branch, such as the README section for a feature it wrote, goes on that branch with the work: " +
			"ask the member for it, since the checkout would describe what it does not have until a person merges it.\n"
	case worksInOwnWorktree(in):
		return fmt.Sprintf("\nYou work in a git worktree of your own, %s, on the branch %s, made from the project's checkout at %s. "+
			"What you change stays there until a person merges it into the branch the checkout is on: you need not commit, and do not push or switch branches. "+
			"Whatever you leave there is merged as your work, so what you build or run only to check it writes outside the worktree, in a temporary folder, "+
			"or you remove what it wrote before your turn ends. To build on what another member committed, merge its branch into yours rather than copying its files; the brief names their branches.\n",
			in.Dir, in.Member.Branch, project.RepoPath)
	}
	return ""
}

// worksInOwnWorktree says the member works this turn in the git worktree
// of its own.
func worksInOwnWorktree(in briefInput) bool {
	return in.Dir != "" && in.Member.WorktreeDir != "" && in.Dir == in.Member.WorkDir
}

// systemPrompt is what a run is given as its system prompt: the agent's
// role card, then, for a runtime that takes its system prompt with every
// run, the member's standing instructions.
func systemPrompt(roleCard, standing, runtimeName string) string {
	if standing == "" || !runtime.TraitsOf(runtimeName).SystemPromptEachRun {
		return roleCard
	}
	if strings.TrimSpace(roleCard) == "" {
		return strings.TrimSpace(standing)
	}
	return strings.TrimSpace(roleCard) + "\n\n" + strings.TrimSpace(standing)
}
