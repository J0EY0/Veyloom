package wiki

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// Scanner looks for secrets in what is about to be written: a wiki is
// meant to be read, exported and shared, so a token pasted into a chat
// must not end up in it (design.md 5.3). It knows the common token formats;
// a person can add their own patterns.
type Scanner struct {
	rules []secretRule
}

type secretRule struct {
	name string
	re   *regexp.Regexp
}

// Finding is one likely secret: which rule matched, on which line. It never
// carries the secret itself.
type Finding struct {
	Rule string
	Line int
}

// defaultRules are formats precise enough to refuse a write on: tokens with
// a fixed prefix or shape, and private keys.
var defaultRules = []secretRule{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github-token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{"openai-key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{32,}`)},
	{"slack-token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"stripe-key", regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{20,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
}

// DefaultScanner knows the common token formats.
func DefaultScanner() *Scanner { return &Scanner{rules: defaultRules} }

// With returns a scanner that also looks for pattern, a Go regular
// expression, reported under name.
func (s *Scanner) With(name, pattern string) (*Scanner, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: secret pattern %s: %v", store.ErrInvalidInput, name, err)
	}
	rules := append(append([]secretRule(nil), s.rules...), secretRule{name, re})
	return &Scanner{rules: rules}, nil
}

// Scan returns what looks like a secret in text, first rule first. A
// piece of text two rules match is reported once, under the earlier rule.
func (s *Scanner) Scan(text string) []Finding {
	var found []Finding
	var taken [][]int
	for _, r := range s.rules {
		for _, loc := range r.re.FindAllStringIndex(text, -1) {
			if overlaps(taken, loc) {
				continue
			}
			taken = append(taken, loc)
			found = append(found, Finding{Rule: r.name, Line: strings.Count(text[:loc[0]], "\n") + 1})
		}
	}
	return found
}

func overlaps(spans [][]int, loc []int) bool {
	for _, s := range spans {
		if loc[0] < s[1] && s[0] < loc[1] {
			return true
		}
	}
	return false
}

// SecretError refuses a write that looks like it holds a secret.
type SecretError struct {
	Path     string
	Findings []Finding
}

func (e *SecretError) Error() string {
	parts := make([]string, 0, len(e.Findings))
	for _, f := range e.Findings {
		parts = append(parts, fmt.Sprintf("%s on line %d", f.Rule, f.Line))
	}
	return fmt.Sprintf("%s looks like it holds a secret (%s); take it out and write again", e.Path, strings.Join(parts, ", "))
}

func (e *SecretError) Unwrap() error { return store.ErrInvalidInput }
