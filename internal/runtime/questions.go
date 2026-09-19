package runtime

import (
	"context"
	"encoding/json"
	"strings"
)

// Question is one thing a runtime asks a person, in the shape every
// runtime's questions are put into so that one card answers them all
// (docs/design.md 4.6). It is the input of a question approval.
type Question struct {
	// ID names the question in the answers.
	ID string `json:"id"`
	// Header is a short label for the question, such as "Database".
	Header   string           `json:"header,omitempty"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options,omitempty"`
	// MultiSelect lets the person pick more than one option.
	MultiSelect bool `json:"multiSelect,omitempty"`
	// Other lets the person write an answer of their own; a question
	// without options always does.
	Other bool `json:"other,omitempty"`
	// Secret asks for something that is not shown again, like a password:
	// the answer reaches the runtime and is kept nowhere else.
	Secret bool `json:"secret,omitempty"`
	// Placeholder hints at what to write in an answer of one's own;
	// Multiline asks for text of several lines, and Default is the text the
	// answer starts from, which the person edits.
	Placeholder string `json:"placeholder,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
	Default     string `json:"default,omitempty"`
}

// QuestionOption is one answer a person may pick.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionSet is the input of a question approval.
type QuestionSet struct {
	Questions []Question `json:"questions"`
}

// Answers is what a person answered, as Decision.Answer carries it: by
// question id, the options picked or the text written.
type Answers struct {
	Answers map[string][]string `json:"answers"`
}

// Answers reads the answers a decision carries. A decision that allowed
// nothing, or carries no answers, has none.
func (d Decision) Answers() map[string][]string {
	if !d.Allow || len(d.Answer) == 0 {
		return nil
	}
	var a Answers
	if json.Unmarshal(d.Answer, &a) != nil {
		return nil
	}
	return a.Answers
}

// askQuestions puts questions to a person on behalf of tool and waits. The
// decision says whether they answered; its Answers say what.
func (t *turnBase) askQuestions(ctx context.Context, tool string, questions []Question) (Decision, error) {
	input, err := json.Marshal(QuestionSet{Questions: questions})
	if err != nil {
		return Decision{}, err
	}
	return t.ask(ctx, ApprovalQuestion, tool, string(input))
}

// answerText joins the answers to one question the way a runtime that takes
// one string per question wants them.
func answerText(answers []string) string {
	return strings.Join(answers, ", ")
}
