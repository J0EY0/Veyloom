package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// claudeLine is the union of the stream-json records the runner reads.
// Fields are filled according to Type.
type claudeLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	// ParentToolUseID is set on a subagent's lines (an Agent tool's): what
	// it does is part of the turn, what it says is not the reply.
	ParentToolUseID *string `json:"parent_tool_use_id"`
	Model           string  `json:"model"`
	// Status is set on system/status lines, "compacting" while the CLI
	// compacts the session, and on task_notification lines, how the
	// background task ended.
	Status  string             `json:"status"`
	Message *claudeMessage     `json:"message"`
	Event   *claudeStreamEvent `json:"event"`
	// control_request and control_cancel_request lines (claude_control.go).
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	// init and status lines: the permission mode the CLI is in.
	PermissionMode string `json:"permissionMode"`
	// init lines: the MCP servers the CLI started, and how that went.
	MCPServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	// hook_response lines: which hook ran for what, how it ended and what
	// it said. What it said is text or JSON, depending on the hook.
	HookName  string          `json:"hook_name"`
	HookEvent string          `json:"hook_event"`
	Outcome   string          `json:"outcome"`
	Stdout    json.RawMessage `json:"stdout"`
	Stderr    json.RawMessage `json:"stderr"`
	Output    json.RawMessage `json:"output"`
	// task_notification lines: what the background task did.
	Summary string `json:"summary"`
	// elicitation_complete lines: an MCP server saying the page it asked a
	// person to open has done its job.
	MCPServerName string `json:"mcp_server_name"`
	ElicitationID string `json:"elicitation_id"`
	// result records
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	// Errors is where the CLI puts what went wrong before the model ran,
	// such as a session it cannot find; Result is empty then.
	Errors     []string     `json:"errors"`
	DurationMS int64        `json:"duration_ms"`
	NumTurns   int          `json:"num_turns"`
	Usage      *claudeUsage `json:"usage"`
	// PermissionDenials lists, on a result, the tool uses the CLI turned
	// down by its own rules, with nobody asked.
	PermissionDenials []claudeDenial `json:"permission_denials"`
	// system/api_retry records: which attempt failed, how many there may
	// be, when the next one comes and what went wrong.
	Attempt      int             `json:"attempt"`
	MaxRetries   int             `json:"max_retries"`
	RetryDelayMS float64         `json:"retry_delay_ms"`
	ErrorStatus  *int            `json:"error_status"`
	Error        json.RawMessage `json:"error"`
}

