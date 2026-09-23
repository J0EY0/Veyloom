package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Whatever Claude Code would ask a person, it asks whoever drives it over
// stdio (--permission-prompt-tool stdio): the control protocol its own SDKs
// and the desktop app speak. The CLI writes
//
//	{"type": "control_request", "request_id": "...", "request": {"subtype": "..."}}
//
// and waits for a control_response with the same id on its stdin, success
// or error; a control_cancel_request takes a request back. A turn answers
// two subtypes:
//
//	can_use_tool  permission to use a tool, which is also how AskUserQuestion
//	              puts its questions and ExitPlanMode its plan
//	elicitation   an MCP server's form to fill in or page to open
//
// and tells the CLI, and people, that it cannot answer any other. Checked
// against claude 2.1.85 and 2.1.275 (docs/design.md 4.6).

// claudeControlRequest is the request of a control_request line, fields
// filled according to Subtype.
type claudeControlRequest struct {
	Subtype string `json:"subtype"`
	// can_use_tool: the tool and the input it would run with. Newer CLIs
	// mark the tools that exist to ask a person, such as AskUserQuestion.
	ToolName                string          `json:"tool_name"`
	Input                   json.RawMessage `json:"input"`
	ToolUseID               string          `json:"tool_use_id"`
	RequiresUserInteraction bool            `json:"requires_user_interaction"`
	// elicitation: what the MCP server asks, a form (the default) or a page
	// to open.
	MCPServerName   string          `json:"mcp_server_name"`
	Message         string          `json:"message"`
	Mode            string          `json:"mode"`
	URL             string          `json:"url"`
	RequestedSchema json.RawMessage `json:"requested_schema"`
}

// claudeAllow and claudeDeny are what can_use_tool accepts back.
// UpdatedInput is required on allow: it is the input the tool then runs
// with. ToolUseID lets the CLI tell a repeated answer from a new one.
type claudeAllow struct {
	Behavior     string `json:"behavior"`
	UpdatedInput any    `json:"updatedInput"`
	ToolUseID    string `json:"toolUseID,omitempty"`
}

type claudeDeny struct {
	Behavior  string `json:"behavior"`
	Message   string `json:"message"`
	ToolUseID string `json:"toolUseID,omitempty"`
}

func claudeAllowed(input any, toolUseID string) claudeAllow {
	return claudeAllow{Behavior: "allow", UpdatedInput: input, ToolUseID: toolUseID}
}

func claudeDenied(message, toolUseID string) claudeDeny {
	return claudeDeny{Behavior: "deny", Message: message, ToolUseID: toolUseID}
}

// Claude Code's tools that exist to ask a person: questions, and approval
// of a plan made in plan mode.
const (
	claudeAskTool  = "AskUserQuestion"
	claudePlanTool = "ExitPlanMode"
)

// serve answers a control request on a goroutine of its own, so a person
// taking their time over one leaves the others, and the output, flowing.
// The request's wait ends when the turn does or when the CLI takes the
// request back; either way nobody reads an answer then, and none is sent.
func (t *claudeTurn) serve(id string, raw json.RawMessage) {
	var req claudeControlRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.replyError(id, "malformed request: "+err.Error())
		return
	}
	ctx, stop := context.WithCancel(t.ctx)
	t.controlMu.Lock()
	t.inflight[id] = stop
	t.controlMu.Unlock()
	go func() {
		defer t.withdraw(id)
		var resp any
		switch req.Subtype {
		case "can_use_tool":
			resp = t.canUseTool(ctx, req)
		case "elicitation":
			resp = t.elicit(ctx, req)
		default:
			t.notice(t.ctx, NoticeError, fmt.Sprintf("Claude Code sent a %q request, which Veyloom cannot answer; it was told so", req.Subtype))
			t.replyError(id, "Veyloom does not handle "+req.Subtype+" requests")
			return
		}
		if ctx.Err() == nil {
			t.reply(id, resp)
		}
	}()
}

// withdraw ends the wait of a control request, the CLI having taken it
// back or its answer having been sent. Unknown ids are ignored.
func (t *claudeTurn) withdraw(id string) {
	t.controlMu.Lock()
	stop := t.inflight[id]
	delete(t.inflight, id)
	t.controlMu.Unlock()
	if stop != nil {
		stop()
	}
}

func (t *claudeTurn) reply(id string, resp any) {
	t.in.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": id, "response": resp}})
}

