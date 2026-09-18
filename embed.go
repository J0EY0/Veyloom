// Package veyloom is the module root. It embeds the files commands hand
// out, so the binary carries them wherever it is installed.
package veyloom

import _ "embed"

// EnvExample is .env.example: every setting at its default, explained.
// `veyloom serve` writes it as .env on first run.
//
//go:embed .env.example
var EnvExample []byte
