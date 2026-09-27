package runtime

// The tools a member talks with besides its reply, which chat turns are
// given through TurnSpec.ExtraTools. send_message (design.md 5.22) posts to
// the topic its turn is in, or to the room to start something new, and the
// members it names there are woken at once, to work alongside it.
// set_reminder has the hub wake the member later in its topic, and
// cancel_reminder takes such a reminder back (design.md 5.23.4).
// draft_action drafts what only a person does, for them to do with one
// press (design.md 5.23.5).
const (
	MessageToolSend           = "send_message"
	MessageToolRemind         = "set_reminder"
	MessageToolCancelReminder = "cancel_reminder"
	MessageToolDraft          = "draft_action"
)

// MessageToolNames lists the tools members talk with.
var MessageToolNames = []string{MessageToolSend, MessageToolRemind, MessageToolCancelReminder, MessageToolDraft}

// messageToolSpecs are the tools members talk with as an agent sees them.
// Their arguments go to the hub as they came.
var messageToolSpecs = []roomToolSpec{
	{
		Name: MessageToolSend, RawArgs: true,
		Description: "Post a message now, while you keep working: to this topic, or to the project's room to start something new. " +
			"Writing @Name of a member wakes it at once to work alongside you: in this topic it joins here; in the room it gets a topic of its own that your message starts. " +
			"Writing @ and the person's name reaches their inbox. Your reply still ends up in this topic as usual; " +
			"use this for what cannot wait for your reply, or to hand work to others. " +
			"Agents waking one another is limited, so do not wake a member only to chat or to thank it; when you are stuck, say so and mention the person.",
		Params: []roomToolParam{
			{Name: "text", Type: "string", Required: true, Description: "The message, in markdown."},
			{Name: "to", Type: "string", Enum: []string{"topic", "room"}, Description: "topic (the default) posts in the topic this turn is in; room posts in the project's room, starting something new."},
			{Name: "title", Type: "string", Description: "When the message hands work on: a few words naming that task, in the language the chat is written in, such as \"Add tests for tags\". The project's task board shows it for the members the message wakes."},
		},
	},
	{
		Name: MessageToolRemind, RawArgs: true,
		Description: "Have Veyloom wake you later, here in this topic, to go on with something: a build or CI to wait for, a usage limit coming back, a person asking to be reminded. " +
			"When it comes due you get a turn whose brief carries your note. It counts as waking yourself, which is limited like agents waking one another: " +
			"do not use it to look again every few minutes, and for work that comes back regularly, set the next reminder once this one comes due. People see it in the topic and can cancel it.",
		Params: []roomToolParam{
			{Name: "note", Type: "string", Required: true, Description: "What to do then, written for yourself: it is what you will be told of why you were woken."},
			{Name: "in", Type: "string", Description: "How long from now, 1m to 30d: 30m, 2h, 1d, 1h30m. Give this or at."},
			{Name: "at", Type: "string", Description: "When, as an RFC 3339 time with its zone, such as 2026-09-28T09:00:00+08:00, 1 minute to 30 days from now. Give this or in."},
		},
	},
	{
		Name: MessageToolDraft, RawArgs: true,
		Description: "Draft something only a person does, ready for them to do with one press, rather than asking them to go and do it: " +
			"put a member's work on the main line (merge), give a member's work up (set_aside), or install a skill of the library for a member (install_skill). " +
			"A card in this topic shows it to the person. With then, you are woken with what came of it once they have run it.",
		Params: []roomToolParam{
			{Name: "kind", Type: "string", Required: true, Enum: []string{"merge", "set_aside", "install_skill"}, Description: "What the person is to do."},
			{Name: "member", Type: "string", Description: "The member whose work it is, or whom the skill is for, by name; you when left out."},
			{Name: "message", Type: "string", Description: "merge: the commit message, its first line saying what the work does."},
			{Name: "reason", Type: "string", Description: "set_aside: why the work is given up."},
			{Name: "skill", Type: "string", Description: "install_skill: the skill's name in the library."},
			{Name: "then", Type: "string", Description: "What you will do once it is done, written for yourself: you are woken then, with what came of it and this note. Leave it out when nothing follows."},
		},
	},
	{
		Name: MessageToolCancelReminder, RawArgs: true,
		Description: "Take back a reminder of yours that has not come due, by its id.",
		Params: []roomToolParam{
			{Name: "id", Type: "string", Required: true, Description: "The reminder's id, as set_reminder or your brief gave it."},
		},
	},
}

// ReplyAsk is what a turn that ended without a word in its topic is asked,
// once, in the session it ran in (docs/design.md 5.24). The fake runtime
// answers it apart.
const ReplyAsk = "Your turn ended without a word in this topic, so whoever asked here does not know what came of it. " +
	"Reply now, in one short message: what you did and what came of it, or that there was nothing to do. Start no new work."

// HandedOnHeading heads what a member summing up the work it handed on is
// told came of it (docs/design.md 5.22). The fake runtime answers a brief
// with it apart.
const HandedOnHeading = "What came of the work you handed on:"
