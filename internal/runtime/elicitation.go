package runtime

import (
	"context"
	"encoding/json"
)

// FormRequest is the input of a form approval: what an MCP server asks a
// person to fill in (MCP elicitation, form mode). Schema is the server's
// requestedSchema as it came, a flat object of text, number, yes-or-no and
// choice fields, which every runtime passes on in the same MCP shape
// (docs/design.md 4.6).
type FormRequest struct {
	Server  string          `json:"server"`
	Message string          `json:"message"`
	Schema  json.RawMessage `json:"schema"`
}

// LinkRequest is the input of a link approval: a page an MCP server asks a
// person to open, such as a sign-in, and to say when done (MCP
// elicitation, url mode).
type LinkRequest struct {
	Server  string `json:"server"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

// FormAnswer is what Decision.Answer carries for a filled-in form: the
// fields by name.
type FormAnswer struct {
	Content json.RawMessage `json:"content"`
}

// Content reads the fields of a filled-in form from a decision; nil when
// the form was declined or came back empty.
func (d Decision) Content() json.RawMessage {
	if !d.Allow || len(d.Answer) == 0 {
		return nil
	}
	var a FormAnswer
	if json.Unmarshal(d.Answer, &a) != nil || len(a.Content) == 0 || string(a.Content) == "null" {
		return nil
	}
	return a.Content
}

// askForm puts an MCP server's form to a person on behalf of tool and waits.
func (t *turnBase) askForm(ctx context.Context, tool string, req FormRequest) (Decision, error) {
	if len(req.Schema) == 0 {
		req.Schema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	input, err := json.Marshal(req)
	if err != nil {
		return Decision{}, err
	}
	return t.ask(ctx, ApprovalForm, tool, string(input))
}

// askLink puts an MCP server's link to a person on behalf of tool and waits.
func (t *turnBase) askLink(ctx context.Context, tool string, req LinkRequest) (Decision, error) {
	input, err := json.Marshal(req)
	if err != nil {
		return Decision{}, err
	}
	return t.ask(ctx, ApprovalLink, tool, string(input))
}

// elicitationTool names, in approvals, what an MCP server asks a person
// through a runtime (MCP elicitation).
const elicitationTool = "elicitation"

// Elicitation actions, as MCP names what a person did with a form or link.
const (
	ElicitAccept  = "accept"
	ElicitDecline = "decline"
	ElicitCancel  = "cancel"
)

// elicitationAction maps a person's decision on a form or a link onto an
// MCP elicitation action: accepted, declined, or cancelled when nobody got
// to decide (the turn ended first).
func elicitationAction(d Decision, err error) string {
	switch {
	case err != nil:
		return ElicitCancel
	case d.Allow:
		return ElicitAccept
	}
	return ElicitDecline
}
