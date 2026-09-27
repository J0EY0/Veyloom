// Package veyloom is the module root. It embeds the files commands hand
// out, so the binary carries them wherever it is installed.
package veyloom

import "embed"

// EnvExample is .env.example: every setting at its default, explained.
// `veyloom serve` writes it as .env on first run.
//
//go:embed .env.example
var EnvExample []byte

// Skills are the skills Veyloom gives every agent, in every project
// (docs/design.md 5.23.6), one folder each under skills/, as Agent Skills
// lays them out: apart from those people install for agents from the skill
// library, and changed here, with Veyloom.
//
//go:embed skills
var Skills embed.FS
