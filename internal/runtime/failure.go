package runtime

import (
	"regexp"
	"slices"
	"strings"
)

// FailureKind says why a turn failed, when its runtime could tell. The hub
// does not decide whether to try again by reading error text: it looks at
// how the turn behaved (it resumed a session and failed before saying or
// doing anything). The kind settles what becomes of the session that would
// not resume: one the runtime itself declared gone is given up at once,
// any other is kept until a fresh session has proved it can do better. A
// failure of the account's, which no session would get past, pauses what
// runs on the account instead (design.md 5.23.3).
type FailureKind string

const (
	// FailureSessionNotFound means the runtime no longer has the session,
	// or will not resume it here.
	FailureSessionNotFound FailureKind = "session_not_found"
	// FailureContextOverflow means the session has outgrown the model's
	// context window and the runtime could not compact it.
	FailureContextOverflow FailureKind = "context_overflow"
	// FailureAuth means the runtime is not signed in, or its credentials
	// were refused: nothing runs on it until a person signs in again.
	FailureAuth FailureKind = "auth"
	// FailureQuota means the account's usage limit or balance is used up:
	// nothing runs on it until the limit resets (Result.RetryAt, when the
	// runtime said when) or a person sees to it.
	FailureQuota FailureKind = "quota"
	// FailureRateLimit means the provider turned requests down as too many,
	// still, after the runtime's own retries.
	FailureRateLimit FailureKind = "rate_limit"
	// FailureServer means the provider failed, or could not be reached,
	// still, after the runtime's own retries.
	FailureServer FailureKind = "server"
)

// Account reports whether the failure is the account's, not the session's
// or the member's: every turn on the runtime there would fail alike until
// it passes.
func (k FailureKind) Account() bool {
	switch k {
	case FailureAuth, FailureQuota, FailureRateLimit, FailureServer:
		return true
	}
	return false
}

// What the CLIs and the providers behind them say, lowercased. The lists
// only need to be good enough to name the reason: a wording they miss
// costs the label, and with it the pause (design.md 5.23.3), no more. A
// runtime that says why in a field of its own is read by that first.
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
	// Ahead of the rate limits: some providers answer a spent quota with a
	// 429 of its own words.
	quotaPhrases = []string{
		"you've hit your",           // claude: You've hit your limit · resets 5pm
		"out of extra usage",        // claude
		"credit balance is too low", // claude, on an API key
		"usage limit",               // codex: You've hit your usage limit …
		"insufficient balance",      // deepseek, among pi's providers
		"insufficient_quota",        // openai
		"exceeded your current quota",
		"quota exceeded",
		"payment required",
		"billing",
	}
	authPhrases = []string{
		"not logged in",     // claude: Not logged in · Please run /login
		"please run /login", // claude
		"oauth token",       // claude: OAuth token revoked · Please run /login
		"no api key",        // pi: No API key found for deepseek
		"invalid api key",
		"invalid x-api-key",
		"incorrect api key",
		"api key is invalid",
		"authentication",
		"unauthorized",
		"not authenticated",
	}
	rateLimitPhrases = []string{
		"rate limit",
		"rate_limit",
		"ratelimit",
		"too many requests",
	}
	serverPhrases = []string{
		"overloaded",
		"internal server error",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"server error",
		"connection refused",
		"connection reset",
		"connection error",
		"network error",
		"fetch failed",
		"socket hang up",
		"timed out",
	}
	// An HTTP status, as the CLIs and the providers name one: opening the
	// message, or a line or a note of it ("429 Too Many Requests", "403
	// {…}"), or after what says it is one ("status 429", "status code: 503",
	// "HTTP 401", "API Error: 529", "Error code: 429", "\"status\":403").
	// A number anywhere else could be anything, a line of a stack trace say
	// ("cli.js:403:15"), and would pause an account for nothing.
	httpStatus = regexp.MustCompile(`(?:^|[\n|])\s*(?:error:\s*)?([1-5][0-9]{2})\b|(?:status(?:[ _]?code)?|http(?:/[0-9.]+)?|(?:api )?error(?:[ _]?code)?)["']?\s*[:=]?\s*\(?([1-5][0-9]{2})\b`)
)

// statuses are the HTTP statuses a lowercased text names.
func statuses(text string) map[string]bool {
	named := map[string]bool{}
	for _, m := range httpStatus.FindAllStringSubmatch(text, -1) {
		named[m[1]+m[2]] = true
	}
	return named
}

// classifyFailure names the reason in an error text, or "" when it is none
// the hub treats specially.
func classifyFailure(text string) FailureKind {
	text = strings.ToLower(text)
	has := func(phrases []string) bool {
		for _, p := range phrases {
			if strings.Contains(text, p) {
				return true
			}
		}
		return false
	}
	codes := statuses(text)
	named := func(of ...string) bool {
		return slices.ContainsFunc(of, func(code string) bool { return codes[code] })
	}
	switch {
	case has(sessionNotFoundPhrases):
		return FailureSessionNotFound
	case has(contextOverflowPhrases):
		return FailureContextOverflow
	case has(quotaPhrases) || named("402"):
		return FailureQuota
	case has(authPhrases) || named("401", "403"):
		return FailureAuth
	case has(rateLimitPhrases) || named("429"):
		return FailureRateLimit
	case has(serverPhrases) || named("500", "502", "503", "504", "529"):
		return FailureServer
	}
	return ""
}
