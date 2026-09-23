package okf

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFields_ReadWhatTheSpecAllows(t *testing.T) {
	d := mustParse(t, []byte(`---
type: Attested Computation
verified: { by: human:ahormati, at: 2026-06-25T09:00:00Z }
sources:
  - id: exec-rev-dash
    resource: dashboards/exec-revenue
    author: team:finance-fpa
    usage_count: 5000
    last_modified: 2026-06-18T00:00:00Z
    unknown_to_us: kept
  - not a mapping
tags: not-a-list
stale_after: 2026-12-31T00:00:00Z
metadata:
  veyloom-team: veyloom
  nested: { a: b }
---
`))
	verified := d.Verified()
	if len(verified) != 1 || verified[0].By != "human:ahormati" || !verified[0].At.Equal(time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("a bare verified mapping should read as a list of one: %+v", verified)
	}
	if TierOf(verified) != HumanReviewed {
		t.Errorf("tier %v", TierOf(verified))
	}
	sources := d.Sources()
	if len(sources) != 1 || sources[0].UsageCount != 5000 || sources[0].Author != "team:finance-fpa" || sources[0].LastModified.IsZero() {
		t.Errorf("sources %+v", sources)
	}
	if d.Tags() != nil || d.Status() != Stable {
		t.Errorf("malformed tags should read as none, a missing status as stable: %v %v", d.Tags(), d.Status())
	}
	if at, ok := d.StaleAfter(); !ok || at.Year() != 2026 {
		t.Errorf("stale after %v %v", at, ok)
	}
	if m := d.Metadata(); m["veyloom-team"] != "veyloom" || len(m) != 1 {
		t.Errorf("metadata should keep only string values: %v", m)
	}

	// Adding keeps what is there, down to keys we do not know.
	d.AddVerified(Stamp{By: "process:finance-nightly", At: time.Date(2026, 6, 26, 2, 0, 0, 0, time.UTC)})
	d.AddSource(Source{ID: "policy", Resource: "/policies/revenue.md"})
	d.SetMetadata("veyloom-team", "billing")
	out, _ := d.Bytes()
	back := mustParse(t, out)
	if got := back.Verified(); len(got) != 2 || got[1].By != "process:finance-nightly" {
		t.Errorf("verified after adding: %+v", got)
	}
	if got := back.Sources(); len(got) != 2 || got[1].Resource != "/policies/revenue.md" {
		t.Errorf("sources after adding: %+v", got)
	}
	if !strings.Contains(string(out), "unknown_to_us: kept") || !strings.Contains(string(out), "nested:") {
		t.Errorf("keys inside entries should survive:\n%s", out)
	}
	if back.Metadata()["veyloom-team"] != "billing" {
		t.Errorf("metadata after set: %v", back.Metadata())
	}
	if n := back.ReplaceSourceResource("/policies/revenue.md", "/policies/recognition.md"); n != 1 || back.Sources()[1].Resource != "/policies/recognition.md" {
		t.Errorf("replaced %d: %+v", n, back.Sources())
	}
}

func TestFields_SettersStartMissingKeys(t *testing.T) {
	d := New("Skill")
	d.SetMetadata("veyloom-team", "veyloom")
	d.SetString(KeyName, "commit-message")
	d.SetStaleAfter(time.Date(2027, 1, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)))
	d.SetTags(nil)
	if want := []string{"type", "name", "stale_after", "metadata"}; !slices.Equal(d.Keys(), want) {
		t.Errorf("keys %v, want %v", d.Keys(), want)
	}
	out, _ := d.Bytes()
	if !strings.Contains(string(out), "stale_after: 2026-12-31T16:00:00Z\n") {
		t.Errorf("times are stored in UTC:\n%s", out)
	}
}

func TestActors(t *testing.T) {
	for actor, ok := range map[string]bool{
		"human:owner":                 true,
		"human:jsmith@acme":           true,
		"process:veyloom":             true,
		"reference_agent/gemini-2.5":  true,
		"pi/deepseek/deepseek-chat":   true,
		"claude-code/claude-sonnet-5": true,
		"human:":                      false,
		"owner":                       false,
		"codex/":                      false,
		"/gpt-5":                      false,
		"team:finance/x":              false,
		"human:two words":             false,
		"":                            false,
	} {
		if ValidActor(actor) != ok {
			t.Errorf("ValidActor(%q) = %v, want %v", actor, !ok, ok)
		}
	}
	at := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	machine := []Stamp{{By: "process:nightly", At: at}}
	if TierOf(nil) != Unverified || TierOf(machine) != MachineConfirmed || TierOf(append(machine, Stamp{By: Human("a"), At: at.Add(time.Hour)})) != HumanReviewed {
		t.Error("tiers go unverified, machine-confirmed, human-reviewed")
	}
	if got := LastVerified(append(machine, Stamp{By: Human("a"), At: at.Add(time.Hour)})); !got.Equal(at.Add(time.Hour)) {
		t.Errorf("last verified %v", got)
	}
	if HumanReviewed.String() != "human-reviewed" || Unverified.String() != "unverified" {
		t.Error("tier names follow the spec")
	}
}