func (t *claudeTurn) replyError(id, message string) {
	t.in.send(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": id, "error": message}})
}

// canUseTool settles a permission request. A person decides, through the
// hub, except in the read-only preset: there the member may read and plan
// but nothing more, as with the other runtimes, so a request to do more is
// turned down on the spot, and people are shown what was asked. Questions
// and plans always reach a person; Veyloom's own tools never need one.
func (t *claudeTurn) canUseTool(ctx context.Context, req claudeControlRequest) any {
	input := req.Input
	if len(input) == 0 || string(input) == "null" {
		input = json.RawMessage("{}")
	}
	switch {
	case req.ToolName == claudeAskTool:
		return t.answerQuestions(ctx, req.ToolUseID, input)
	case req.ToolName == claudePlanTool:
		return t.reviewPlan(ctx, req.ToolUseID, input)
	case isVeyloomTool(req.ToolName):
		// Veyloom's own tools, asked about in plan mode: the wiki is not
		// the project, a read-only member may record what it found, and
		// the hub decides which changes wait for a person.
		return claudeAllowed(input, req.ToolUseID)
	case t.preset == PermissionReadOnly && !req.RequiresUserInteraction:
		t.notice(t.ctx, NoticeWarning, fmt.Sprintf("Claude Code asked to run %s; the member is read-only, so it was turned down without asking anyone",
			describeClaudeUse(req.ToolName, input, t.maxEventBytes)))
		return claudeDenied(claudeReadOnlyRefusal, req.ToolUseID)
	}
	d, err := t.requestApproval(ctx, req.ToolName, string(input))
	switch {
	case err != nil:
		return claudeDenied("the turn ended before anyone decided", req.ToolUseID)
	case d.Allow:
		return claudeAllowed(input, req.ToolUseID)
	}
	return claudeDenied(orDefault(d.Message, "denied by a Veyloom user"), req.ToolUseID)
}

// isVeyloomTool reports whether a tool name is one of the turn's own tools
// on Veyloom's MCP server: every turn's or an optional one, which is there
// only for the turns given it.
func isVeyloomTool(name string) bool {
	for _, tool := range append(append(append([]string(nil), AgentToolNames...), MemoryToolNames...), UpkeepToolNames...) {
		if name == claudeToolName(tool) {
			return true
		}
	}
	return false
}

// claudeReadOnlyRefusal is what Claude Code hears when the read-only
// preset turns a request down.
const claudeReadOnlyRefusal = "This member is read-only in Veyloom: it can read and plan, not change anything or run what needs permission. " +
	"Carry on without it, or say what you would do; a person can switch the member to a preset that allows it."

// claudePlanApprovedReadOnly is what Claude Code hears when a person
// approves its plan in the read-only preset. Allowing ExitPlanMode would
// take the CLI out of plan mode, so the approval goes back in words.
const claudePlanApprovedReadOnly = "The plan is approved. This member stays read-only in Veyloom, so do not start on it: " +
	"end your turn with the plan as your answer. To carry it out, a person switches the member to a preset that can edit files."

// reviewPlan shows the plan Claude Code made in plan mode to people, who
// approve it or send it back.
func (t *claudeTurn) reviewPlan(ctx context.Context, toolUseID string, input json.RawMessage) any {
	d, err := t.requestApproval(ctx, claudePlanTool, string(input))
	switch {
	case err != nil:
		return claudeDenied("the turn ended before anyone looked at the plan", toolUseID)
	case !d.Allow:
		return claudeDenied(orDefault(d.Message, "the plan was not approved"), toolUseID)
	case t.preset == PermissionReadOnly:
		return claudeDenied(claudePlanApprovedReadOnly+suffixMessage(d.Message), toolUseID)
	}
	return claudeAllowed(input, toolUseID)
}

// answerQuestions puts AskUserQuestion's questions to people and tells the
// CLI what they said: allowed, with the answers keyed by question text, one
// string each (several picks joined), as the tool reads them. A person who
// chooses not to answer denies it, which the agent hears as a decline.
func (t *claudeTurn) answerQuestions(ctx context.Context, toolUseID string, input json.RawMessage) any {
	var in struct {
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(input, &in)
	questions := make([]Question, len(in.Questions))
	for i, q := range in.Questions {
		options := make([]QuestionOption, len(q.Options))
		for j, o := range q.Options {
			options[j] = QuestionOption{Label: o.Label, Description: o.Description}
		}
		// The CLI always offers an answer of one's own besides the options.
		questions[i] = Question{ID: strconv.Itoa(i + 1), Header: q.Header, Question: q.Question, Options: options, MultiSelect: q.MultiSelect, Other: true}
	}

	d, err := t.askQuestions(ctx, claudeAskTool, questions)
	if err != nil {
		return claudeDenied("the turn ended before anyone answered", toolUseID)
	}
	if !d.Allow {
		return claudeDenied(orDefault(d.Message, "the person chose not to answer"), toolUseID)
	}
	answers := d.Answers()
	byText := make(map[string]string, len(questions))
	for _, q := range questions {
		if a := answers[q.ID]; len(a) > 0 {
			byText[q.Question] = answerText(a)
		}
	}
	updated := map[string]any{}
	_ = json.Unmarshal(input, &updated)
	updated["answers"] = byText
	return claudeAllowed(updated, toolUseID)
}

// elicit puts an MCP server's request to people, a form to fill in or a
// page to open, and answers with the MCP elicitation result. Content is
// left out unless a form was filled in: the CLI takes no null there.
func (t *claudeTurn) elicit(ctx context.Context, req claudeControlRequest) any {
	var d Decision
	var err error
	if req.Mode == "url" {
		d, err = t.askLink(ctx, elicitationTool, LinkRequest{Server: req.MCPServerName, Message: req.Message, URL: req.URL})
	} else {
		d, err = t.askForm(ctx, elicitationTool, FormRequest{Server: req.MCPServerName, Message: req.Message, Schema: req.RequestedSchema})
	}
	action := elicitationAction(d, err)
	resp := map[string]any{"action": action}
	if content := d.Content(); action == ElicitAccept && req.Mode != "url" && content != nil {
		resp["content"] = content
	}
	return resp
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// suffixMessage appends a person's note to a sentence meant for the agent.
func suffixMessage(message string) string {
	if message == "" {
		return ""
	}
	return " They added: " + message
}