// claudeDenial is one tool use the CLI turned down, on its own or as it was
// answered.
type claudeDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// claudeUsage is the token count of a whole run, summed by Claude Code over
// every model call in it. Anthropic reports cached input apart from fresh
// input, so the parts map straight across.
type claudeUsage struct {
	InputTokens              float64 `json:"input_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type claudeStreamEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// claudeParser turns stream-json records into events and collects what
// the result needs. It keeps the tool_use id → name map so tool results
// can be attributed, and the reply text as a fallback should the result
// record be missing.
type claudeParser struct {
	cfg       ClaudeConfig
	emit      func(Event)
	toolNames map[string]string
	// edits are the files edit tools are about to write, by tool_use id:
	// written once the tool reports it went through, which a denial stops.
	edits map[string]string
	// askedIDs are the tool uses the CLI asked about over the control
	// protocol: whatever became of them was a person's answer, or the
	// preset's, and people have seen it.
	askedIDs  map[string]bool
	mode      string
	sessionID string
	text      strings.Builder
	result    *claudeLine
	malformed int
}

func newClaudeParser(cfg ClaudeConfig, emit func(Event)) *claudeParser {
	return &claudeParser{cfg: cfg, emit: emit, toolNames: make(map[string]string), edits: make(map[string]string), askedIDs: make(map[string]bool)}
}

// asked notes a tool use the CLI asked permission for.
func (p *claudeParser) asked(toolUseID string) {
	if toolUseID != "" {
		p.askedIDs[toolUseID] = true
	}
}

// handle takes one output record other than the control protocol's.
func (p *claudeParser) handle(line claudeLine) {
	// The session is reported the moment it is known, and again should
	// the CLI rename it when resuming, as older versions did. Only lines of
	// a session that exists count: a result that says the session could
	// not be found carries a made-up id of its own.
	if line.SessionID != "" && line.SessionID != p.sessionID && !(line.Type == "result" && line.IsError) {
		p.sessionID = line.SessionID
		p.emit(Event{Kind: EventSession, SessionRef: line.SessionID})
	}

	switch line.Type {
	case "system":
		switch line.Subtype {
		case "init":
			text := "session started"
			if line.Model != "" {
				text += " (model " + line.Model + ")"
			}
			p.emit(Event{Kind: EventStatus, Text: text})
			p.mode = line.PermissionMode
			for _, s := range line.MCPServers {
				switch s.Status {
				case "failed":
					p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: fmt.Sprintf("Claude Code: MCP server %q failed to start", s.Name)})
				case "needs-auth":
					p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: fmt.Sprintf("Claude Code: MCP server %q needs signing in first", s.Name)})
				}
			}
		case "status":
			if line.Status == "compacting" {
				p.emit(Event{Kind: EventCompaction, Phase: CompactionStart})
			}
			// Approving a plan, for one, takes the CLI out of plan mode.
			if line.PermissionMode != "" && line.PermissionMode != p.mode {
				p.mode = line.PermissionMode
				p.emit(Event{Kind: EventNotice, Level: NoticeInfo, Text: fmt.Sprintf("Claude Code switched to permission mode %s", line.PermissionMode)})
			}
		case "compact_boundary":
			// The line the CLI writes once the summary has replaced the
			// older part of the session.
			p.emit(Event{Kind: EventCompaction, Phase: CompactionEnd})
		case "api_retry":
			p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: claudeRetryText(line)})
		case "hook_response":
			// The person's own hooks: news only when one did not simply
			// succeed.
			if line.Outcome != "" && line.Outcome != "success" {
				p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: p.hookText(line)})
			}
		case "task_notification":
			p.emit(claudeTaskNotice(line))
		case "elicitation_complete":
			p.emit(Event{Kind: EventNotice, Level: NoticeInfo, Text: fmt.Sprintf("Claude Code: MCP server %q says request %s is done", line.MCPServerName, line.ElicitationID)})
		}
	case "stream_event":
		if ev := line.Event; ev != nil && ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" && line.ParentToolUseID == nil {
			p.text.WriteString(ev.Delta.Text)
			p.emit(Event{Kind: EventText, Text: ev.Delta.Text})
		}
	case "assistant":
		p.assistant(line.Message, line.ParentToolUseID != nil)
	case "user":
		p.user(line.Message)
	case "result":
		result := line
		p.result = &result
		for _, d := range line.PermissionDenials {
			if p.askedIDs[d.ToolUseID] {
				continue
			}
			p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: fmt.Sprintf(
				"Claude Code turned down %s by its own rules, without asking anyone", describeClaudeUse(d.ToolName, d.ToolInput, p.cfg.MaxEventBytes))})
		}
	}
}

// claudeRetryText says what an api_retry record reports.
func claudeRetryText(line claudeLine) string {
	what := "the API request failed"
	if line.ErrorStatus != nil {
		what += fmt.Sprintf(" (HTTP %d)", *line.ErrorStatus)
	}
	if reason := claudeErrorText(line.Error); reason != "" {
		what += ": " + reason
	}
	return fmt.Sprintf("Claude Code: %s; retrying in %s, attempt %d of %d",
		what, time.Duration(line.RetryDelayMS*float64(time.Millisecond)).Round(100*time.Millisecond), line.Attempt, line.MaxRetries)
}

// hookText says how one of the person's hooks ended when it did not
// succeed, with what it said.
func (p *claudeParser) hookText(line claudeLine) string {
	how := map[string]string{"blocking": "blocked it", "non_blocking_error": "failed", "error": "failed", "cancelled": "was cancelled"}[line.Outcome]
	if how == "" {
		how = "ended " + line.Outcome
	}
	text := fmt.Sprintf("Claude Code's %s hook %q %s", line.HookEvent, line.HookName, how)
	for _, said := range []json.RawMessage{line.Stderr, line.Output, line.Stdout} {
		if s := strings.TrimSpace(rawText(said)); s != "" {
			return text + ": " + truncate(s, p.cfg.MaxEventBytes)
		}
	}
	return text
}

// claudeTaskNotice tells people how a background task ended.
func claudeTaskNotice(line claudeLine) Event {
	level, how := NoticeInfo, "finished"
	switch line.Status {
	case "failed":
		level, how = NoticeWarning, "failed"
	case "stopped":
		how = "was stopped"
	}
	text := "Claude Code's background task " + how
	if line.Summary != "" {
		text += ": " + line.Summary
	}
	return Event{Kind: EventNotice, Level: level, Text: text}
}

// rawText is a JSON value as text: a string as itself, anything else
// compacted, null as nothing.
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return compactJSON(raw)
}

// claudeErrorText is the readable part of an error the CLI reports, which
// is a string or an object with a message.
func claudeErrorText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		if obj.Message != "" {
			return obj.Message
		}
		if obj.Error != nil && obj.Error.Message != "" {
			return obj.Error.Message
		}
	}
	return compactJSON(raw)
}

// describeClaudeUse names a tool use in one line: a command as itself,
// anything else as the tool and its input.
func describeClaudeUse(tool string, input json.RawMessage, max int) string {
	var in struct {
		Command string `json:"command"`
	}
	if tool == "Bash" && json.Unmarshal(input, &in) == nil && in.Command != "" {
		return "`" + truncate(in.Command, max) + "`"
	}
	return tool + " " + truncate(compactJSON(input), max)
}

func (p *claudeParser) assistant(msg *claudeMessage, subagent bool) {
	for _, block := range blocksOf(msg) {
		switch block.Type {
		case "text":
			// With partial messages on, this text already streamed as deltas.
			if !p.cfg.StreamPartials && !subagent && block.Text != "" {
				p.text.WriteString(block.Text)
				p.emit(Event{Kind: EventText, Text: block.Text})
			}
		case "tool_use":
			p.toolNames[block.ID] = block.Name
			p.emit(Event{Kind: EventToolCall, Tool: block.Name, Input: truncate(compactJSON(block.Input), p.cfg.MaxEventBytes)})
			if path := editedPath(block); path != "" {
				p.edits[block.ID] = path
			}
		}
	}
}

func (p *claudeParser) user(msg *claudeMessage) {
	for _, block := range blocksOf(msg) {
		if block.Type != "tool_result" {
			continue
		}
		p.emit(Event{
			Kind: EventToolResult,
			Tool: p.toolNames[block.ToolUseID],
			Text: truncate(toolResultText(block.Content), p.cfg.MaxEventBytes),
		})
		if path, ok := p.edits[block.ToolUseID]; ok {
			delete(p.edits, block.ToolUseID)
			if !block.IsError {
				p.emit(Event{Kind: EventFileChanged, Path: path})
			}
		}
	}
}

// finish reconciles the parsed result with how the process ended.
func (p *claudeParser) finish(waitErr error, stderr string) (Result, error) {
	if p.result == nil {
		if waitErr != nil {
			detail := strings.TrimSpace(stderr)
			return Result{Failure: classifyFailure(detail)}, fmt.Errorf("claude: %w: %s", waitErr, detail)
		}
		return Result{}, errors.New("claude: output ended without a result record")
	}
	res := *p.result
	var usage Usage
	if u := res.Usage; u != nil {
		usage = Usage{InputTokens: tokens(u.InputTokens), CacheReadTokens: tokens(u.CacheReadInputTokens), CacheWriteTokens: tokens(u.CacheCreationInputTokens), OutputTokens: tokens(u.OutputTokens)}
	}
	if res.IsError {
		// The real reason, wherever the CLI put it; the subtype alone
		// ("error_during_execution") tells a person nothing.
		reason := strings.TrimSpace(res.Result)
		if reason == "" {
			reason = strings.TrimSpace(strings.Join(res.Errors, "; "))
		}
		if reason == "" {
			reason = strings.TrimSpace(stderr)
		}
		if reason == "" {
			reason = res.Subtype
		}
		return Result{Usage: usage, Failure: classifyFailure(reason)}, fmt.Errorf("claude: %s", reason)
	}

	output := res.Result
	if output == "" {
		output = p.text.String()
	}
	return Result{Output: output, SessionRef: p.sessionID, Usage: usage}, nil
}

// blocksOf decodes a message's content, which is an array of blocks or, for
// plain user text, a string.
func blocksOf(msg *claudeMessage) []claudeBlock {
	if msg == nil || len(msg.Content) == 0 {
		return nil
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

// editedPath returns the file an edit-type tool call targets.
func editedPath(block claudeBlock) string {
	if !claudeEditTools[block.Name] {
		return ""
	}
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if err := json.Unmarshal(block.Input, &input); err != nil {
		return ""
	}
	if input.FilePath != "" {
		return input.FilePath
	}
	return input.NotebookPath
}

// toolResultText flattens a tool_result content, which is a string or a
// list of text blocks.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf strings.Builder
	dec := json.NewEncoder(&buf)
	dec.SetEscapeHTML(false)
	var v any
	if err := json.Unmarshal(raw, &v); err != nil || dec.Encode(v) != nil {
		return string(raw)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// truncate cuts s to at most max bytes on a rune boundary and says how
// much was dropped.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… [%d more bytes]", s[:cut], len(s)-cut)
}

func optFloat(o map[string]any, key string) float64 {
	switch v := o[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// optStrings reads a list option; JSON arrays arrive as []any.
func optStrings(o map[string]any, key string) []string {
	list, _ := o[key].([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
