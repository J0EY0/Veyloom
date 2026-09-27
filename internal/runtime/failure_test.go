package runtime

import "testing"

func TestClassifyFailure(t *testing.T) {
	cases := map[string]FailureKind{
		"claude: No conversation found with session ID: 0b6f5c1e":                        FailureSessionNotFound,
		"pi: No session found matching '00000000-dead-beef'":                             FailureSessionNotFound,
		"pi: Fork this session into current directory? [y/N]":                            FailureSessionNotFound,
		"claude: Prompt is too long":                                                     FailureContextOverflow,
		"codex: Codex ran out of room in the model's context window. Start a new thread": FailureContextOverflow,
		"This model's maximum context length is 128000 tokens":                           FailureContextOverflow,

		"claude: Not logged in · Please run /login":            FailureAuth,
		"claude: OAuth token revoked · Please run /login":      FailureAuth,
		"claude: Invalid API key · Fix external API key":       FailureAuth,
		"pi: No API key found for deepseek":                    FailureAuth,
		"pi: 401 Authentication Fails, Your api key is wrong":  FailureAuth,
		`403 {"error":{"message":"permission denied"}}`:        FailureAuth,
		"codex: unexpected status 401":                         FailureAuth,
		"HTTP 402 when asked":                                  FailureQuota,
		`Error code: 429 - {"error":{"type":"requests"}}`:      FailureRateLimit,
		"pi: bad response | 503 upstream said no":              FailureServer,
		`{"type":"error","status":502}`:                        FailureServer,
		"claude: You've hit your limit · resets 5pm":           FailureQuota,
		"claude: Credit balance is too low":                    FailureQuota,
		"codex: You've hit your usage limit. Try again later.": FailureQuota,
		"pi: 402 Insufficient Balance":                         FailureQuota,
		"pi: 429 You exceeded your current quota":              FailureQuota,
		"pi: 429 Too Many Requests":                            FailureRateLimit,
		"codex: rate limit reached for requests":               FailureRateLimit,
		"claude: API Error: 529 Overloaded":                    FailureServer,
		"pi: 503 Service Unavailable":                          FailureServer,
		"pi: fetch failed":                                     FailureServer,

		// A number that is no status, and what the hub has no word for.
		"pi: the prompt had 4290 tokens and 5021 lines":                                                                      "",
		"claude: TypeError: x is not a function\n    at run (/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js:403:15)": "",
		"pi: at agent-session.js:429:7 | at process (node:internal/process/task_queues:95:5)":                                "",
		"pi: read 503 files, wrote 401 lines":                                                                                "",
		"codex: exit status 1":                                                                                               "",
		"":                                                                                                                   "",
	}
	for text, want := range cases {
		if got := classifyFailure(text); got != want {
			t.Errorf("classifyFailure(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestFailureKind_Account(t *testing.T) {
	for kind, want := range map[FailureKind]bool{
		FailureAuth: true, FailureQuota: true, FailureRateLimit: true, FailureServer: true,
		FailureSessionNotFound: false, FailureContextOverflow: false, "": false,
	} {
		if kind.Account() != want {
			t.Errorf("%q.Account() = %v", kind, !want)
		}
	}
}
