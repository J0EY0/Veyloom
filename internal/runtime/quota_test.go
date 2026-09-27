package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A claude.ai account that reaches its usage limit: the CLI tells how the
// account stands as it goes, and the turn fails for the quota, until the
// limit resets. The CLI's own message of the failed call is no reply.
func TestClaude_ALimitReachedIsTheQuotaUntilItResets(t *testing.T) {
	fakeClaudeCLI(t, `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1790500000,"rateLimitType":"five_hour","utilization":0.91},"uuid":"u1","session_id":"sess-1"}
{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1790503600,"rateLimitType":"five_hour","isUsingOverage":false},"uuid":"u2","session_id":"sess-1"}
{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"You've hit your limit · resets 5pm"}]},"parent_tool_use_id":null,"error":"rate_limit","session_id":"sess-1"}
{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit · resets 5pm","session_id":"sess-1","usage":{"input_tokens":0,"output_tokens":0}}
`, 1, "")
	events, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || res.Failure != FailureQuota || !res.RetryAt.Equal(time.Unix(1790503600, 0)) {
		t.Fatalf("err = %v, result = %+v; want the quota, until 1790503600", err, res)
	}
	quotas := eventsOf(events, EventQuota)
	if len(quotas) != 2 {
		t.Fatalf("quota events = %+v", quotas)
	}
	if q := quotas[0].Quota; q.Limited || q.Window != "5h" || q.UsedPercent == nil || *q.UsedPercent != 91 || !q.ResetsAt.Equal(time.Unix(1790500000, 0)) {
		t.Errorf("the warning: %+v", q)
	}
	if q := quotas[1].Quota; !q.Limited || q.UsedPercent == nil || *q.UsedPercent != 100 {
		t.Errorf("the limit reached: %+v", q)
	}
	if text := eventsOf(events, EventText); len(text) != 0 {
		t.Errorf("the CLI's message of the failure is no reply: %+v", text)
	}
}

// A limit reached with extra usage to go on with holds nothing up.
func TestClaude_ALimitReachedOnExtraUsageIsNoQuota(t *testing.T) {
	q := claudeRateLimit{Status: "rejected", ResetsAt: 1790503600, RateLimitType: "seven_day_opus", IsUsingOverage: true}.quota()
	if q.Limited || q.Window != "7d opus" {
		t.Errorf("quota = %+v", q)
	}
}

// What the CLI names of a failed model call says why the turn failed.
func TestClaude_TheFailedCallNamesTheFailure(t *testing.T) {
	for kind, want := range map[string]FailureKind{
		"authentication_failed": FailureAuth,
		"billing_error":         FailureQuota,
		"rate_limit":            FailureRateLimit,
		"server_error":          FailureServer,
		"invalid_request":       "",
	} {
		fakeClaudeCLI(t, `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5"}
{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"API Error"}]},"parent_tool_use_id":null,"error":"`+kind+`","session_id":"sess-1"}
{"type":"result","subtype":"success","is_error":true,"result":"API Error","session_id":"sess-1","usage":{"input_tokens":0,"output_tokens":0}}
`, 1, "")
		_, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
		if err == nil || res.Failure != want || !res.RetryAt.IsZero() {
			t.Errorf("%s: err = %v, result = %+v, want %q", kind, err, res, want)
		}
	}
}

// Codex tells how the account's limits stand, the tightest window first,
// and names the failure; the limit reached gives when it resets.
func TestCodex_ALimitReachedIsTheQuotaUntilItResets(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[limit]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	if err == nil || res.Failure != FailureQuota || !res.RetryAt.Equal(time.Unix(1790503600, 0)) {
		t.Fatalf("err = %v, result = %+v", err, res)
	}
	quotas := eventsOf(events, EventQuota)
	if len(quotas) != 2 {
		t.Fatalf("quota events = %+v", quotas)
	}
	if q := quotas[0].Quota; q.Limited || q.Window != "5h" || *q.UsedPercent != 80 {
		t.Errorf("the first: %+v", q)
	}
	if q := quotas[1].Quota; !q.Limited || q.Window != "5h" || *q.UsedPercent != 100 || !q.ResetsAt.Equal(time.Unix(1790503600, 0)) {
		t.Errorf("the limit reached: %+v", q)
	}
	if len(h.sent(t)["account/rateLimits/read"]) != 0 {
		t.Error("the reset was known: no need to ask")
	}
}

// A limit reached that Codex did not tell of: the runner asks when it
// resets.
func TestCodex_ALimitReachedUntoldIsAskedAbout(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[limit-read]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, _ := turn.Result()
	if res.Failure != FailureQuota || !res.RetryAt.Equal(time.Unix(1790507200, 0)) {
		t.Errorf("result = %+v", res)
	}
}

func TestCodex_NamesWhyTheAccountFailed(t *testing.T) {
	for prompt, want := range map[string]FailureKind{"[unauthorized]": FailureAuth, "[disconnected]": FailureServer} {
		h := newCodexHarness(t)
		turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: prompt, Permission: PermissionFullAuto})
		if err != nil {
			t.Fatal(err)
		}
		drain(t, turn)
		if res, err := turn.Result(); err == nil || res.Failure != want {
			t.Errorf("%s: err = %v, failure = %q, want %q", prompt, err, res.Failure, want)
		}
	}
}

