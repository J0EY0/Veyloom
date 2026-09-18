package runtime

import "strings"

// FailureKind says why a turn failed, when its runtime could tell. The hub
// does not decide whether to try again by reading error text: it looks at
// how the turn behaved (it resumed a session and failed before saying or
// doing anything). The kind only settles what becomes of the session that
// would not resume: one the runtime itself declared gone is given up at
// once, any other is kept until a fresh session has proved it can do better.
type FailureKind string

const (
	// FailureSessionNotFound means the runtime no longer has the session,
	// or will not resume it here.
	FailureSessionNotFound FailureKind = "session_not_found"
	// FailureContextOverflow means the session has outgrown the model's
	// context window and the runtime could not compact it.
	FailureContextOverflow FailureKind = "context_overflow"
)

// What the CLIs say, lowercased. The lists only need to be good enough to
// name the reason: a wording they miss costs nothing but the label.
var (
	sessionNotFoundPhrases = []string{
		"no conversation found", // claude: No conversation found with session ID: …
		"no session found",      // pi: No session found matching '…'
		"fork this session",     // pi, asked to resume a session of another directory
		"session not found",
		"thread not found",
		"conversation not found",
	}
	contextOverflowPhrases = []string{
		"prompt is too long", // claude
		"context window",     // codex: … ran out of room in the model's context window
		"context length",
		"context_length_exceeded",
		"maximum context",
		"too many tokens",
	}
)

// classifyFailure names the reason in an error text, or "" when it is none
// the hub treats specially.
func classifyFailure(text string) FailureKind {
	text = strings.ToLower(text)
	for _, p := range sessionNotFoundPhrases {
		if strings.Contains(text, p) {
			return FailureSessionNotFound
		}
	}
	for _, p := range contextOverflowPhrases {
		if strings.Contains(text, p) {
			return FailureContextOverflow
		}
	}
	return ""
}
