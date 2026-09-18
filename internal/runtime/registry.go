package runtime

// Builtin returns the detectors for every runtime Veyloom knows about, in the
// order they should be displayed. The fake runtime is included so that an
// agent may target it for tests and demos.
func Builtin() []Detector {
	return []Detector{Claude(), Codex(), Pi(), NewFake()}
}

// RunnerOptions are the settings of the machine the runners run on, as far
// as the runners care.
type RunnerOptions struct {
	// SessionDir is where runners that keep session files of their own put
	// them (see PiConfig.SessionDir). Empty leaves sessions to each CLI.
	SessionDir string
	// ToolDir is where runners write the files that give their CLI the
	// room tools (see PiConfig.ToolDir). Empty means a temporary directory.
	ToolDir string
}

// BuiltinRunners returns the runtimes that can execute turns, keyed by name,
// with no machine settings: sessions live wherever each CLI keeps them.
func BuiltinRunners() map[string]Runner {
	return BuiltinRunnersWith(RunnerOptions{})
}

// BuiltinRunnersWith is BuiltinRunners for a machine with settings. Real
// runtimes are added here as their runners are implemented.
func BuiltinRunnersWith(opts RunnerOptions) map[string]Runner {
	fake := NewFake()
	claude := NewClaudeRunner(DefaultClaudeConfig())
	codex := NewCodexRunner(DefaultCodexConfig())
	piCfg := DefaultPiConfig()
	piCfg.SessionDir = opts.SessionDir
	piCfg.ToolDir = opts.ToolDir
	pi := NewPiRunner(piCfg)
	return map[string]Runner{fake.Name(): fake, claude.Name(): claude, codex.Name(): codex, pi.Name(): pi}
}

// Names returns the identifiers of every builtin runtime, for validating
// references such as an agent's runtime field.
func Names() []string {
	detectors := Builtin()
	names := make([]string, 0, len(detectors))
	for _, d := range detectors {
		names = append(names, d.Name())
	}
	return names
}
