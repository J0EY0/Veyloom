package runtime

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// commandWords splits a command a runtime asks to run into its words when
// it is one simple command: no operators (;, &&, |, redirections,
// subshells), no expansions ($, backquotes), no comments and no newlines.
// A command wrapped in a shell, as Codex runs them (/bin/zsh -lc '…'), is
// unwrapped first. ok is false for anything else: no prefix ever covers
// such a command, and a person decides on it (docs/design.md 4.6).
func commandWords(command string) (words []string, ok bool) {
	words, ok = shellWords(command)
	if !ok {
		return nil, false
	}
	if len(words) == 3 && shells[filepath.Base(words[0])] && (words[1] == "-c" || words[1] == "-lc") {
		return shellWords(words[2])
	}
	return words, true
}

// shells are the shells a command may come wrapped in.
var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true}

// shellWords splits s as a POSIX shell splits one simple command: on blanks,
// with single quotes, double quotes and backslashes honoured. ok is false
// when s is empty, leaves a quote open, or holds anything that would make
// it more than one plain command: an operator, a redirection, an expansion
// or a comment.
func shellWords(s string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, false
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			for i++; ; i++ {
				if i >= len(s) {
					return nil, false
				}
				d := s[i]
				if d == '"' {
					break
				}
				switch d {
				case '$', '`':
					return nil, false
				case '\\':
					// Inside double quotes a backslash escapes only these.
					if i+1 < len(s) && strings.IndexByte("$`\"\\", s[i+1]) >= 0 {
						i++
						d = s[i]
					}
				}
				cur.WriteByte(d)
			}
			inWord = true
		case c == '\\':
			if i+1 >= len(s) || s[i+1] == '\n' {
				return nil, false
			}
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == '#' && !inWord:
			return nil, false
		case strings.IndexByte(";&|<>()`$\n\r", c) >= 0:
			return nil, false
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, len(words) > 0
}

// hasPrefix reports whether words start with every word of prefix.
func hasPrefix(words, prefix []string) bool {
	if len(prefix) == 0 || len(words) < len(prefix) {
		return false
	}
	for i, w := range prefix {
		if words[i] != w {
			return false
		}
	}
	return true
}

// commandPrefixes reads the command prefixes among rules, each a JSON array
// of words as Similar.Standing keeps them; anything else is left out.
func commandPrefixes(rules []string) [][]string {
	var out [][]string
	for _, rule := range rules {
		var words []string
		if json.Unmarshal([]byte(rule), &words) == nil && len(words) > 0 {
			out = append(out, words)
		}
	}
	return out
}
