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
		"claude: Not logged in · Please run /login":                                      "",
		"pi: No API key found for deepseek":                                              "",
		"":                                                                               "",
	}
	for text, want := range cases {
		if got := classifyFailure(text); got != want {
			t.Errorf("classifyFailure(%q) = %q, want %q", text, got, want)
		}
	}
}
