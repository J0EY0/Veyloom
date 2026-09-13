package engine

// Builtin returns the detectors for every engine Veyloom knows about, in the
// order they should be displayed. The fake engine is included so that a
// template may target it for tests and demos.
func Builtin() []Detector {
	return []Detector{Claude(), Codex(), Pi(), NewFake()}
}

// BuiltinRunners returns the engines that can execute turns, keyed by name.
// Real engines are added here as their runners are implemented.
func BuiltinRunners() map[string]Runner {
	fake := NewFake()
	claude := NewClaudeRunner(DefaultClaudeConfig())
	codex := NewCodexRunner(DefaultCodexConfig())
	pi := NewPiRunner(DefaultPiConfig())
	return map[string]Runner{fake.Name(): fake, claude.Name(): claude, codex.Name(): codex, pi.Name(): pi}
}

// Names returns the identifiers of every builtin engine, for validating
// references such as an agent template's engine field.
func Names() []string {
	detectors := Builtin()
	names := make([]string, 0, len(detectors))
	for _, d := range detectors {
		names = append(names, d.Name())
	}
	return names
}
