package store

import "fmt"

// Problem is a failure a person can run into, which the web client tells
// them of in their own language: Code names it in the client's word lists
// (error.<code> in web/src/i18n), and Params fill in the words. Text is the
// reason in English, for agents, logs and any client that does not know
// the code. Kind is the sentinel it is one of, which callers act on, the
// API by its HTTP status.
type Problem struct {
	Kind   error
	Code   string
	Params map[string]string
	Text   string
}

func (p *Problem) Error() string { return p.Kind.Error() + ": " + p.Text }

func (p *Problem) Unwrap() error { return p.Kind }

// Params are the named values a problem's words are filled in with.
type Params = map[string]string

// Invalid is a problem with what a person asked for (ErrInvalidInput).
func Invalid(code string, params Params, format string, args ...any) error {
	return &Problem{Kind: ErrInvalidInput, Code: code, Params: params, Text: fmt.Sprintf(format, args...)}
}

// Conflicting is a problem with the state of things (ErrConflict): what a
// person asked for clashes with what is there, or with what someone else
// did first.
func Conflicting(code string, params Params, format string, args ...any) error {
	return &Problem{Kind: ErrConflict, Code: code, Params: params, Text: fmt.Sprintf(format, args...)}
}

// Missing is a problem of something not there (ErrNotFound).
func Missing(code string, params Params, format string, args ...any) error {
	return &Problem{Kind: ErrNotFound, Code: code, Params: params, Text: fmt.Sprintf(format, args...)}
}

// stillRunning is what stops a change that has to wait for a member's or a
// project's turns to end.
func stillRunning() error {
	return Conflicting("turnStillRunning", nil, "a turn is still running")
}
