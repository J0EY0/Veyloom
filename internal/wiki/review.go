package wiki

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Checking pages again (docs/design.md 5.16, after LLM Wiki v2's
// lifecycle). A page counts as checked when it was last written or
// confirmed, whichever is later; it is due to be checked again when a file
// it names has changed since, when as long has passed as pages of its type
// hold, or when its stale_after has come. A resident page, which every
// turn carries, is checked twice as often.

const day = 24 * time.Hour

// reviewEvery is how long each type of page holds before it is checked
// again. A type not here is not checked: a topic's write-up says what the
// topic came to, which does not go out of date.
var reviewEvery = map[string]time.Duration{
	"Decision":   180 * day,
	"Convention": 180 * day,
	"Module":     90 * day,
	"Fact":       60 * day,
	"Pitfall":    30 * day,
}

// CheckedAt is when the page was last written or confirmed, whichever is
// later; a page written by hand without either is as old as its file.
func (s Summary) CheckedAt() time.Time {
	at := s.Generated.At
	if s.Verified.After(at) {
		at = s.Verified
	}
	if at.IsZero() {
		at = s.Modified
	}
	return at
}

// ReviewEvery is how long the page holds before it is checked again: zero
// for one that is not checked again, being deprecated or of a type that
// does not go out of date.
func (s Summary) ReviewEvery() time.Duration {
	every := reviewEvery[s.Type]
	if every == 0 || s.Status == okf.Deprecated {
		return 0
	}
	if s.Carried() {
		every /= 2
	}
	return every
}

// maxMentions bounds the paths kept for one page.
const maxMentions = 50

// codeSpan is text in backticks, where a file name alone names a file.
var codeSpan = regexp.MustCompile("`([^`\n]+)`")

// fileExts are the extensions that make a name alone in code a file's. A
// word with a dot in code is as often a method or a field (t.Parallel,
// m.name) or a version (v0.2) as a file, so only a name ending the way
// files commonly do counts.
var fileExts = map[string]bool{}

func init() {
	for _, ext := range strings.Fields(`
		go mod sum work md mdx txt rst adoc json jsonc yaml yml toml ini cfg conf env lock
		ts tsx js jsx mjs cjs vue svelte astro css scss sass less html htm svg
		py pyi rb rs java kt kts scala swift m mm c h cc cpp cxx hpp cs fs php lua pl
		ex exs erl hs ml clj dart zig nim jl r sql graphql gql proto thrift
		sh bash zsh fish ps1 bat mk make cmake gradle bazel bzl nix tf hcl
		xml plist csv tsv log patch diff pem crt key ipynb wasm
		png jpg jpeg gif webp ico pdf zip tar gz tgz`) {
		fileExts[ext] = true
	}
}

// Mentions are the paths of the repository a page's text names: a path
// like internal/hub/brief.go or web/src/features/, and in code a file name
// like `go.mod`. Web addresses, links to pages of the wiki and the files it
// keeps (/files/…) are not. They are as written, without a leading ./ or a
// trailing slash; a path's last part may be a directory.
func Mentions(body string) []string {
	var out []string
	add := func(token string, bare bool) {
		token = strings.TrimRight(token, ".")
		token = strings.TrimPrefix(token, "./")
		token = strings.TrimSuffix(token, "/")
		switch {
		case token == "" || len(out) >= maxMentions || slices.Contains(out, token):
		case strings.HasPrefix(token, "//"):
			// What is left of a web address split at its colon.
		case strings.HasPrefix(token, "/") && strings.HasSuffix(token, ".md"):
			// A link to another page of the wiki.
		case strings.HasPrefix(token, "/"+AssetDir+"/"):
			// A file the wiki keeps, not one of the repository.
		case strings.Contains(strings.Trim(token, "/"), "/"):
			out = append(out, token)
		case bare && fileName(token):
			out = append(out, token)
		}
	}
	for _, m := range codeSpan.FindAllStringSubmatch(body, -1) {
		for _, token := range strings.FieldsFunc(m[1], notPathRune) {
			add(token, true)
		}
	}
	for _, token := range strings.FieldsFunc(body, notPathRune) {
		add(token, false)
	}
	return out
}

// fileName reports whether a name alone is a file's: a name, a dot, and
// an extension files commonly have.
func fileName(token string) bool {
	i := strings.LastIndex(token, ".")
	return i > 0 && fileExts[strings.ToLower(token[i+1:])]
}

// notPathRune splits text into what may be paths: ASCII letters, digits
// and the few marks paths are written with.
func notPathRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	}
	return !strings.ContainsRune("/._-@", r)
}

// NamesFile reports whether a path a page names is the file a turn changed,
// or a directory it is in. Turns record files as their runtime gave them,
// from the repository's root, from the worktree's or in full, so a path
// named from the root matches the end of one written in full.
func NamesFile(mention, file string) bool {
	if file == mention || strings.HasSuffix(file, "/"+mention) {
		return true
	}
	return strings.HasPrefix(file, mention+"/") || strings.Contains(file, "/"+mention+"/")
}
