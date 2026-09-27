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
	// ProxyBinary is the veyloom executable Claude Code and Codex run as
	// `mcp-proxy` to reach a turn's tools. Empty means there is no bridge:
	// their turns run without the room tools, and a Claude turn that would
	// ask for approval does not start.
	ProxyBinary string
	// RecordDir keeps what each CLI prints, as it prints it, a file a
	// turn: the real output the replay tests are made of (docs/design.md
	// 5.23.9). Empty records nothing.
	RecordDir string
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
	claudeCfg := DefaultClaudeConfig()
	claudeCfg.ProxyBinary = opts.ProxyBinary
	claudeCfg.RecordDir = opts.RecordDir
	claude := NewClaudeRunner(claudeCfg)
	codexCfg := DefaultCodexConfig()
	codexCfg.ProxyBinary = opts.ProxyBinary
	codexCfg.RecordDir = opts.RecordDir
	codex := NewCodexRunner(codexCfg)
	piCfg := DefaultPiConfig()
	piCfg.SessionDir = opts.SessionDir
	piCfg.ToolDir = opts.ToolDir
	piCfg.RecordDir = opts.RecordDir
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
