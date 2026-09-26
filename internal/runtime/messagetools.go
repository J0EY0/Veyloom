package runtime

// The tool a member talks with besides its reply (design.md 5.22): it posts
// to the topic its turn is in, or to the room to start something new, and
// the members it names there are woken at once, to work alongside it.
// Chat turns are given it through TurnSpec.ExtraTools.
const MessageToolSend = "send_message"

// MessageToolNames lists the tools members talk with.
var MessageToolNames = []string{MessageToolSend}

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
}

// HandedOnHeading heads what a member summing up the work it handed on is
// told came of it (docs/design.md 5.22). The fake runtime answers a brief
// with it apart.
const HandedOnHeading = "What came of the work you handed on:"
