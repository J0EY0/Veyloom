package runtime

import (
	"context"
	"encoding/json"
	"time"
)

// Pi's extensions reach the person through ctx.ui, which in RPC mode pi
// turns into extension_ui_request lines on stdout (docs/design.md 4.6).
// The dialogs wait for an extension_ui_response with the same id:
//
//	confirm  yes or no                 an approval, confirmed or not
//	select   one of a list of options  a question with those options
//	input    a line of text            a question answered in writing
//	editor   text of several lines     the same, starting from its prefill
//
// notify shows the person something, and becomes a notice. The rest
// (setStatus, setWidget, setTitle, set_editor_text) dress pi's own screen
// and have no place in the room.

// piUIRequest is an extension_ui_request, fields filled by Method.
type piUIRequest struct {
	ID          string   `json:"id"`
	Method      string   `json:"method"`
	Title       string   `json:"title"`
	Message     string   `json:"message"`
	Options     []string `json:"options"`
	Placeholder string   `json:"placeholder"`
	Prefill     string   `json:"prefill"`
	// Timeout, in milliseconds, is when pi stops waiting and settles the
	// dialog with its default.
	Timeout    float64 `json:"timeout"`
	NotifyType string  `json:"notifyType"`
}

// piConfirmTool names, in approvals, an extension's yes-or-no question.
const piConfirmTool = "confirm"

// serveUI takes an extension's request: a dialog is put to people on a
// goroutine of its own, a notification shown.
func (t *piTurn) serveUI(raw []byte) {
	var req piUIRequest
	if json.Unmarshal(raw, &req) != nil {
		return
	}
	switch req.Method {
	case "notify":
		level := NoticeInfo
		if req.NotifyType == NoticeWarning || req.NotifyType == NoticeError {
			level = req.NotifyType
		}
		t.notice(t.ctx, level, req.Message)
	case "confirm", "select", "input", "editor":
		go t.answerUI(req)
	}
}

// answerUI puts a dialog to people and sends pi what they said. A dialog
// with a timeout is taken back from people once pi has settled it itself,
// and nothing is sent then; nor once the turn is over.
func (t *piTurn) answerUI(req piUIRequest) {
	ctx := t.ctx
	if req.Timeout > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, time.Duration(req.Timeout*float64(time.Millisecond)))
		defer stop()
	}
	resp := map[string]any{"type": "extension_ui_response", "id": req.ID}
	if req.Method == "confirm" {
		input, _ := json.Marshal(map[string]string{"title": req.Title, "message": req.Message})
		d, err := t.requestApproval(ctx, piConfirmTool, string(input))
		if err != nil {
			return
		}
		resp["confirmed"] = d.Allow
	} else {
		q := Question{ID: "1", Question: req.Title}
		switch req.Method {
		case "select":
			q.Options = make([]QuestionOption, len(req.Options))
			for i, o := range req.Options {
				q.Options[i] = QuestionOption{Label: o}
			}
		case "input":
			q.Placeholder = req.Placeholder
		case "editor":
			q.Multiline, q.Default = true, req.Prefill
		}
		d, err := t.askQuestions(ctx, req.Method, []Question{q})
		if err != nil {
			return
		}
		if answer := d.Answers()[q.ID]; len(answer) > 0 {
			resp["value"] = answer[0]
		} else {
			resp["cancelled"] = true
		}
	}
	if ctx.Err() == nil {
		t.in.send(resp)
	}
}