func TestCodexLimits_Quota(t *testing.T) {
	window := func(used int, mins, resets int64) *codexLimitWindow {
		return &codexLimitWindow{UsedPercent: used, WindowDurationMins: mins, ResetsAt: resets}
	}
	reached := "rate_limit_reached"
	for name, c := range map[string]struct {
		limits  codexLimits
		limited bool
		window  string
		used    int
	}{
		"the most used":         {codexLimits{Primary: window(20, 300, 10), Secondary: window(70, 10080, 20)}, false, "7d", 70},
		"the one used up":       {codexLimits{Primary: window(100, 300, 10), Secondary: window(90, 10080, 20)}, true, "5h", 100},
		"both, the later reset": {codexLimits{Primary: window(100, 300, 10), Secondary: window(100, 10080, 20)}, true, "7d", 100},
		"said reached":          {codexLimits{Primary: window(97, 300, 10), RateLimitReachedType: &reached}, true, "5h", 97},
		"one of its own":        {codexLimits{Primary: window(5, 90, 10)}, false, "90m", 5},
		// Used up, credits go on: not limited until Codex says so.
		"on credits":            {codexLimits{Primary: window(100, 300, 10), Credits: &codexCredits{HasCredits: true}}, false, "5h", 100},
		"on unlimited credits":  {codexLimits{Primary: window(100, 300, 10), Credits: &codexCredits{Unlimited: true}}, false, "5h", 100},
		"credits spent":         {codexLimits{Primary: window(100, 300, 10), Credits: &codexCredits{}}, true, "5h", 100},
		"credits, said reached": {codexLimits{Primary: window(100, 300, 10), Credits: &codexCredits{HasCredits: true}, RateLimitReachedType: &reached}, true, "5h", 100},
	} {
		q, ok := c.limits.quota()
		if !ok || q.Limited != c.limited || q.Window != c.window || *q.UsedPercent != c.used {
			t.Errorf("%s: quota = %+v", name, q)
		}
	}
	if _, ok := (codexLimits{}).quota(); ok {
		t.Error("no window, no quota")
	}

	// A sparse update leaves the credits as they were.
	known := codexLimits{Primary: window(100, 300, 10), Credits: &codexCredits{HasCredits: true}}
	if merged := known.merge(codexLimits{Primary: window(100, 300, 12)}); merged.Credits == nil || !merged.Credits.HasCredits || merged.Primary.ResetsAt != 12 {
		t.Errorf("merged: %+v", merged)
	}
	// When the limit a turn ran into resets.
	for name, c := range map[string]struct {
		limits codexLimits
		want   int64
	}{
		"used up":       {codexLimits{Primary: window(100, 300, 10)}, 10},
		"said reached":  {codexLimits{Primary: window(90, 300, 10), RateLimitReachedType: &reached}, 10},
		"not reached":   {codexLimits{Primary: window(90, 300, 10)}, 0},
		"no reset told": {codexLimits{Primary: window(100, 300, 0)}, 0},
		"no window":     {codexLimits{}, 0},
	} {
		if got := c.limits.reset(); c.want == 0 && !got.IsZero() || c.want != 0 && !got.Equal(time.Unix(c.want, 0)) {
			t.Errorf("%s: reset %v", name, got)
		}
	}
	// Whose limit it is.
	for id, want := range map[string]bool{"": true, "codex": true, "gpt-5.3-codex-spark": false} {
		l := codexLimits{}
		if id != "" {
			l.LimitID = &id
		}
		if l.ofCodex() != want {
			t.Errorf("limit %q of Codex's turns: %v", id, !want)
		}
	}
}

// Pi names only in words why the provider turned it down.
func TestPi_NamesWhyTheAccountFailed(t *testing.T) {
	for text, want := range map[string]FailureKind{"402 Insufficient Balance": FailureQuota, "401 Authentication Fails": FailureAuth, "429 Too Many Requests": FailureRateLimit} {
		fakePiCLI(t, `{"type":"session","version":3,"id":"s","timestamp":"","cwd":"/tmp"}
{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"`+text+`"}}
{"type":"agent_end","messages":[]}
`, 0, "")
		_, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), text) || res.Failure != want {
			t.Errorf("%s: err = %v, failure = %q, want %q", text, err, res.Failure, want)
		}
	}
}

// The fake scripts the account's standing and a failure of the account's.
func TestFake_QuotaAndAccountFailures(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{
		"quota": map[string]any{"limited": true, "window": "5h", "used_percent": 100, "resets_in_ms": 60000},
		"fail":  true, "failure": "quota", "retry_in_ms": 60000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	quotas := eventsOf(events, EventQuota)
	if len(quotas) != 1 || !quotas[0].Quota.Limited || quotas[0].Quota.ResetsAt.Before(time.Now().Add(50*time.Second)) {
		t.Errorf("quota events = %+v", quotas)
	}
	if err == nil || res.Failure != FailureQuota || res.RetryAt.Before(time.Now().Add(50*time.Second)) {
		t.Errorf("result = %+v, %v", res, err)
	}
}
